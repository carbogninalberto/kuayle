package repository

import (
	"context"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestScheduledJobLeaseAndCancelFence(t *testing.T) {
	f := newTaskWatcherFixture(t)
	r := NewConversationRepository(f.db)
	ctx := domain.WithAccess(f.ctx, domain.Access{WorkspaceID: f.workspace, UserID: f.user})
	var id string
	require.NoError(t, f.db.Get(&id, `INSERT INTO conversation_jobs(workspace_id,user_id,conversation_id,parent_id,kind,spec,instructions,action,next_run_at,deadline) VALUES($1,$2,$3,$4,'once','{}','Reminder','notify',now(),now()+interval '1 day') RETURNING id`, f.workspace, f.user, f.conversation, f.message))
	// A different API process may still hold the candidate row in its transaction.
	locked, err := f.db.Beginx()
	require.NoError(t, err)
	defer locked.Rollback()
	_, err = locked.Exec(`SELECT id FROM conversation_jobs WHERE id=$1 FOR UPDATE`, id)
	require.NoError(t, err)
	claimCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	skipped, err := r.ClaimScheduledJob(claimCtx, time.Minute)
	require.NoError(t, err)
	require.Nil(t, skipped)
	require.NoError(t, locked.Rollback())
	first, err := r.ClaimScheduledJob(ctx, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	duplicate, err := r.ClaimScheduledJob(ctx, time.Minute)
	require.NoError(t, err)
	require.Nil(t, duplicate)
	_, err = f.db.Exec(`UPDATE conversation_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, id)
	require.NoError(t, err)
	second, err := r.ClaimScheduledJob(ctx, time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, first.LeaseToken, second.LeaseToken)
	_, err = r.FinishScheduledJob(ctx, *first, JobOutcome{State: "completed", Notice: "stale"})
	require.ErrorIs(t, err, ErrConversationConflict)
	_, changed, err := r.CancelScheduledJob(ctx, f.workspace, f.conversation, second.ID)
	require.NoError(t, err)
	require.True(t, changed)
	_, changed, err = r.CancelScheduledJob(ctx, f.workspace, f.conversation, second.ID)
	require.NoError(t, err)
	require.False(t, changed)
	_, err = r.FinishScheduledJob(ctx, *second, JobOutcome{State: "completed", Notice: "late"})
	require.ErrorIs(t, err, ErrConversationConflict)
}
