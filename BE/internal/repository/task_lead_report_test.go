package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestLeadSummaryNotifiesOnlyItsOwnerOnce(t *testing.T) {
	f := newMemberFixture(t)
	ctx := context.Background()
	other := newEnvironmentRaceFixture(t, f.db)
	configure := func(ws, user uuid.UUID) {
		t.Helper()
		_, err := f.db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,(random()*1e12)::bigint+1,'x','x',true,'{12345}')`, ws)
		require.NoError(t, err)
		_, err = f.db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,12345)`, ws, user)
		require.NoError(t, err)
	}
	// The same owner has a linked bot in another workspace.
	_, err := f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, other.workspaceID, f.task.CreatorID)
	require.NoError(t, err)
	configure(other.workspaceID, f.task.CreatorID)
	configure(f.task.WorkspaceID, f.task.CreatorID)
	post := func(role, key, body string) domain.TaskMessage {
		t.Helper()
		p := f.member(role, domain.RoomSummary, body, domain.TaskRoomData{})
		p.DedupeKey = key
		msg, err := f.repo.RecordRoomMessage(ctx, f.task.ID, p)
		require.NoError(t, err)
		return *msg
	}
	msg := post("lead", "first", "CI passed.")
	require.Nil(t, msg.RejectReason)
	require.Equal(t, msg.ID, post("lead", "first", "CI passed.").ID)
	require.NotNil(t, post("reviewer", "review", "Reviewer report").RejectReason)
	_, err = f.repo.RecordRoomMessage(ctx, f.task.ID, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomSummary, Body: "Waiting on CI."})
	require.NoError(t, err)
	bad := f.member("lead", domain.RoomSummary, "Undeliverable report", domain.TaskRoomData{})
	bad.ToRoles = []string{"unknown"}
	refused, err := f.repo.RecordRoomMessage(ctx, f.task.ID, bad)
	require.NoError(t, err)
	require.NotNil(t, refused.RejectReason)

	_, err = f.db.Exec(`INSERT INTO user_preferences(user_id,notification_events) VALUES($1,'{"issue_updated":false}') ON CONFLICT(user_id) DO UPDATE SET notification_events=EXCLUDED.notification_events`, f.task.CreatorID)
	require.NoError(t, err)
	require.Nil(t, post("lead", "muted", "Muted report").RejectReason)
	var notices []domain.Notification
	require.NoError(t, f.db.Select(&notices, `SELECT * FROM notifications`))
	require.Len(t, notices, 1)
	require.Equal(t, f.task.CreatorID, notices[0].UserID)
	require.Equal(t, f.task.WorkspaceID, notices[0].WorkspaceID)
	require.Equal(t, &f.task.IssueID, notices[0].IssueID)
	require.Contains(t, notices[0].Title, "CI passed.")
	otherCtx := domain.WithAccess(ctx, domain.Access{WorkspaceID: other.workspaceID, UserID: other.userID})
	_, err = NewNotificationRepository(f.db).GetByID(otherCtx, notices[0].ID)
	require.Error(t, err)

	tg := NewTelegramRepository(f.db)
	d, err := tg.ClaimDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, d)
	var sent []domain.TelegramDelivery
	require.NoError(t, tg.Deliver(ctx, *d, "", func(delivery domain.TelegramDelivery) error {
		sent = append(sent, delivery)
		return nil
	}))
	require.Len(t, sent, 1)
	require.Equal(t, f.task.WorkspaceID, sent[0].WorkspaceID)
	require.Equal(t, f.task.CreatorID, sent[0].UserID)
	require.Equal(t, f.task.ConversationID, sent[0].ConversationID)
	require.Contains(t, sent[0].Text, "CI passed.")
	again, err := tg.ClaimDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, again)

	_, err = f.db.Exec(`UPDATE user_preferences SET notification_events='{}' WHERE user_id=$1`, f.task.CreatorID)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE telegram_settings SET enabled=false WHERE workspace_id=$1`, f.task.WorkspaceID)
	require.NoError(t, err)
	second := post("lead", "second", "CI passed.")
	secondNotice, err := NewNotificationRepository(f.db).GetByID(f.ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, notices[0].Title, secondNotice.Title, "separate reports may have the same text")
	again, err = tg.ClaimDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, again, "an enabled bot in another workspace cannot receive this report")
}

func TestOwnerDecisionUsesLinkedLeadTelegramChannel(t *testing.T) {
	f := newRoomFixture(t)
	ctx := context.Background()
	_, err := f.db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,1,'x','x',true,'{12345}')`, f.task.WorkspaceID)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,12345)`, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)

	ask := f.member("lead", domain.RoomAsk, "Which search should ship?", domain.TaskRoomData{})
	ask.ToRoles, ask.DedupeKey = []string{"human"}, "search"
	for i := 0; i < 2; i++ {
		_, err = f.repo.RecordRoomMessage(ctx, f.task.ID, ask)
		require.NoError(t, err)
	}
	tg := NewTelegramRepository(f.db)
	d, err := tg.ClaimDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, d)
	var delivered bool
	require.NoError(t, tg.Deliver(ctx, *d, "", func(msg domain.TelegramDelivery) error {
		delivered = true
		require.Equal(t, f.task.CreatorID, msg.UserID)
		require.Equal(t, f.task.ConversationID, msg.ConversationID)
		require.Contains(t, msg.Text, ask.Body)
		return nil
	}))
	require.True(t, delivered)
	duplicate, err := tg.ClaimDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, duplicate)
}
