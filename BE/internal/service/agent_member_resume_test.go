package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// reviewTurn runs the second attempt of a review task on the harness, after
// a succeeded first one on the same machine in session; member says whether
// the task runs the turns of a pipeline task's member. It returns the run
// Kuayle created and the store it asked.
func reviewTurn(t *testing.T, provider, session string, member bool) (domain.DevMachineAgentRun, *taskRuntimeStoreFake) {
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
	root := uuid.New()
	// The previous turn ran on the same harness (AgentHandoff.Provider, which
	// PreviousAttempt reads from the run): only it can continue the session.
	tasks := &taskRuntimeStoreFake{previous: &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: machine, Provider: provider, NativeSessionID: session}}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, MachineID: &machine, CheckoutID: checkout, RunID: uuid.New(), State: "preparing", Provider: provider,
		Prompt: "You are the @reviewer of the team working on this task.", Kind: domain.AgentTaskKindReview, ReviewOfTaskID: &root, Generation: 2}
	if member {
		tasks.member = &domain.TaskMember{ID: uuid.New(), WorkspaceID: ws, TaskID: root, Role: "reviewer", MemberTaskID: &task.ID, State: domain.MemberQueued}
	}
	runtime := &devMachineTaskRuntime{machines: machines, tasks: tasks}
	_, err := runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Len(t, store.agentRuns, 1)
	return store.agentRuns[0], tasks
}

// A member's next turn is the next generation of its review task, and it
// continues the session of its previous turn on Claude Code, Codex and
// OpenCode alike, with the handoff that says so.
func TestAMembersNextTurnResumesItsSession(t *testing.T) {
	for provider, session := range map[string]string{
		"claude-code": "0c9a6c1e-0000-4000-8000-000000000001",
		"codex":       "0199a213-81c0-7800-8aa1-bbab2a035a53",
		"opencode":    "ses_df21986abffefn2FxDuH6uZGLA",
	} {
		t.Run(provider, func(t *testing.T) {
			run, tasks := reviewTurn(t, provider, session, true)
			require.Equal(t, 1, tasks.previousCalls)
			var argv []string
			require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
			require.Contains(t, strings.Join(argv, " "), session, "the launch carries the resume id")
			require.Contains(t, run.Prompt, "this continues your previous session")
			require.Contains(t, run.Prompt, "You are the @reviewer")
			require.False(t, run.PushBranch, "a member that reviews never pushes")
		})
	}
}

// Every other review (one a graph workflow's reviewer step or the assistant
// requested) keeps starting cold: Kuayle does not even read its previous
// attempt.
func TestOtherReviewsStillStartCold(t *testing.T) {
	const session = "0c9a6c1e-0000-4000-8000-000000000001"
	run, tasks := reviewTurn(t, "claude-code", session, false)
	require.Zero(t, tasks.previousCalls)
	var argv []string
	require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
	require.NotContains(t, strings.Join(argv, " "), session)
	require.NotContains(t, run.Prompt, "previous attempt")
}

// A member's next turn reads a commit published after its worktree was
// made, so its worktree fetches again first, once per turn; its first turn
// and any other task's checkout are used as they are.
func TestAMembersNextTurnFetchesItsWorktreeAgain(t *testing.T) {
	ws, user, machineID, issueID, repoID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, IssueID: issueID, MachineID: &machineID, CheckoutID: uuid.New(), Kind: domain.AgentTaskKindReview, Generation: 1}
	store := &devMachineStoreFake{
		policy:        testPolicy(ws),
		machine:       &domain.DevMachine{ID: machineID, WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		issues:        map[uuid.UUID]*domain.Issue{issueID: {ID: issueID, WorkspaceID: ws, TeamID: uuid.New(), Identifier: "ENG-7"}},
		reposByID:     map[uuid.UUID]*domain.GitHubRepoModel{repoID: {ID: repoID, WorkspaceID: ws, FullName: "kuayle/api", DefaultBranch: "main", IsActive: true}},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{scopeKey(nil, nil, &issueID): {WorkspaceID: ws, GitHubRepoID: &repoID}},
	}
	svc := newTestDevMachineService(store)
	first, err := svc.checkoutTask(context.Background(), task, false)
	require.NoError(t, err)
	store.checkouts[0].Status = "ready"
	store.createCheckoutCalled = false
	again, err := svc.checkoutTask(context.Background(), task, false)
	require.NoError(t, err)
	require.Equal(t, "ready", again.Status)
	require.False(t, store.createCheckoutCalled, "the first turn reads the worktree it made")

	task.Generation = 2
	refreshed, err := svc.checkoutTask(context.Background(), task, true)
	require.NoError(t, err)
	require.True(t, store.createCheckoutCalled, "the next turn fetches the worktree again")
	require.Equal(t, "queued", refreshed.Status)
	require.Equal(t, first.WorkspacePath, refreshed.WorkspacePath, "the same worktree, so its session finds it")
	require.Equal(t, fmt.Sprintf("task-checkout-retry:%s:2", task.ID), store.checkoutOperation.IdempotencyKey)

	// Fetched once for the turn: the turn's later steps use it as it is.
	store.checkouts[0].Status = "ready"
	store.createCheckoutCalled = false
	store.queuedOperation = store.checkoutOperation
	store.queuedOperation.Status = domain.DevMachineOpStatusCompleted
	ready, err := svc.checkoutTask(context.Background(), task, true)
	require.NoError(t, err)
	require.Equal(t, "ready", ready.Status)
	require.False(t, store.createCheckoutCalled)

}

// Only a pipeline member's next turn fetches its worktree again: a review a
// graph workflow or the assistant asked for, retried, and an implementing
// task's next iteration keep theirs as they are.
func TestOnlyAMembersNextTurnRefreshesItsWorktree(t *testing.T) {
	member := &domain.TaskMember{ID: uuid.New(), Role: "reviewer"}
	for name, c := range map[string]struct {
		task   domain.AgentTask
		member *domain.TaskMember
		want   bool
	}{
		"a member's next turn":          {domain.AgentTask{Kind: domain.AgentTaskKindReview, Generation: 2}, member, true},
		"a member's first turn":         {domain.AgentTask{Kind: domain.AgentTaskKindReview, Generation: 1}, member, false},
		"a retried requested review":    {domain.AgentTask{Kind: domain.AgentTaskKindReview, Generation: 2}, nil, false},
		"an implementing iteration":     {domain.AgentTask{Kind: domain.AgentTaskKindImplement, Generation: 3}, nil, false},
		"a pipeline writer's next turn": {domain.AgentTask{Kind: domain.AgentTaskKindImplement, Generation: 3, Pipeline: true}, nil, false},
	} {
		runtime := &devMachineTaskRuntime{tasks: &taskRuntimeStoreFake{member: c.member}}
		refresh, err := runtime.checkoutRefresh(context.Background(), c.task)
		require.NoError(t, err)
		require.Equal(t, c.want, refresh, name)
	}
}
