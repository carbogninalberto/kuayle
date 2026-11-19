package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A run stopped by a machine manager restart is cleaned up as a failure, and
// the cleanup then starts a new attempt of the same step instead of failing
// the task: the counters of the step stay where they were.
func TestPlatformRetryRestartsTheSameStep(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, task.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Transition(ctx, *claimed, "running", "Running"))
	_, err = db.Exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	claimed, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)

	retried, err := repo.BeginPlatformRetry(ctx, *claimed, "Retrying automatically: Kuayle's machine manager restarted while the agent worked (retry 1 of 3); the workflow budget is unchanged")
	require.NoError(t, err)
	require.True(t, retried)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", current.State, "the runtime is cleaned up first, as for a failure")
	require.Equal(t, 1, current.PlatformRetries)
	require.True(t, current.PlatformRetryPending)
	require.True(t, current.FailurePending)
	require.Nil(t, current.FailureClass, "a platform retry is not a failure")
	require.Contains(t, current.Reason, "Retrying automatically")

	require.NoError(t, repo.FinishCancellation(ctx, *current, "Runtime cancellation confirmed"))
	restarted, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", restarted.State, "the same step runs again")
	require.Equal(t, claimed.Generation+1, restarted.Generation)
	require.NotEqual(t, claimed.RunID, restarted.RunID, "the new attempt has its own run")
	require.Nil(t, restarted.FailureClass)
	require.False(t, restarted.FailurePending)
	require.False(t, restarted.PlatformRetryPending)
	require.Equal(t, 1, restarted.PlatformRetries, "the step keeps its count across attempts")
}

// A person's cancel during a platform retry wins: the runtime is cleaned up
// and the task ends cancelled instead of the step running again.
func TestCancelDuringAPlatformRetryEndsTheTaskCancelled(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, task.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Transition(ctx, *claimed, "running", "Running"))
	_, err = db.Exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	claimed, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)

	retried, err := repo.BeginPlatformRetry(ctx, *claimed, "Retrying automatically: Kuayle's machine manager restarted while the agent worked (retry 1 of 3); the workflow budget is unchanged")
	require.NoError(t, err)
	require.True(t, retried)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", current.State)

	require.NoError(t, repo.RequestCancel(ctx, task.WorkspaceID, task.ID))
	cancelling, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", cancelling.State, "the runtime is still cleaned up first")
	require.False(t, cancelling.PlatformRetryPending, "a person's cancel wins over the retry")
	require.False(t, cancelling.FailurePending)
	require.Equal(t, "Cancellation requested", cancelling.Reason)

	require.NoError(t, repo.FinishCancellation(ctx, *cancelling, "Runtime cancellation confirmed"))
	ended, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", ended.State)
	require.Equal(t, claimed.Generation, ended.Generation, "no new attempt")
	require.Equal(t, claimed.RunID, ended.RunID)
	require.False(t, ended.PlatformRetryPending)
	require.False(t, ended.FailurePending)
	require.Nil(t, ended.FailureClass)
}

// The automatic retries are capped: the step that already had
// MaxPlatformRetries of them is not retried again, and nothing changes.
func TestPlatformRetryStopsAtTheCap(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Transition(ctx, *claimed, "running", "Running"))
	_, err = db.Exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds',platform_retries=$2 WHERE id=$1`, task.ID, domain.MaxPlatformRetries)
	require.NoError(t, err)
	claimed, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)

	retried, err := repo.BeginPlatformRetry(ctx, *claimed, "Retrying automatically")
	require.NoError(t, err)
	require.False(t, retried, "the caller fails the task instead")
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "running", current.State, "nothing changed")
	require.False(t, current.FailurePending)
	require.False(t, current.PlatformRetryPending)
	require.Equal(t, domain.MaxPlatformRetries, current.PlatformRetries)
	require.Equal(t, claimed.Generation, current.Generation)
	require.Equal(t, claimed.RunID, current.RunID)
}

// Retrying a PID-limit crash preserves the workflow position and counters.
func TestPIDLimitRetryKeepsTheWorkflowPosition(t *testing.T) {
	f := newPauseFixture(t)
	f.exec(`UPDATE agent_tasks SET workflow_iterations=8 WHERE id=$1`, f.task.ID)
	before := f.current()
	require.NotNil(t, before.WorkflowNode)
	f.exec(`UPDATE dev_machine_agent_runs SET status='failed',failure_class='platform',failure_rule='pid_limit' WHERE id=$1`, before.RunID)
	claimed := f.claim()
	require.NotNil(t, claimed)

	reason := "Retrying automatically: the agent container reached its PID limit (retry 1 of 3). The workflow iteration budget is unchanged"
	retried, err := f.repo.BeginPlatformRetry(f.ctx, *claimed, reason)
	require.NoError(t, err)
	require.True(t, retried)
	retrying := f.current()
	require.Equal(t, "cancelling", retrying.State)
	require.Equal(t, 1, retrying.PlatformRetries)
	require.True(t, retrying.PlatformRetryPending)
	notebook, err := f.repo.Notebook(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.NotEmpty(t, notebook.Notebook.Events)
	require.Equal(t, reason, notebook.Notebook.Events[len(notebook.Notebook.Events)-1].Text)

	// The runtime is cleaned up, then the cleanup restarts the step.
	require.NoError(t, f.repo.CancelAttempt(f.ctx, retrying))
	f.exec(`UPDATE dev_machine_operations SET status='completed' WHERE agent_run_id=$1 AND action='cancel_agent'`, retrying.RunID)
	require.NoError(t, f.repo.FinishCancellation(f.ctx, retrying, "Runtime cancellation confirmed"))
	restarted := f.current()
	require.Equal(t, "queued", restarted.State)
	require.Equal(t, before.Generation+1, restarted.Generation)
	require.NotEqual(t, before.RunID, restarted.RunID)
	require.Nil(t, restarted.FailureClass)
	require.False(t, restarted.PlatformRetryPending)
	require.Equal(t, 1, restarted.PlatformRetries)
	require.Equal(t, before.WorkflowSequence, restarted.WorkflowSequence)
	require.Equal(t, *before.WorkflowNode, *restarted.WorkflowNode)
	require.JSONEq(t, string(before.WorkflowVisits), string(restarted.WorkflowVisits), "no visit is used")
	require.Equal(t, before.WorkflowIterations, restarted.WorkflowIterations, "no iteration is used")
	var steps int
	require.NoError(t, f.db.Get(&steps, `SELECT count(*) FROM agent_task_workflow_steps WHERE task_id=$1`, f.task.ID))
	require.Equal(t, 2, steps, "a retry enters no step")
	step := f.step(restarted.WorkflowSequence)
	require.EqualValues(t, restarted.Generation, *step.Generation, "the open step follows the new attempt")
	require.Nil(t, step.FinishedAt)

	handoff, err := f.repo.PreviousAttempt(f.ctx, restarted)
	require.NoError(t, err)
	require.NotNil(t, handoff)
	require.Equal(t, before.RunID, handoff.AttemptID)
	require.Equal(t, domain.FailureRulePIDLimit, handoff.FailureRule, "the successor knows the cause of the crash")
}
