package machine

import (
	"context"
	"errors"
	"time"

	"github.com/kuayle/kuayle-backend/internal/repository"
)

// Agent output writes are idempotent (keyed by stream and sequence), so a
// short database stall is retried instead of killing a run that may have
// worked for an hour. Authorization loss and content conflicts are final.
var (
	agentWriteAttemptTimeout = 15 * time.Second
	agentWriteBudget         = 3 * time.Minute
	agentWriteBackoff        = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second}
)

func retryAgentWrite(ctx context.Context, write func(context.Context) error) error {
	deadline := time.Now().Add(agentWriteBudget)
	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, agentWriteAttemptTimeout)
		err := write(attemptCtx)
		cancel()
		if err == nil || !retryableAgentWrite(err) || ctx.Err() != nil {
			return err
		}
		wait := agentWriteBackoff[min(attempt, len(agentWriteBackoff)-1)]
		if time.Now().Add(wait).After(deadline) {
			return err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

func retryableAgentWrite(err error) bool {
	return !errors.Is(err, repository.ErrAgentTaskUnauthorized) &&
		!errors.Is(err, repository.ErrAgentOutputConflict) &&
		!errors.Is(err, context.Canceled)
}
