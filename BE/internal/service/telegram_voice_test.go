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

// Voice notes and audio messages from a linked sender are downloaded with
// the photo checks, transcribed once and recorded as the user's message with
// a mic marker. Unlinked senders, duplicates, oversized or over-long notes
// and workspaces with voice off are never downloaded or transcribed; linked
// senders get a short explanation instead.
func TestTelegramVoiceNotesBecomeTranscribedMessages(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_telegram_voice_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	var mu sync.Mutex
	var hookSecret string
	downloads := map[string]int{}
	files := map[string]struct {
		path string
		size int64
		body string
	}{
		"note-1":  {"voice/file_1.oga", 4, "OggS"},
		"audio-1": {"music/file_2.m4a", 4, "ftyp"},
		"huge":    {"voice/file_3.oga", domain.VoiceMaxBytes + 1, "OggS"},
		"escape":  {"../../etc/passwd", 4, "OggS"},
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
					_, _ = fmt.Fprint(w, file.body)
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
	workspaces := repository.NewWorkspaceRepository(db)
	ai := NewAISettingsService(aiRepo, workspaces, nil, key)
	repo := repository.NewTelegramRepository(db)
	svc := NewTelegramService(repo, workspaces, ai, key, "https://kuayle.example.test")
	svc.endpoint, svc.client = remote.URL, remote.Client()
	fake, client := newFakeVoiceServer(t)
	fake.states["vosk-small-en-us-0.15"] = "ready"
	fake.text = "please fix the login page"
	conversations := repository.NewConversationRepository(db)
	voiceRepo := repository.NewVoiceRepository(db)
	voice := NewVoiceService(voiceRepo, workspaces, conversations, client)
	svc.SetVoice(voice)
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
		svc.WaitVoice()
	}
	voiceNote := func(file string, duration int, size int64) map[string]any {
		return map[string]any{"voice": map[string]any{"file_id": file, "duration": duration, "mime_type": "audio/ogg", "file_size": size}}
	}
	replies := func() []string {
		t.Helper()
		var texts []string
		require.NoError(t, db.Select(&texts, `SELECT text FROM telegram_deliveries WHERE workspace_id=$1 ORDER BY created_at, id`, ws))
		return texts
	}
	finishReply := func() {
		t.Helper()
		attempt, err := conversations.Claim(context.Background())
		require.NoError(t, err)
		require.NoError(t, conversations.Finish(ownerCtx, *attempt, "completed"))
	}

	command, err := svc.Link(ownerCtx, ws, 12345)
	require.NoError(t, err)
	send(1, 12345, map[string]any{"text": command})
	linked := len(replies())

	// Voice off: the linked sender is told, nothing is downloaded.
	send(2, 12345, voiceNote("note-1", 3, 4))
	require.Empty(t, downloads)
	require.Empty(t, fake.transcribed)
	require.Contains(t, replies()[linked], "Voice notes are off")

	_, err = voice.Select(ownerCtx, ws, "vosk-small-en-us-0.15")
	require.NoError(t, err)

	// An allowed but unlinked sender's voice note is never downloaded.
	send(3, 777, voiceNote("note-1", 3, 4))
	require.Empty(t, downloads)
	require.Empty(t, fake.transcribed)

	// Over five minutes or over the size limit: explained, never downloaded.
	send(4, 12345, voiceNote("note-1", domain.VoiceMaxSeconds+1, 4))
	send(5, 12345, voiceNote("huge", 10, domain.VoiceMaxBytes+1))
	require.Empty(t, downloads)
	texts := replies()
	require.Contains(t, texts[len(texts)-2], "at most 5 minutes")
	require.Contains(t, texts[len(texts)-1], "too large")

	// A linked sender's voice note becomes their message, once.
	send(6, 12345, voiceNote("note-1", 3, 4))
	send(6, 12345, voiceNote("note-1", 3, 4))
	require.Equal(t, 1, downloads["voice/file_1.oga"])
	require.Equal(t, []string{"vosk-small-en-us-0.15 audio/ogg OggS"}, fake.transcribed)
	chats, err := conversations.List(ownerCtx, ws)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	history, err := conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 2, "the duplicate update adds nothing")
	require.Equal(t, "🎤 please fix the login page", history.Messages[0].Content)
	require.Equal(t, "user", history.Messages[0].Role)
	finishReply()

	// Audio messages are transcribed too, keeping their caption.
	send(7, 12345, map[string]any{"audio": map[string]any{"file_id": "audio-1", "duration": 20, "mime_type": "audio/mp4", "file_size": 4}, "caption": "from the meeting"})
	history, err = conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 4)
	require.Equal(t, "🎤 please fix the login page\n\nfrom the meeting", history.Messages[2].Content)
	finishReply()

	// A path escaping Telegram's file area is not fetched; a file Telegram
	// reports as too large after getFile is not fetched either.
	files["late-huge"] = struct {
		path string
		size int64
		body string
	}{"voice/file_9.oga", domain.VoiceMaxBytes + 1, "OggS"}
	send(8, 12345, voiceNote("escape", 3, 4))
	send(9, 12345, voiceNote("late-huge", 3, 4))
	require.Zero(t, downloads["../../etc/passwd"])
	require.Zero(t, downloads["voice/file_9.oga"])
	texts = replies()
	require.Contains(t, texts[len(texts)-2], "could not be received")
	require.Contains(t, texts[len(texts)-1], "too large")

	// The voice service's verdicts are explained to the sender.
	fake.failure = "too_long"
	send(10, 12345, voiceNote("note-1", 3, 4))
	fake.failure = ""
	fake.text = "   "
	send(11, 12345, voiceNote("note-1", 3, 4))
	texts = replies()
	require.Contains(t, texts[len(texts)-2], "at most 5 minutes")
	require.Contains(t, texts[len(texts)-1], "No speech was recognized")
	history, err = conversations.History(ownerCtx, ws, chats[0].ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 4, "notices never start a turn")
}
