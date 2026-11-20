package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The claimed reply's channel decides whether it may carry rich fences: a
// message queued for a linked Telegram chat is telegram, everything else web.
func TestConversationMessageChannelFollowsTheTelegramLink(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, ws := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Channel','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Channel',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,12345,'x','x',true,'{12345}')`, ws)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	repo := NewConversationRepository(db)
	conversation, err := repo.Create(ctx, ws, "Channel")
	require.NoError(t, err)

	// Each turn sends a fresh message, claims its reply and reads its channel.
	turn := func() string {
		_, err := repo.Send(ctx, ws, conversation.ID, uuid.New(), "Hello", "compatible", "model")
		require.NoError(t, err)
		attempt, err := repo.Claim(context.Background())
		require.NoError(t, err)
		require.NotNil(t, attempt)
		channel, err := repo.MessageChannel(context.Background(), *attempt)
		require.NoError(t, err)
		require.NoError(t, repo.Finish(ctx, *attempt, "completed"))
		return channel
	}

	require.Equal(t, domain.ConversationChannelWeb, turn(), "an unlinked conversation replies on the web channel")

	_, err = db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id,conversation_id) VALUES($1,$2,12345,$3)`, ws, user, conversation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ConversationChannelTelegram, turn(), "a reply queued for a linked chat is telegram")

	_, err = db.Exec(`DELETE FROM telegram_links WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	require.Equal(t, domain.ConversationChannelWeb, turn(), "unlinking returns the conversation to the web channel")
}
