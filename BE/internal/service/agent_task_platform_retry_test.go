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
			require.Contains(t, store.platformReason, "The workflow iteration budget is unchanged")
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
	require.Contains(t, store.failReason, "after 3 platform retries")
	require.Contains(t, store.failReason, "machine manager restarted")
	require.Contains(t, store.failReason, "Retry the task to continue")
}

func TestAgentTaskRetriesPIDLimitWithoutAdvancingWorkflow(t *testing.T) {
	for _, retries := range []int{0, domain.MaxPlatformRetries} {
		workflow := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 8})
		workflow.w.task.WorkflowIterations = 8
		workflow.w.task.PlatformRetries = retries
		task := workflow.w.task
		store := &platformRetryStoreFake{taskStoreFake: &taskStoreFake{task: task}}
		runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{
			Status:       domain.DevMachineAgentRunStatusFailed,
			FailureClass: ptr(string(domain.FailurePlatform)),
			FailureRule:  ptr(domain.FailureRulePIDLimit),
			FailureCause: ptr("The agent container reached its limit of 1024 processes and threads (PID limit) (exit code 139, SIGSEGV)"),
		}}
		svc := &AgentTaskService{store: store, runtime: runtime, workflow: workflow.runner}
		require.NoError(t, svc.step(context.Background(), task))
		require.Equal(t, task.WorkflowSequence, workflow.w.task.WorkflowSequence)
		require.Equal(t, task.WorkflowIterations, workflow.w.task.WorkflowIterations)
		require.Nil(t, workflow.w.steps[task.WorkflowSequence].Outcome, "the workflow does not receive an agent_failed outcome")
		require.Equal(t, 1, store.platformCalls)
		if retries < domain.MaxPlatformRetries {
			require.True(t, store.retried)
			require.Contains(t, store.platformReason, "PID limit")
			require.Contains(t, store.platformReason, "retry 1 of 3")
			require.Contains(t, store.platformReason, "The workflow iteration budget is unchanged")
			require.Empty(t, store.failReason)
		} else {
			require.False(t, store.retried)
			require.Equal(t, domain.FailurePlatform, store.failClass)
			require.Contains(t, store.failReason, "PID limit")
			require.Contains(t, store.failReason, "stopped retrying")
		}
	}
}

func TestAgentTaskDoesNotRetryOtherPlatformFailures(t *testing.T) {
	message := "agent_failed: exit 1"
	for name, failure := range map[string]struct{ class, rule string }{
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

func TestPIDLimitAttemptIsToldHowToReduceProcessCount(t *testing.T) {
	for _, resumed := range []bool{true, false} {
		prompt := withHandoff("Implement", &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed", FailureRule: domain.FailureRulePIDLimit}, resumed, resumed)
		require.Contains(t, prompt, "PID limit")
		require.Contains(t, prompt, "--workers=1")
		require.Contains(t, prompt, "continue from the worktree")
		require.NotContains(t, prompt, "usage limit")
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
