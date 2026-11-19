package service

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// The reply's system prompt carries today's date in the requesting user's
// timezone: at 2026-11-13 22:30 UTC an Auckland user has already crossed
// midnight, so their prompt must say Saturday 2026-11-14, not UTC's
// Friday.
func TestExecutePinsTodayInTheUserProfileTimezone(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_assistant_today_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Test','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Test',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO user_preferences(user_id,timezone) VALUES($1,'Pacific/Auckland')`, user)
	require.NoError(t, err)

	provider, requests := scriptedProvider(t, finalAnswer, finalAnswer)
	key := crypto.DeriveKey(uuid.NewString())
	ai := NewAISettingsService(repository.NewAISettingsRepository(db), repository.NewWorkspaceRepository(db), nil, key, WithAIPrivateNetworkAccess(true))
	apiKey, err := crypto.Encrypt("provider-fixture", key)
	require.NoError(t, err)
	require.NoError(t, repository.NewAISettingsRepository(db).Upsert(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user}), &domain.AISettings{WorkspaceID: ws, Provider: "openai_compatible", BaseURL: provider.URL, Model: "fixture", APIKeyEncrypted: &apiKey}))
	conversations := repository.NewConversationRepository(db)
	chat := NewConversationService(conversations, ai)
	chat.SetPreferences(repository.NewUserPreferencesRepository(db))
	now := time.Date(2026, 11, 13, 22, 30, 0, 0, time.UTC)
	chat.now = func() time.Time { return now }

	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	conversation, err := conversations.Create(ctx, ws, "Planning")
	require.NoError(t, err)
	for i, expected := range []string{
		"Today is Saturday, 2026-11-14 (Pacific/Auckland).",
		"Today is Sunday, 2026-11-15 (Pacific/Auckland).",
	} {
		_, err = conversations.Send(ctx, ws, conversation.ID, uuid.New(), "What's the date of next Friday?", "openai_compatible", "fixture")
		require.NoError(t, err)
		attempt, err := conversations.Claim(context.Background())
		require.NoError(t, err)
		require.NotNil(t, attempt)
		chat.execute(context.Background(), *attempt)

		require.Len(t, *requests, i+1)
		messages := (*requests)[i]["messages"].([]any)
		prompt := messages[0].(map[string]any)
		require.Equal(t, "system", prompt["role"])
		require.Contains(t, prompt["content"], expected)
		now = now.AddDate(0, 0, 1)
	}
}
