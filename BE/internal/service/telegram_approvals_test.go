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
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type telegramRecorder struct {
	mu       sync.Mutex
	messages []map[string]any
	answers  []string
	edits    int
}

func (r *telegramRecorder) serve(t *testing.T, secret *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		switch {
		case strings.HasSuffix(req.URL.Path, "/getMe"):
			fmt.Fprint(w, `{"ok":true,"result":{"id":987654321,"is_bot":true,"username":"fixture_bot"}}`)
		case strings.HasSuffix(req.URL.Path, "/setWebhook"):
			*secret = body["secret_token"].(string)
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case strings.HasSuffix(req.URL.Path, "/sendMessage"):
			r.messages = append(r.messages, body)
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
		case strings.HasSuffix(req.URL.Path, "/answerCallbackQuery"):
			r.answers = append(r.answers, body["text"].(string))
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case strings.HasSuffix(req.URL.Path, "/editMessageReplyMarkup"):
			r.edits++
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected Telegram method %s", req.URL.Path)
			w.WriteHeader(400)
		}
	}))
}

func TestTelegramApprovalButtonsResolveThroughTheSharedBackend(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_telegram_approval_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	user, ws, team := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Test','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Test',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2))`, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})

	var hookSecret string
	recorder := &telegramRecorder{}
	remote := recorder.serve(t, &hookSecret)
	defer remote.Close()
	key := crypto.DeriveKey(uuid.NewString())
	ai := NewAISettingsService(repository.NewAISettingsRepository(db), repository.NewWorkspaceRepository(db), nil, key, WithAIPrivateNetworkAccess(true))
	telegram := NewTelegramService(repository.NewTelegramRepository(db), repository.NewWorkspaceRepository(db), ai, key, "https://kuayle.example.test")
	telegram.endpoint, telegram.client = remote.URL, remote.Client()
	token := "987654321:" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = telegram.Save(ctx, ws, TelegramSettingsRequest{Token: dto.OptionalString{Set: true, Value: &token}, Enabled: true, AllowedUserIDs: []int64{12345, 777}})
	require.NoError(t, err)
	receive := func(raw map[string]any) error {
		data, _ := json.Marshal(raw)
		return telegram.Receive(context.Background(), ws, hookSecret, data)
	}
	message := func(id int, from int64, text string) map[string]any {
		return map[string]any{"update_id": id, "message": map[string]any{"from": map[string]any{"id": from}, "chat": map[string]any{"id": from, "type": "private"}, "text": text}}
	}
	press := func(id int, from int64, data string) map[string]any {
		return map[string]any{"update_id": id, "callback_query": map[string]any{"id": fmt.Sprint("cb", id), "data": data, "from": map[string]any{"id": from}, "message": map[string]any{"message_id": 42, "chat": map[string]any{"id": from, "type": "private"}}}}
	}
	link, err := telegram.Link(ctx, ws, 12345)
	require.NoError(t, err)
	require.NoError(t, receive(message(1, 12345, link)))

	// The model configuration is real; replies come from a scripted provider.
	provider, _ := scriptedProvider(t, toolCallStream("delegate_issue", `{"identifier":"ENG-1","instructions":"Implement dark mode"}`), finalAnswer)
	apiKey, err := crypto.Encrypt("provider-fixture", key)
	require.NoError(t, err)
	require.NoError(t, repository.NewAISettingsRepository(db).Upsert(ctx, &domain.AISettings{WorkspaceID: ws, Provider: "openai_compatible", BaseURL: provider.URL, Model: "fixture", APIKeyEncrypted: &apiKey}))
	conversations := repository.NewConversationRepository(db)
	issueRepo, teamRepo := repository.NewIssueRepository(db), repository.NewTeamRepository(db)
	issues := NewIssueService(issueRepo, teamRepo, repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db), realtime.NewHub(), nil)
	chat := NewConversationService(conversations, ai)
	chat.SetTools(NewAssistantTools(issueRepo, teamRepo, repository.NewWorkspaceRepository(db), issues))
	tasks := &approvalTaskFake{repo: repository.NewAgentTaskRepository(db)}
	chat.SetApprovals(NewAssistantApprovals(conversations, tasks))
	telegram.SetApprovals(chat)

	require.NoError(t, receive(message(2, 12345, "Start a coding agent on ENG-1")))
	attempt, err := conversations.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, attempt)
	chat.execute(context.Background(), *attempt)

	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { telegram.Run(workerCtx); close(done) }()
	defer func() { cancel(); <-done }()
	var keyboard []any
	require.Eventually(t, func() bool {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		for _, m := range recorder.messages {
			if markup, ok := m["reply_markup"].(map[string]any); ok {
				keyboard = markup["inline_keyboard"].([]any)
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond)
	row := keyboard[0].([]any)
	approve := row[0].(map[string]any)["callback_data"].(string)
	deny := row[1].(map[string]any)["callback_data"].(string)
	require.Equal(t, "Approve ENG-1", row[0].(map[string]any)["text"])
	approvalID := uuid.MustParse(strings.TrimPrefix(approve, "approve:"))

	// An allowlisted but unlinked Telegram user cannot decide someone's approval.
	require.NoError(t, receive(press(10, 777, approve)))
	// Forged data and group chats are ignored before any lookup.
	require.NoError(t, receive(press(11, 12345, "approve:not-a-uuid")))
	group := press(12, 12345, approve)
	group["callback_query"].(map[string]any)["message"].(map[string]any)["chat"] = map[string]any{"id": -100, "type": "group"}
	require.NoError(t, receive(group))
	var status string
	require.NoError(t, db.Get(&status, `SELECT status FROM conversation_approvals WHERE id=$1`, approvalID))
	require.Equal(t, "pending", status)
	require.Zero(t, tasks.creates)

	require.NoError(t, receive(press(13, 12345, approve)))
	require.NoError(t, receive(press(13, 12345, approve)), "a redelivered update is ignored")
	require.NoError(t, receive(press(14, 12345, deny)))
	require.NoError(t, db.Get(&status, `SELECT status FROM conversation_approvals WHERE id=$1`, approvalID))
	require.Equal(t, "approved", status)
	var taskCount int
	require.NoError(t, db.Get(&taskCount, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, ws))
	require.Equal(t, 1, taskCount)
	recorder.mu.Lock()
	require.Equal(t, []string{"Approved: the coding agent is starting.", "This approval was already decided."}, recorder.answers)
	require.Equal(t, 2, recorder.edits)
	recorder.mu.Unlock()

	// The Web UI sees and replays the same decision.
	web, err := chat.ResolveApproval(ctx, ws, attempt.Conversation.ID, approvalID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", web.Status)
}

// Telegram answers an accepted approval as acceptance, never as a started agent.
func TestTelegramApprovalAnswerDescribesAcceptance(t *testing.T) {
	answer, final := telegramApprovalAnswer(&domain.ConversationApproval{Action: "accept_agent_work", Status: "approved"}, nil)
	require.True(t, final)
	require.Equal(t, "Approved: the work was accepted in Kuayle without merging.", answer)
	reason := "Only workspace admins or the task's creator can accept its work"
	answer, _ = telegramApprovalAnswer(&domain.ConversationApproval{Action: "accept_agent_work", Status: "failed", Error: &reason}, nil)
	require.Equal(t, "The work could not be accepted. "+reason, answer)
}
