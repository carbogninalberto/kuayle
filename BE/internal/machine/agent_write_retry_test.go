package machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func fastAgentWriteRetry(t *testing.T, budget time.Duration) {
	t.Helper()
	oldTimeout, oldBudget, oldBackoff := agentWriteAttemptTimeout, agentWriteBudget, agentWriteBackoff
	agentWriteAttemptTimeout, agentWriteBudget, agentWriteBackoff = 20*time.Millisecond, budget, []time.Duration{time.Millisecond}
	t.Cleanup(func() {
		agentWriteAttemptTimeout, agentWriteBudget, agentWriteBackoff = oldTimeout, oldBudget, oldBackoff
	})
}

func TestRetryAgentWriteSurvivesADatabaseStall(t *testing.T) {
	fastAgentWriteRetry(t, time.Second)
	calls := 0
	err := retryAgentWrite(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 4 {
			<-ctx.Done() // the database does not answer in time
			return ctx.Err()
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 4, calls)
}

func TestRetryAgentWriteStopsOnFinalErrors(t *testing.T) {
	fastAgentWriteRetry(t, time.Second)
	for _, final := range []error{repository.ErrAgentTaskUnauthorized, repository.ErrAgentOutputConflict} {
		calls := 0
		err := retryAgentWrite(context.Background(), func(context.Context) error { calls++; return final })
		require.ErrorIs(t, err, final)
		require.Equal(t, 1, calls)
	}
}

func TestRetryAgentWriteGivesUpAfterItsBudget(t *testing.T) {
	fastAgentWriteRetry(t, 30*time.Millisecond)
	stall := errors.New("connection reset")
	calls := 0
	err := retryAgentWrite(context.Background(), func(context.Context) error { calls++; return stall })
	require.ErrorIs(t, err, stall)
	require.Greater(t, calls, 1)
}

func TestRetryAgentWriteStopsWhenTheRunEnds(t *testing.T) {
	fastAgentWriteRetry(t, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryAgentWrite(ctx, func(context.Context) error { calls++; cancel(); return errors.New("slow") })
	require.Error(t, err)
	require.Equal(t, 1, calls)
}
