package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// followUpAttempt runs the second attempt of a task on the harness, after
// the previous one described by previous, and returns the run Kuayle created.
func followUpAttempt(t *testing.T, provider string, previous func(machine uuid.UUID) *domain.AgentHandoff) (domain.AgentTask, domain.DevMachineAgentRun) {
	t.Helper()
	ws, machine, user, checkout := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &taskPolicyStore{devMachineStoreFake: &devMachineStoreFake{
		policy:        testPolicy(ws),
		machine:       &domain.DevMachine{ID: machine, WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		agentProvider: &domain.DevMachineAgentProvider{ProviderID: provider, Enabled: true, RequiredSecrets: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		checkouts:     []domain.DevMachineCheckout{{ID: checkout, WorkspaceID: ws, MachineID: machine, Status: "ready", WorkspacePath: "/workspace/tasks/check", RepositoryFullName: "example/test", BaseBranch: "main", WorkingBranch: "task/check"}},
	}}
	store.policy.AllowedProviders = json.RawMessage(`["opencode","codex","claude-code"]`)
	machines := newTestDevMachineService(store)
	machines.agents = agent.NewRegistry(agent.NewOpenCodeProvider(""), agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""))
	runtime := &devMachineTaskRuntime{machines: machines, tasks: &taskRuntimeStoreFake{previous: previous(machine)}}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, MachineID: &machine, CheckoutID: checkout, RunID: uuid.New(), State: "preparing", Provider: provider, Prompt: "Address the review", Kind: domain.AgentTaskKindImplement, Generation: 2}
	_, err := runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Len(t, store.agentRuns, 1)
	return task, store.agentRuns[0]
}

// A second OpenCode attempt on the same machine after a succeeded one
// continues the session the first one reported, in the task's session
// directory, and its handoff tells the agent so. Kuayle never assigns an
// OpenCode session: the attempt records the one OpenCode reports.
func TestOpenCodeFollowUpContinuesTheSessionOfASucceededAttempt(t *testing.T) {
	const session = "ses_df21986abffefn2FxDuH6uZGLA"
	task, run := followUpAttempt(t, "opencode", func(machine uuid.UUID) *domain.AgentHandoff {
		return &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: machine, NativeSessionID: session}
	})
	var argv []string
	require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
	require.Equal(t, "/workspace/.kuayle/agent-sessions/"+task.ID.String(), argv[4])
	require.Contains(t, strings.Join(argv, " "), "--standalone --session "+session+" -- ")
	require.Nil(t, run.NativeSessionID, "the session is recorded from OpenCode's own events")
	require.Contains(t, run.Prompt, "this continues your previous session")

	_, fresh := followUpAttempt(t, "opencode", func(uuid.UUID) *domain.AgentHandoff {
		return &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: uuid.New(), NativeSessionID: session}
	})
	require.NoError(t, json.Unmarshal(fresh.CommandArgv, &argv))
	require.NotContains(t, argv, "--session", "the store of another machine is not here")
	require.Contains(t, fresh.Prompt, "the previous session cannot be resumed")
}
