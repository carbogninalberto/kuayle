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
	store.policy.AllowedProviders = json.RawMessage(`["opencode","codex","claude-code","qwen-code","kimi","goose"]`)
	machines := newTestDevMachineService(store)
	machines.agents = agent.NewRegistry(agent.NewOpenCodeProvider(""), agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""),
		agent.NewQwenCodeProvider(""), agent.NewKimiProvider(""), agent.NewGooseProvider(""))
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
		return &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: machine, NativeSessionID: session, Provider: "opencode"}
	})
	var argv []string
	require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
	require.Equal(t, "/workspace/.kuayle/agent-sessions/"+task.ID.String(), argv[4])
	require.Contains(t, strings.Join(argv, " "), "--standalone --session "+session+" -- ")
	require.Nil(t, run.NativeSessionID, "the session is recorded from OpenCode's own events")
	require.Contains(t, run.Prompt, "this continues your previous session")

	_, fresh := followUpAttempt(t, "opencode", func(uuid.UUID) *domain.AgentHandoff {
		return &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: uuid.New(), NativeSessionID: session, Provider: "opencode"}
	})
	require.NoError(t, json.Unmarshal(fresh.CommandArgv, &argv))
	require.NotContains(t, argv, "--session", "the store of another machine is not here")
	require.Contains(t, fresh.Prompt, "the previous session cannot be resumed")
}

// A second attempt of Qwen Code, Kimi or Goose on the same machine after a
// succeeded one continues the session the first one ran in, in the task's
// session directory, and its handoff tells the agent so. Qwen Code's
// session is recorded from its own events; Kimi's and Goose's is the one
// Kuayle named, recorded when the run is created.
func TestQwenKimiAndGooseFollowUpsContinueTheSessionOfASucceededAttempt(t *testing.T) {
	const session = "258fdb71-4e37-4a51-adb1-415e3ff0af9c"
	for provider, continues := range map[string]string{
		"qwen-code": "qwen --yolo --output-format stream-json --resume " + session + " ",
		"kimi":      "kimi --print --yolo --output-format stream-json --session " + session + " --prompt ",
		"goose":     "goose run --resume --name " + session + " --quiet --output-format stream-json --text ",
	} {
		task, run := followUpAttempt(t, provider, func(machine uuid.UUID) *domain.AgentHandoff {
			return &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: machine, NativeSessionID: session, Provider: provider}
		})
		var argv []string
		require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
		require.Equal(t, "kuayle-session", argv[3], provider)
		require.Equal(t, "/workspace/.kuayle/agent-sessions/"+task.ID.String(), argv[4], provider)
		require.Contains(t, strings.Join(argv, " "), continues, provider)
		require.Contains(t, run.Prompt, "this continues your previous session", provider)
		if provider == "qwen-code" {
			require.Nil(t, run.NativeSessionID, "the session is recorded from Qwen Code's own events")
		} else {
			require.NotNil(t, run.NativeSessionID, provider)
			require.Equal(t, session, *run.NativeSessionID, provider)
		}
	}
}

// Kimi and Goose start a task's first session under an ID Kuayle names and
// records with the run, so the next attempt and the closing continuation
// find it without reading their output.
func TestKimiAndGooseStartTheSessionKuayleNamed(t *testing.T) {
	for provider, starts := range map[string]string{"kimi": "--session ", "goose": "goose run --name "} {
		_, run := followUpAttempt(t, provider, func(uuid.UUID) *domain.AgentHandoff { return nil })
		require.NotNil(t, run.NativeSessionID, provider)
		require.True(t, agent.ValidSessionID(*run.NativeSessionID), provider)
		var argv []string
		require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
		require.Contains(t, strings.Join(argv, " "), starts+*run.NativeSessionID+" ", provider)
		require.NotContains(t, argv, "--resume", provider)
		require.NotContains(t, argv, "--no-session", provider)
	}
}

// A task whose profile switched harness between attempts on the same
// machine starts a fresh session: the previous attempt's session belongs
// to the other harness, which stored it, and its ID means nothing to this
// one. That holds for a paused task and for an attempt started for a
// person's message too, which otherwise continue the previous session.
func TestAnotherHarnessNeverContinuesThePreviousAttemptsSession(t *testing.T) {
	const session = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	for provider, other := range map[string]string{"qwen-code": "codex", "claude-code": "codex", "codex": "claude-code", "opencode": "qwen-code", "kimi": "goose", "goose": "kimi"} {
		_, run := followUpAttempt(t, provider, func(machine uuid.UUID) *domain.AgentHandoff {
			return &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: machine, NativeSessionID: session, Provider: other}
		})
		var argv []string
		require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
		require.NotContains(t, strings.Join(argv, " "), session, provider)
		require.NotContains(t, argv, "--resume", provider)
		require.Contains(t, run.Prompt, "the previous session cannot be resumed", provider)
		if agent.KuayleNamesSession(provider) {
			require.NotNil(t, run.NativeSessionID, provider)
			require.NotEqual(t, session, *run.NativeSessionID, "a new session, named for this harness")
		}
	}

	machine := uuid.New()
	generation := int64(2)
	for name, task := range map[string]domain.AgentTask{
		"paused":            {ID: uuid.New(), Provider: "qwen-code", MachineID: &machine, Generation: 2, ResumeNativeSession: true},
		"for a message":     {ID: uuid.New(), Provider: "qwen-code", MachineID: &machine, Generation: 2, LiveResumeGeneration: &generation},
		"interrupted":       {ID: uuid.New(), Provider: "qwen-code", MachineID: &machine, Generation: 2, ResumeNativeSession: true, LiveResumeGeneration: &generation},
		"continued by hand": {ID: uuid.New(), Provider: "qwen-code", MachineID: &machine, Generation: 2},
	} {
		previous := &domain.AgentHandoff{Status: "cancelled", MachineID: machine, NativeSessionID: session, Provider: "claude-code", ContinuedInteractively: true}
		require.Empty(t, taskSession(task, previous).ResumeID, name)
		previous.Provider = "qwen-code"
		require.Equal(t, session, taskSession(task, previous).ResumeID, name+": the same harness continues")
	}
}
