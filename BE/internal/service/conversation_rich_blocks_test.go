package service

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// Web replies learn the two rich fences the chat renders, so a request for a
// chart or a widget gets a block the UI can draw; Telegram shows them as raw
// code, so its guidance keeps only the attachment note shared by every channel.
// The exact slice is asserted, so no stray system message can slip in.
func TestGuidanceMessagesMatchTheChannel(t *testing.T) {
	tests := []struct {
		name    string
		images  bool
		channel string
	}{
		{"web", false, domain.ConversationChannelWeb},
		{"web with images", true, domain.ConversationChannelWeb},
		{"telegram", false, domain.ConversationChannelTelegram},
		{"telegram with images", true, domain.ConversationChannelTelegram},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := []chatCompletionMessage{}
			if test.images {
				want = append(want, chatCompletionMessage{Role: "system", Content: attachmentsPrompt})
			}
			if test.channel == domain.ConversationChannelWeb {
				want = append(want, chatCompletionMessage{Role: "system", Content: richBlocksPrompt})
			}
			require.Equal(t, want, guidanceMessages(test.images, test.channel))
		})
	}
}

// execute wires the channel into the outgoing model request: a web reply's
// request carries the rich-block prompt, a reply queued for a linked Telegram
// chat does not.
func TestExecuteSendsRichBlocksPromptOnlyToWebReplies(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_assistant_rich_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`); admin.Close() })
	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	parsed.Path, parsed.RawPath = "/"+name, ""
	migrator, err := migrate.New("file://../../migrations", parsed.String())
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	migrator.Close()
	db, err := sqlx.Connect("pgx", parsed.String())
	require.NoError(t, err)
	defer db.Close()

	user, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Rich','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Rich',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, user)
	require.NoError(t, err)

	provider, requests := scriptedProvider(t, finalAnswer, finalAnswer)
	key := crypto.DeriveKey(uuid.NewString())
	ai := NewAISettingsService(repository.NewAISettingsRepository(db), repository.NewWorkspaceRepository(db), nil, key, WithAIPrivateNetworkAccess(true))
	apiKey, err := crypto.Encrypt("provider-fixture", key)
	require.NoError(t, err)
	require.NoError(t, repository.NewAISettingsRepository(db).Upsert(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user}), &domain.AISettings{WorkspaceID: ws, Provider: "openai_compatible", BaseURL: provider.URL, Model: "fixture", APIKeyEncrypted: &apiKey}))
	conversations := repository.NewConversationRepository(db)
	chat := NewConversationService(conversations, ai)

	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	conversation, err := conversations.Create(ctx, ws, "Rich")
	require.NoError(t, err)

	turn := func() map[string]any {
		_, err = conversations.Send(ctx, ws, conversation.ID, uuid.New(), "Show me a chart", "openai_compatible", "fixture")
		require.NoError(t, err)
		attempt, err := conversations.Claim(context.Background())
		require.NoError(t, err)
		require.NotNil(t, attempt)
		chat.execute(context.Background(), *attempt)
		require.NotEmpty(t, *requests)
		return (*requests)[len(*requests)-1]
	}

	messages := func(request map[string]any) []string {
		var contents []string
		for _, message := range request["messages"].([]any) {
			contents = append(contents, message.(map[string]any)["content"].(string))
		}
		return contents
	}

	require.Contains(t, messages(turn()), richBlocksPrompt, "a web reply's model request learns the rich fences")

	_, err = db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,12345,'x','x',true,'{12345}')`, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id,conversation_id) VALUES($1,$2,12345,$3)`, ws, user, conversation.ID)
	require.NoError(t, err)
	require.NotContains(t, messages(turn()), richBlocksPrompt, "a reply queued for a linked Telegram chat is not told about the rich fences")
}
