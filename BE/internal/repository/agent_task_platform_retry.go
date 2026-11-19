package repository

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// BeginPlatformRetry ends the claimed attempt, whose run failed for a
// retryable platform cause, for an automatic retry of the same step: the
// runtime is cleaned up as for a failure (state cancelling), and
// FinishCancellation then queues a new attempt of the same step instead of
// failing. It returns false, changing nothing, when the step already had
// MaxPlatformRetries automatic retries; the caller then fails the task.
func (r *AgentTaskRepository) BeginPlatformRetry(ctx context.Context, claimed domain.AgentTask, reason string) (bool, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return false, err
	}
	// Bound retries so repeated platform failures cannot restart a step forever.
	if task.PlatformRetries >= domain.MaxPlatformRetries {
		return false, nil
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET platform_retries=platform_retries+1,platform_retry_pending=true WHERE id=$1 RETURNING *`, task.ID); err != nil {
		return false, err
	}
	if err = noteNotebook(ctx, tx, task.ID, nil, domain.NotebookNote{Event: reason}); err != nil {
		return false, err
	}
	// A retry is not a failure: the task keeps no failure class.
	if err = beginAgentTaskFailure(ctx, tx, task, "", reason); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
