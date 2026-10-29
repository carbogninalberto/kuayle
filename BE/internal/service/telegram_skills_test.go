package service

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// A reply from Telegram lists the workspace's skills in its system prompt, and
// a skill it proposes is delivered as a named draft awaiting activation.
func TestTelegramReplyNamesProposedSkillDrafts(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_telegram_skills_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	skills := repository.NewAssistantSkillRepository(db)
	_, err = skills.Create(ctx, ws, user, domain.AssistantSkillInput{Name: "How we release", Description: "Use when releasing", Body: "1. Tag", Status: "active", AuthorKind: "user"})
	require.NoError(t, err)

	var hookSecret string
	recorder := &telegramRecorder{}
	remote := recorder.serve(t, &hookSecret)
	defer remote.Close()
	key := crypto.DeriveKey(uuid.NewString())
	ai := NewAISettingsService(repository.NewAISettingsRepository(db), repository.NewWorkspaceRepository(db), nil, key, WithAIPrivateNetworkAccess(true))
	telegram := NewTelegramService(repository.NewTelegramRepository(db), repository.NewWorkspaceRepository(db), ai, key, "https://kuayle.example.test")
	telegram.endpoint, telegram.client = remote.URL, remote.Client()
	token := "987654321:" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = telegram.Save(ctx, ws, TelegramSettingsRequest{Token: dto.OptionalString{Set: true, Value: &token}, Enabled: true, AllowedUserIDs: []int64{12345}})
	require.NoError(t, err)
	receive := func(id int, text string) error {
		data, _ := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{"from": map[string]any{"id": 12345}, "chat": map[string]any{"id": 12345, "type": "private"}, "text": text}})
		return telegram.Receive(context.Background(), ws, hookSecret, data)
	}
	link, err := telegram.Link(ctx, ws, 12345)
	require.NoError(t, err)
	require.NoError(t, receive(1, link))

	provider, requests := scriptedProvider(t, toolCallStream("propose_skill", `{"name":"Hotfix","description":"Use when shipping a hotfix","body":"1. Branch\n2. Ship"}`), finalAnswer)
	apiKey, err := crypto.Encrypt("provider-fixture", key)
	require.NoError(t, err)
	require.NoError(t, repository.NewAISettingsRepository(db).Upsert(ctx, &domain.AISettings{WorkspaceID: ws, Provider: "openai_compatible", BaseURL: provider.URL, Model: "fixture", APIKeyEncrypted: &apiKey}))
	conversations := repository.NewConversationRepository(db)
	issueRepo, teamRepo := repository.NewIssueRepository(db), repository.NewTeamRepository(db)
	issues := NewIssueService(issueRepo, teamRepo, repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db), realtime.NewHub(), nil)
	chat := NewConversationService(conversations, ai)
	tools := NewAssistantTools(issueRepo, teamRepo, repository.NewWorkspaceRepository(db), issues)
	tools.SetSkills(skills)
	chat.SetTools(tools)

	require.NoError(t, receive(2, "Make a skill from how we ship hotfixes"))
	attempt, err := conversations.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, attempt)
	chat.execute(context.Background(), *attempt)
	system := []string{}
	for _, message := range (*requests)[0]["messages"].([]any) {
		if entry := message.(map[string]any); entry["role"] == "system" {
			system = append(system, entry["content"].(string))
		}
	}
	require.Contains(t, strings.Join(system, "\n"), "- How we release: Use when releasing")

	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { telegram.Run(workerCtx); close(done) }()
	defer func() { cancel(); <-done }()
	require.Eventually(t, func() bool {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		for _, m := range recorder.messages {
			if text, _ := m["text"].(string); strings.Contains(text, "Draft skill “Hotfix” saved as version 1. It is not active") {
				return strings.Contains(text, "Settings → Assistant → Skills")
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond)
}
