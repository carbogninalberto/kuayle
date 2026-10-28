package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTelegramAllowlistLinkingAndSharedConversation(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, other, ws := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Test','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Test',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{user, other} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, id)
		require.NoError(t, err)
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	repo := NewTelegramRepository(db)
	config := domain.TelegramSettings{WorkspaceID: ws, BotID: 42, TokenEncrypted: "encrypted-fixture", WebhookSecret: "header-fixture", Enabled: true, AllowedUserIDs: []int64{12345, 67890}}
	require.NoError(t, repo.Save(ctx, config))
	denied := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: other})
	require.ErrorIs(t, repo.Save(denied, config), ErrTelegramDenied)
	require.NoError(t, repo.CreateLink(ctx, ws, 12345, "code-hash"))
	require.NoError(t, repo.CreateLink(denied, ws, 12345, "other-pending-code"))
	update := domain.TelegramUpdate{ID: 1, UserID: 67890, ChatID: 67890, Text: "/start code", Private: true}
	require.NoError(t, repo.Receive(context.Background(), ws, "header-fixture", update, "code-hash", "compatible", "test"))
	// A different allowlisted account cannot redeem the intended user's code.
	var n int
	require.NoError(t, db.Get(&n, `SELECT count(*) FROM telegram_links`))
	require.Zero(t, n)
	update.UserID = 12345
	update.ChatID = 12345
	update.ID = 2
	require.ErrorIs(t, repo.Receive(context.Background(), ws, "wrong", update, "code-hash", "compatible", "test"), ErrTelegramDenied)
	require.NoError(t, repo.Receive(context.Background(), ws, "header-fixture", update, "code-hash", "compatible", "test"))
	require.NoError(t, db.Get(&n, `SELECT count(*) FROM telegram_link_codes`))
	require.Zero(t, n, "linking invalidates competing pending codes for that Telegram ID")
	update.ID = 3
	update.Text = "Hello from Telegram"
	duplicateResults := make(chan error, 2)
	for range 2 {
		go func() {
			duplicateResults <- repo.Receive(context.Background(), ws, "header-fixture", update, "", "compatible", "test")
		}()
	}
	require.NoError(t, <-duplicateResults)
	require.NoError(t, <-duplicateResults)
	chats, err := NewConversationRepository(db).List(ctx, ws)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	history, err := NewConversationRepository(db).History(ctx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 2)
	require.Equal(t, update.Text, history.Messages[0].Content)
	// A Web writer must not deadlock with an inbound update waiting for the
	// conversation: inserting the outbox row also checks its settings FK.
	require.NoError(t, NewConversationRepository(db).Cancel(ctx, ws, chats[0].ID))
	webTx, _, err := NewConversationRepository(db).authorized(ctx, ws, chats[0].ID)
	require.NoError(t, err)
	defer webTx.Rollback()
	incoming := update
	incoming.ID = 30
	incoming.Text = "/help"
	incomingDone := make(chan error, 1)
	go func() {
		incomingDone <- repo.Receive(context.Background(), ws, "header-fixture", incoming, "", "compatible", "test")
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		_ = db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM conversations WHERE id=%')`)
		return waiting
	}, 2*time.Second, 10*time.Millisecond)
	nextRequest := uuid.New()
	webUser, err := addConversationMessage(ctx, webTx, chats[0].ID, nextRequest, nil, "user", "From Web", "completed", "", "")
	require.NoError(t, err)
	_, err = addConversationMessage(ctx, webTx, chats[0].ID, nextRequest, &webUser.ID, "assistant", "", "queued", "compatible", "test")
	require.NoError(t, err)
	require.NoError(t, webTx.Commit())
	require.NoError(t, <-incomingDone)
	// Concurrent settings changes fail fast instead of holding all pool slots.
	entered, release := make(chan struct{}), make(chan struct{})
	configured := make(chan error, 1)
	go func() {
		configured <- repo.Configure(context.Background(), ws, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	busyErr := repo.Configure(context.Background(), ws, func() error { t.Error("concurrent configuration callback ran"); return nil })
	close(release)
	require.ErrorIs(t, busyErr, ErrTelegramBusy)
	require.NoError(t, <-configured)
	// Claim the account acknowledgement, then revoke before outbound I/O.
	claimed, err := repo.ClaimDelivery(context.Background())
	require.NoError(t, err)
	require.NotNil(t, claimed)
	// Empty allowlist denies both new input and previously queued output.
	config.AllowedUserIDs = []int64{}
	require.NoError(t, repo.Save(ctx, config))
	sent := false
	require.NoError(t, repo.Deliver(context.Background(), *claimed, func(domain.TelegramDelivery) error { sent = true; return nil }))
	require.False(t, sent, "a previously claimed delivery must reauthorize after revocation")
	update.ID = 4
	require.NoError(t, repo.Receive(context.Background(), ws, "header-fixture", update, "", "compatible", "test"))
	delivery, err := repo.ClaimDelivery(context.Background())
	require.NoError(t, err)
	require.Nil(t, delivery)
	// Recovery must commit even when there is no ready delivery to claim.
	_, err = db.Exec(`UPDATE telegram_deliveries SET status='sending',updated_at=now()-interval '2 minutes' WHERE id=$1`, claimed.ID)
	require.NoError(t, err)
	delivery, err = repo.ClaimDelivery(context.Background())
	require.NoError(t, err)
	require.Nil(t, delivery)
	var recovered string
	require.NoError(t, db.Get(&recovered, `SELECT status FROM telegram_deliveries WHERE id=$1`, claimed.ID))
	require.Equal(t, "uncertain", recovered)
	require.NoError(t, db.Get(&n, `SELECT count(*) FROM conversation_messages`))
	require.Equal(t, 4, n)
	config.AllowedUserIDs = []int64{12345}
	require.NoError(t, repo.Save(ctx, config))
	_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, user)
	require.NoError(t, err)
	update.ID = 5
	require.NoError(t, repo.Receive(context.Background(), ws, "header-fixture", update, "", "compatible", "test"))
	require.NoError(t, db.Get(&n, `SELECT count(*) FROM conversation_messages`))
	require.Equal(t, 4, n)
}
