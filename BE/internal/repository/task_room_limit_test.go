package repository

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A role over its hourly room limit is told at its next turn (KUA-315, the
// v0.4.0 liveness seed 4): its first post over the limit is kept as a
// refused post, which reaches it as any refusal does; later posts in the
// hour are dropped without another note. The limit itself is unchanged.
func TestARoleOverItsRoomLimitIsToldOnce(t *testing.T) {
	f := newRoomFixture(t)
	ctx := context.Background()
	for range domain.AgentMessagesPerHour {
		_, err := f.repo.RecordRoomMessage(ctx, f.task.ID, f.member("reviewer", domain.RoomSay, "Note", domain.TaskRoomData{}))
		require.NoError(t, err)
	}
	told, err := f.repo.RecordRoomMessage(ctx, f.task.ID, f.member("reviewer", domain.RoomAsk, "Is the toggle persisted?", domain.TaskRoomData{}))
	require.NoError(t, err)
	require.NotNil(t, told.RejectReason)
	require.Contains(t, *told.RejectReason, "hourly limit")
	_, err = f.repo.RecordRoomMessage(ctx, f.task.ID, f.member("reviewer", domain.RoomSay, "And another", domain.TaskRoomData{}))
	require.ErrorIs(t, err, ErrTaskRoomRateLimited)

	inbox, err := f.repo.MemberInbox(ctx, f.task.ID, "reviewer")
	require.NoError(t, err)
	prompt := domain.RoomTurnPrompt("reviewer", inbox)
	require.Contains(t, prompt, "was refused")
	require.Contains(t, prompt, "hourly limit")
	require.NotContains(t, prompt, "And another", "a dropped post is not kept")
}
