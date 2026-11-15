package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// platformRetryStoreFake records the automatic retries a platform failure asks
// for, and accepts while the step is under its cap.
type platformRetryStoreFake struct {
	*taskStoreFake
	retried        bool
	platformReason string
	platformCalls  int
}

func (f *platformRetryStoreFake) BeginPlatformRetry(_ context.Context, task domain.AgentTask, reason string) (bool, error) {
	f.platformCalls++
	f.platformReason = reason
	if task.PlatformRetries >= domain.MaxPlatformRetries {
		return false, nil
	}
	f.retried = true
	f.task.State = "cancelling"
	return true, nil
}

// A run stopped by a machine manager restart retries the same step instead of
// failing it or following a workflow's "agent failed" edge.
func TestAgentTaskRetriesAPlatformFailureOnTheSameStep(t *testing.T) {
	class, rule := string(domain.FailurePlatform), domain.FailureRuleManagerRestart
	message := "exit status 137"
	for name, task := range map[string]domain.AgentTask{
		"plain task":    {State: "running", Provider: "claude-code"},
		"workflow task": {State: "running", Provider: "claude-code", WorkflowVersionID: ptr(uuid.New())},
	} {
		t.Run(name, func(t *testing.T) {
			store := &platformRetryStoreFake{taskStoreFake: &taskStoreFake{task: task}}
			runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{
				Status:       domain.DevMachineAgentRunStatusFailed,
				FailureClass: &class,
				FailureRule:  &rule,
				ErrorMessage: &message,
			}}
			// A workflow that would have consumed the budget is never consulted.
			svc := &AgentTaskService{store: store, runtime: runtime}
			require.NoError(t, svc.step(context.Background(), store.task))
			require.Equal(t, 1, store.platformCalls, "the step is retried automatically")
			require.True(t, store.retried)
			require.Contains(t, store.platformReason, "retry 1 of 3")
			require.Contains(t, store.platformReason, "workflow budget is unchanged")
			require.Empty(t, store.failReason, "a retry is not a failure")
			require.Empty(t, store.failClass)
			require.Equal(t, "cancelling", store.task.State, "the runtime is cleaned up first")
		})
	}
}

// After MaxPlatformRetries automatic retries during this step the task fails,
// saying why Kuayle stopped retrying.
func TestAgentTaskFailsAfterThePlatformRetryCap(t *testing.T) {
	class, rule := string(domain.FailurePlatform), domain.FailureRuleManagerRestart
	message := "exit status 137"
	store := &platformRetryStoreFake{taskStoreFake: &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "claude-code", PlatformRetries: domain.MaxPlatformRetries}}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{
		Status:       domain.DevMachineAgentRunStatusFailed,
		FailureClass: &class,
		FailureRule:  &rule,
		ErrorMessage: &message,
	}}
	require.NoError(t, (&AgentTaskService{store: store, runtime: runtime}).step(context.Background(), store.task))
	require.Equal(t, 1, store.platformCalls)
	require.False(t, store.retried)
	require.Equal(t, domain.FailurePlatform, store.failClass)
	require.Contains(t, store.failReason, "3 times during this step")
	require.Contains(t, store.failReason, "retry the task to continue")
}

// Only a manager restart is retried automatically: a resource limit or an
// unclassified kill would happen again, so the task fails as before.
func TestAgentTaskDoesNotRetryOtherPlatformFailures(t *testing.T) {
	message := "agent_failed: exit 1"
	for name, failure := range map[string]struct{ class, rule string }{
		"pid limit":     {string(domain.FailurePlatform), "pid_limit"},
		"out of memory": {string(domain.FailurePlatform), "out_of_memory"},
		"killed":        {string(domain.FailureUnknown), "killed"},
	} {
		t.Run(name, func(t *testing.T) {
			class, rule := failure.class, failure.rule
			store := &platformRetryStoreFake{taskStoreFake: &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "claude-code"}}}
			runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{
				Status:       domain.DevMachineAgentRunStatusFailed,
				FailureClass: &class,
				FailureRule:  &rule,
				ErrorMessage: &message,
			}}
			require.NoError(t, (&AgentTaskService{store: store, runtime: runtime}).step(context.Background(), store.task))
			require.Zero(t, store.platformCalls, "nothing is retried automatically")
			require.NotEmpty(t, store.failReason, "the task fails as before")
			require.Equal(t, domain.FailureClass(class), store.failClass)
		})
	}
}

// The successor attempt is told the restart was Kuayle's, so it continues the
// work in the worktree instead of starting over.
func TestRestartedAttemptIsToldToContinueItsWork(t *testing.T) {
	prompt := withHandoff("Implement KUA-61", &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed", FailureRule: domain.FailureRuleManagerRestart}, true, true)
	require.Contains(t, prompt, "machine manager restarted")
	require.Contains(t, prompt, "do not start over")
	require.NotContains(t, prompt, "usage limit")
	// An ordinary continuation keeps the provider's usage limit as the cause.
	limit := withHandoff("Implement KUA-61", &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed"}, true, true)
	require.Contains(t, limit, "the provider's usage limit")
}
