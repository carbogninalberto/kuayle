package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The final failed state keeps a resource limit reason instead of the
// generic cleanup message, so the task says what to change.
func TestFailedTaskKeepsItsResourceLimitReason(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	reason := domain.MemoryLimitMessage("agent", 1503238553, true)
	require.NoError(t, repo.Fail(ctx, *claimed, reason))
	require.NoError(t, repo.Release(ctx, *claimed))
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	cleanup, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	require.NoError(t, repo.FinishCancellation(ctx, *cleanup, "Cleanup confirmed"))
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State)
	require.Equal(t, reason, current.Reason)
}
