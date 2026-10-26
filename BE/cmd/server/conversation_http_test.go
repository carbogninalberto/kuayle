package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func TestConversationHTTPStreamingAndReplay(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	user, other, guest, ws := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Chat HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Chat HTTP',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{user: "owner", other: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	team, issue := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Issue chat','CHAT')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,creator_id,status_id) VALUES($1,$2,$3,1,'CHAT-1','Issue context title','Authorized issue snapshot',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	providerRequests := make(chan string, 4)
	var calls atomic.Int32
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		providerRequests <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"A live response before completion.\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	key := crypto.DeriveKey("conversation-http-test")
	encrypted, err := crypto.Encrypt("test-credential", key)
	require.NoError(t, err)
	aiRepo := repository.NewAISettingsRepository(db)
	require.NoError(t, aiRepo.Upsert(context.Background(), &domain.AISettings{WorkspaceID: ws, Provider: domain.AIProviderOpenAICompatible, BaseURL: provider.URL, Model: "test-model", APIKeyEncrypted: &encrypted}))
	repo := repository.NewConversationRepository(db)
	svc := service.NewConversationService(repo, service.NewAISettingsService(aiRepo, nil, nil, key, service.WithAIPrivateNetworkAccess(true)))
	workerCtx, stop := context.WithCancel(context.Background())
	defer stop()
	finished := make(chan struct{})
	go func() { svc.Run(workerCtx); close(finished) }()
	defer func() { stop(); <-finished }()
	workspaces := repository.NewWorkspaceRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "conversation-http-secret-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	middleware.publicRateLimit = mw.RateLimit(0.01, 2)
	e := echo.New()
	telegramSvc := service.NewTelegramService(repository.NewTelegramRepository(db), workspaces, service.NewAISettingsService(aiRepo, workspaces, nil, key), key, "https://kuayle.test")
	registerRoutes(e, &appHandlers{conversation: handler.NewConversationHandler(svc, "https://kuayle.test"), telegram: handler.NewTelegramHandler(telegramSvc)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	token := tokenFor(user)
	root := server.URL + "/api/workspaces/" + ws.String()
	request := func(method, path, body, auth string, status int, target any) {
		t.Helper()
		req, e := http.NewRequest(method, root+path, strings.NewReader(body))
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode)
		if target != nil {
			require.NoError(t, json.NewDecoder(res.Body).Decode(target))
		}
	}
	var conversation domain.Conversation
	request("POST", "/conversations", fmt.Sprintf(`{"issue_id":%q}`, issue.String()), token, 201, &conversation)
	require.Equal(t, issue, *conversation.IssueID)
	var reopened domain.Conversation
	request("POST", "/conversations", fmt.Sprintf(`{"issue_id":%q}`, issue.String()), token, 201, &reopened)
	require.Equal(t, conversation.ID, reopened.ID)
	path := "/conversations/" + conversation.ID.String()
	request("GET", path, "", tokenFor(other), 404, nil)
	request("POST", "/conversations", `{"title":"Denied"}`, tokenFor(guest), 404, nil)
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "Chat denied", Scopes: []string{"issues:read"}, TokenHash: hash, TokenPrefix: prefix}))
	request("GET", path, "", raw, 403, nil)
	request("GET", "/telegram", "", raw, 403, nil)
	request("POST", "/telegram/link", `{"telegram_user_id":12345}`, raw, 403, nil)
	request("GET", "/telegram", "", tokenFor(guest), 403, nil)
	request("PATCH", "/telegram", `{"enabled":true,"allowed_user_ids":[12345]}`, tokenFor(other), 403, nil)
	request("GET", "/telegram", "", token, 200, nil)
	webhookReq, err := http.NewRequest("POST", server.URL+"/api/telegram/webhook/"+ws.String(), strings.NewReader(`{"update_id":1}`))
	require.NoError(t, err)
	webhookRes, err := http.DefaultClient.Do(webhookReq)
	require.NoError(t, err)
	require.Equal(t, 403, webhookRes.StatusCode)
	webhookRes.Body.Close()
	for _, expected := range []int{403, 429} {
		req, e := http.NewRequest("POST", server.URL+"/api/telegram/webhook/"+ws.String(), strings.NewReader(`{}`))
		require.NoError(t, e)
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		require.Equal(t, expected, res.StatusCode)
		res.Body.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(after int64) *websocket.Conn {
		t.Helper()
		header := http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"https://kuayle.test"}}
		conn, _, e := websocket.Dial(ctx, strings.Replace(root, "http:", "ws:", 1)+path+fmt.Sprintf("/stream?after=%d", after), &websocket.DialOptions{HTTPHeader: header})
		require.NoError(t, e)
		return conn
	}
	_, deniedResponse, deniedErr := websocket.Dial(ctx, strings.Replace(root, "http:", "ws:", 1)+path+"/stream", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"https://untrusted.test"}}})
	require.Error(t, deniedErr)
	require.NotNil(t, deniedResponse)
	require.Equal(t, http.StatusForbidden, deniedResponse.StatusCode)
	first, second := dial(0), dial(0)
	defer first.CloseNow()
	defer second.CloseNow()
	var reply domain.ConversationMessage
	body := fmt.Sprintf(`{"request_id":%q,"content":"Hello"}`, uuid.NewString())
	request("POST", path+"/messages", body, token, 202, &reply)
	readUntil := func(conn *websocket.Conn, kind string) domain.ConversationEvent {
		t.Helper()
		for {
			var event domain.ConversationEvent
			require.NoError(t, wsjson.Read(ctx, conn, &event))
			if event.Type == kind {
				return event
			}
		}
	}
	event := readUntil(first, "message.delta")
	same := readUntil(second, "message.delta")
	require.Equal(t, event.ID, same.ID)
	require.Contains(t, string(event.Payload), "before completion")
	require.Contains(t, <-providerRequests, "Authorized issue snapshot")
	require.Equal(t, int32(1), calls.Load())
	first.CloseNow()
	resumed := dial(event.Cursor)
	defer resumed.CloseNow()
	close(release)
	completed := readUntil(resumed, "message.completed")
	require.Greater(t, completed.Cursor, event.Cursor)
	request("POST", path+"/messages", body, token, 202, &reply)
	require.Equal(t, int32(1), calls.Load(), "replay and idempotent sends must not call the provider again")
	var history domain.ConversationHistory
	request("GET", path, "", token, 200, &history)
	require.Len(t, history.Messages, 2)
	require.NotNil(t, history.Issue)
	require.Equal(t, "CHAT-1", history.Issue.Identifier)
	require.Equal(t, "completed", history.Messages[1].Status)
	require.Equal(t, "A live response before completion.", history.Messages[1].Content)
	_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, user)
	require.NoError(t, err)
	_, _, err = resumed.Read(ctx)
	require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
	request("GET", path, "", token, 403, nil)
}
