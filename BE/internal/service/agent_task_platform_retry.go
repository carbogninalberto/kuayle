package service

import (
	"context"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// retryAfterPlatformFailure retries the same step of a run that failed for a
// retryable platform cause. The new attempt stays in the same workflow visit
// and iteration. At the automatic retry cap the task fails with the platform
// reason.
func (s *AgentTaskService) retryAfterPlatformFailure(ctx context.Context, task domain.AgentTask, run *domain.DevMachineAgentRun) error {
	interruption := "Kuayle's machine manager restarted while the agent worked"
	advice := "Retry the task to continue"
	if *run.FailureRule == domain.FailureRulePIDLimit {
		interruption = "the agent container reached its PID limit"
		advice = "Use fewer parallel workers when retrying the task"
	}
	reason := fmt.Sprintf("Retrying automatically: %s (retry %d of %d). The workflow iteration budget is unchanged", interruption, task.PlatformRetries+1, domain.MaxPlatformRetries)
	retried, err := s.store.BeginPlatformRetry(ctx, task, reason)
	if err != nil {
		return err
	}
	if retried {
		return nil
	}
	cause := fmt.Sprintf("Kuayle stopped retrying this step automatically after %d platform retries. The latest failure occurred when %s. %s", domain.MaxPlatformRetries, interruption, advice)
	return s.store.FailWithClass(ctx, task, domain.FailurePlatform, domain.TaskFailureReason(domain.FailurePlatform, cause))
}
