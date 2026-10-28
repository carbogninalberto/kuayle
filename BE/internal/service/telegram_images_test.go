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

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// Photos sent to the bot by a linked user are downloaded from Telegram's file
// endpoint, sniffed and attached to the Telegram-originated user message with
// the caption as text. Unlinked senders, duplicates and oversized or
// non-image files are never stored.
func TestTelegramPhotosBecomeConversationAttachments(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_telegram_images_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})

	photo := testPNG(t, 5, 4)
	var mu sync.Mutex
	var hookSecret string
	downloads := map[string]int{}
	files := map[string]struct {
		path string
		size int64
		body []byte
	}{
		"photo-small": {"photos/file_1.jpg", int64(len(photo)), photo},
		"photo-large": {"photos/file_2.jpg", int64(len(photo)), photo},
		"html-doc":    {"documents/file_3.png", 40, []byte("<html><script>alert(1)</script></html>")},
		"huge":        {"documents/file_4.png", domain.ConversationAttachmentMaxBytes + 1, photo},
		"escape":      {"../../etc/passwd", 10, photo},
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			fmt.Fprint(w, `{"ok":true,"result":{"id":987654321,"is_bot":true,"username":"fixture_bot"}}`)
		case strings.HasSuffix(r.URL.Path, "/setWebhook"):
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			hookSecret = body["secret_token"].(string)
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			file, ok := files[body["file_id"]]
			if !ok {
				fmt.Fprint(w, `{"ok":false,"description":"Bad Request: invalid file_id"}`)
				return
			}
			fmt.Fprintf(w, `{"ok":true,"result":{"file_id":%q,"file_path":%q,"file_size":%d}}`, body["file_id"], file.path, file.size)
		case strings.Contains(r.URL.Path, "/file/bot"):
			for _, file := range files {
				if strings.HasSuffix(r.URL.Path, "/"+file.path) {
					downloads[file.path]++
					_, _ = w.Write(file.body)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
		}
	}))
	defer remote.Close()
	key := crypto.DeriveKey(uuid.NewString())
	aiRepo := repository.NewAISettingsRepository(db)
	ai := NewAISettingsService(aiRepo, repository.NewWorkspaceRepository(db), nil, key)
	repo := repository.NewTelegramRepository(db)
	svc := NewTelegramService(repo, repository.NewWorkspaceRepository(db), ai, key, "https://kuayle.example.test")
	svc.endpoint, svc.client = remote.URL, remote.Client()
	storage := newMemoryStorage()
	svc.SetImages(NewImageStore(storage))
	token := "987654321:" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = svc.Save(ownerCtx, ws, TelegramSettingsRequest{Token: dto.OptionalString{Set: true, Value: &token}, Enabled: true, AllowedUserIDs: []int64{12345, 777}})
	require.NoError(t, err)
	encrypted, err := crypto.Encrypt("provider-fixture", key)
	require.NoError(t, err)
	require.NoError(t, aiRepo.Upsert(ownerCtx, &domain.AISettings{WorkspaceID: ws, Provider: "openai_compatible", BaseURL: "https://model.example.test", Model: "fixture", APIKeyEncrypted: &encrypted}))
	send := func(id, from int, message map[string]any) {
		t.Helper()
		message["from"] = map[string]any{"id": from}
		message["chat"] = map[string]any{"id": from, "type": "private"}
		raw, _ := json.Marshal(map[string]any{"update_id": id, "message": message})
		require.NoError(t, svc.Receive(context.Background(), ws, hookSecret, raw))
	}
	photoSizes := []map[string]any{{"file_id": "photo-small", "file_size": 100}, {"file_id": "photo-large", "file_size": 200}, {"file_id": "too-big", "file_size": domain.ConversationAttachmentMaxBytes + 1}}

	// An allowed but unlinked sender's photo is never downloaded.
	send(1, 777, map[string]any{"photo": photoSizes, "caption": "unlinked"})
	require.Empty(t, downloads)

	command, err := svc.Link(ownerCtx, ws, 12345)
	require.NoError(t, err)
	send(2, 12345, map[string]any{"text": command})
	send(3, 12345, map[string]any{"photo": photoSizes, "caption": "Why does this crash?"})
	send(3, 12345, map[string]any{"photo": photoSizes, "caption": "Why does this crash?"})
	require.Equal(t, 1, downloads["photos/file_2.jpg"], "the largest photo within the limit, downloaded once")
	require.Zero(t, downloads["photos/file_1.jpg"])

	conversations := repository.NewConversationRepository(db)
	chats, err := conversations.List(ownerCtx, ws)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	history, err := conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 2, "the duplicate update adds nothing")
	message := history.Messages[0]
	require.Equal(t, "Why does this crash?", message.Content)
	require.Len(t, message.Attachments, 1)
	attachment := message.Attachments[0]
	require.Equal(t, "image/png", attachment.ContentType, "the type is sniffed, not taken from Telegram")
	require.Equal(t, [2]int{5, 4}, [2]int{attachment.Width, attachment.Height})
	require.Equal(t, user, attachment.UploadedBy)
	require.Len(t, storage.objects, 1)
	_, ok := storage.objects[attachment.StorageKey]
	require.True(t, ok)

	// Finish the queued reply so the next message is accepted.
	attempt, err := conversations.Claim(context.Background())
	require.NoError(t, err)
	require.NoError(t, conversations.Finish(ownerCtx, *attempt, "completed"))
	// Disguised, oversized and path-escaping documents are not stored; without
	// a caption the user message says the image could not be received.
	send(4, 12345, map[string]any{"document": map[string]any{"file_id": "html-doc", "mime_type": "image/png", "file_name": "x.png", "file_size": 40}})
	require.Len(t, storage.objects, 1)
	history, err = conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 4)
	require.Empty(t, history.Messages[2].Attachments)
	require.Contains(t, history.Messages[2].Content, "could not be received")
	attempt, err = conversations.Claim(context.Background())
	require.NoError(t, err)
	require.NoError(t, conversations.Finish(ownerCtx, *attempt, "completed"))
	send(5, 12345, map[string]any{"document": map[string]any{"file_id": "huge", "mime_type": "image/png", "file_size": 10}, "caption": "huge"})
	send(6, 12345, map[string]any{"document": map[string]any{"file_id": "escape", "mime_type": "image/png", "file_size": 10}, "caption": "escape"})
	send(7, 12345, map[string]any{"document": map[string]any{"file_id": "photo-small", "mime_type": "application/pdf", "file_size": 10}, "caption": "pdf"})
	require.Len(t, storage.objects, 1)
	require.Zero(t, downloads["../../etc/passwd"])
	require.Zero(t, downloads["documents/file_4.png"])
}
