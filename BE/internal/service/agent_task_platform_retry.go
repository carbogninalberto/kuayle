package service

import (
	"context"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// retryAfterPlatformFailure retries the same step of a run that failed for a
// retryable platform cause (the machine manager restarted): a new attempt of
// the same visit and iteration, which never uses the workflow's budget. Once
// the step had domain.MaxPlatformRetries automatic retries the task fails with
// the platform reason instead.
func (s *AgentTaskService) retryAfterPlatformFailure(ctx context.Context, task domain.AgentTask, run *domain.DevMachineAgentRun) error {
	reason := fmt.Sprintf("Retrying automatically: Kuayle's machine manager restarted while the agent worked (retry %d of %d); the workflow budget is unchanged", task.PlatformRetries+1, domain.MaxPlatformRetries)
	retried, err := s.store.BeginPlatformRetry(ctx, task, reason)
	if err != nil {
		return err
	}
	if retried {
		return nil
	}
	cause := fmt.Sprintf("Kuayle's machine manager restarted %d times during this step, so Kuayle stopped retrying it automatically; retry the task to continue", domain.MaxPlatformRetries)
	return s.store.FailWithClass(ctx, task, domain.FailurePlatform, domain.TaskFailureReason(domain.FailurePlatform, cause))
}
