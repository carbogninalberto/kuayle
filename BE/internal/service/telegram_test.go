package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestTelegramSettingsAndBotReplies(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_telegram_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`); admin.Close() })
	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	parsed.Path = "/" + name
	parsed.RawPath = ""
	migrator, err := migrate.New("file://../../migrations", parsed.String())
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	migrator.Close()
	db, err := sqlx.Connect("pgx", parsed.String())
	require.NoError(t, err)
	defer db.Close()
	user, member, ws := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, member} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Test','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Test',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for _, id := range []uuid.UUID{user, member} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, id)
		require.NoError(t, err)
	}
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	memberCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: member})
	var mu sync.Mutex
	var hookSecret string
	var sent []string
	var calls int
	var dropPending []bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			fmt.Fprint(w, `{"ok":true,"result":{"id":987654321,"is_bot":true,"username":"fixture_bot"}}`)
		case strings.HasSuffix(r.URL.Path, "/setWebhook"):
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			hookSecret = body["secret_token"].(string)
			drop, _ := body["drop_pending_updates"].(bool)
			dropPending = append(dropPending, drop)
			require.Equal(t, "https://kuayle.example.test/api/telegram/webhook/"+ws.String(), body["url"])
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Nil(t, body["parse_mode"])
			sent = append(sent, body["text"].(string))
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
		default:
			t.Errorf("unexpected method")
			w.WriteHeader(400)
		}
	}))
	defer remote.Close()
	key := crypto.DeriveKey(uuid.NewString())
	ai := NewAISettingsService(repository.NewAISettingsRepository(db), repository.NewWorkspaceRepository(db), nil, key)
	repo := repository.NewTelegramRepository(db)
	svc := NewTelegramService(repo, repository.NewWorkspaceRepository(db), ai, key, "https://kuayle.example.test")
	svc.endpoint = remote.URL
	svc.client = remote.Client()
	token := "987654321:" + strings.ReplaceAll(uuid.NewString(), "-", "")
	request := TelegramSettingsRequest{Token: dto.OptionalString{Set: true, Value: &token}, Enabled: true, AllowedUserIDs: []int64{12345}}
	_, err = svc.Save(memberCtx, ws, request)
	require.ErrorIs(t, err, repository.ErrTelegramDenied)
	require.Zero(t, calls)
	status, err := svc.Save(ownerCtx, ws, request)
	require.NoError(t, err)
	require.Equal(t, "connected", status.Status)
	stored, err := repo.Get(ownerCtx, ws)
	require.NoError(t, err)
	require.NotEqual(t, token, stored.TokenEncrypted)
	decoded, err := crypto.Decrypt(stored.TokenEncrypted, key)
	require.NoError(t, err)
	require.Equal(t, token, decoded)
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), token)
	require.NotContains(t, string(encoded), hookSecret)
	status, err = svc.Status(memberCtx, ws)
	require.NoError(t, err)
	require.Empty(t, status.AllowedUserIDs)
	require.ErrorIs(t, svc.Receive(context.Background(), ws, "invalid-header", []byte("not JSON")), repository.ErrTelegramDenied)
	beforeDisable := hookSecret
	_, err = svc.Save(ownerCtx, ws, TelegramSettingsRequest{Enabled: false, AllowedUserIDs: []int64{12345}})
	require.NoError(t, err)
	disabled, err := repo.Get(ownerCtx, ws)
	require.NoError(t, err)
	require.Equal(t, beforeDisable, disabled.WebhookSecret, "disabled hooks acknowledge and ignore messages using the existing secret")
	require.NoError(t, svc.Receive(context.Background(), ws, beforeDisable, []byte(`{"update_id":90,"message":{"from":{"id":12345},"chat":{"id":12345,"type":"private"},"text":"during disabled"}}`)))
	_, err = svc.Save(ownerCtx, ws, TelegramSettingsRequest{Enabled: true, AllowedUserIDs: []int64{12345}})
	require.NoError(t, err)
	mu.Lock()
	dropped := dropPending[len(dropPending)-1]
	mu.Unlock()
	require.True(t, dropped, "re-enabling discards Telegram's queued updates from the disabled period")
	command, err := svc.Link(ownerCtx, ws, 12345)
	require.NoError(t, err)
	receive := func(id, from int, text string) error {
		raw, _ := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{"from": map[string]any{"id": from}, "chat": map[string]any{"id": from, "type": "private"}, "text": text}})
		return svc.Receive(context.Background(), ws, hookSecret, raw)
	}
	require.NoError(t, receive(1, 12345, command))
	require.NoError(t, receive(1, 12345, command))
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { svc.Run(workerCtx); close(done) }()
	defer func() { cancel(); <-done }()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(sent) == 1 }, 5*time.Second, 20*time.Millisecond)
	// Receive unauthorized callback/message updates without any reply or conversation.
	require.NoError(t, receive(2, 55555, "untrusted"))
	require.NoError(t, svc.Receive(context.Background(), ws, hookSecret, []byte(`{"update_id":3,"callback_query":{"from":{"id":55555},"data":"approve"}}`)))
	chats, err := repository.NewConversationRepository(db).List(ownerCtx, ws)
	require.NoError(t, err)
	require.Empty(t, chats)
	// Model response and Web UI history share the same persisted records.
	encrypted, err := crypto.Encrypt("provider-fixture", key)
	require.NoError(t, err)
	require.NoError(t, repository.NewAISettingsRepository(db).Upsert(ownerCtx, &domain.AISettings{WorkspaceID: ws, Provider: "openai_compatible", BaseURL: "https://model.example.test", Model: "fixture", APIKeyEncrypted: &encrypted}))
	require.NoError(t, receive(4, 12345, "Hello from Telegram"))
	require.NoError(t, receive(4, 12345, "Hello from Telegram"))
	conversations := repository.NewConversationRepository(db)
	chats, err = conversations.List(ownerCtx, ws)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	history, err := conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 2)
	attempt, err := conversations.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, attempt)
	require.Equal(t, ws, attempt.Conversation.WorkspaceID)
	_, err = conversations.Append(ownerCtx, *attempt, "A shared reply")
	require.NoError(t, err)
	require.NoError(t, conversations.Finish(ownerCtx, *attempt, "completed"))
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(sent) == 2 }, 5*time.Second, 20*time.Millisecond)
	mu.Lock()
	require.Contains(t, sent[1], "A shared reply")
	require.Contains(t, sent[1], chats[0].ID.String())
	mu.Unlock()
	// A Web-originated message in the linked conversation is delivered too.
	_, err = conversations.Send(ownerCtx, ws, chats[0].ID, uuid.New(), "From Web", "openai_compatible", "fixture")
	require.NoError(t, err)
	attempt, err = conversations.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, attempt)
	_, err = conversations.Append(ownerCtx, *attempt, "Web reply also delivered")
	require.NoError(t, err)
	require.NoError(t, conversations.Finish(ownerCtx, *attempt, "completed"))
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(sent) == 3 }, 5*time.Second, 20*time.Millisecond)
	// Removal revokes the webhook immediately and retains Web conversation history.
	_, err = svc.Save(ownerCtx, ws, TelegramSettingsRequest{Token: dto.OptionalString{Set: true}})
	require.NoError(t, err)
	require.ErrorIs(t, receive(5, 12345, "after removal"), repository.ErrTelegramDenied)
	history, err = conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 4)
}
func TestTelegramTransportAndTextLimits(t *testing.T) {
	svc := NewTelegramService(nil, nil, nil, nil, "https://kuayle.example.test")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer remote.Close()
	svc.endpoint = remote.URL
	svc.client = newAIProviderClient(true)
	err := svc.call(context.Background(), "fixture-token", "getMe", nil, nil)
	require.ErrorIs(t, err, ErrTelegramAPI)
	require.NotContains(t, err.Error(), "fixture-token")
	require.Equal(t, strings.Repeat("😀", 1600)+"…", telegramText(strings.Repeat("😀", 2000), 3200))
}
