package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestAgentMessageHTTPRegistryAndModeration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	owner, member, guest, ws, team := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Message HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Message HTTP',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Messages','MSG')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	issues := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range issues {
		var parent *uuid.UUID
		if i > 0 {
			parent = &issues[0]
		}
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id,parent_id) VALUES($1,$2,$3,$4::int,'MSG-'||$4::int,'Part',$5,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1),$6)`, id, ws, team, i+1, owner, parent)
		require.NoError(t, err)
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: owner})
	tasks := repository.NewAgentTaskRepository(db)
	created := []*domain.AgentTask{}
	for _, issue := range issues[1:] {
		chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
		require.NoError(t, err)
		task, err := tasks.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
		require.NoError(t, err)
		created = append(created, task)
	}
	messages := repository.NewAgentMessageRepository(db)
	recorded, err := messages.RecordTaskOutbox(ctx, created[0].ID, []domain.AgentOutboxFile{{Worktree: "/workspace/tasks/msg-2", Name: "1.json", Data: []byte(`{"to":"MSG-3","body":"Shared types are in pkg/api"}`)}})
	require.NoError(t, err)
	require.Equal(t, 1, recorded)

	secret := "message-http-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, repository.NewPersonalAccessTokenRepository(db)))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return true })
	e := echo.New()
	registerRoutes(e, &appHandlers{agentMessage: handler.NewAgentMessageHandler(service.NewAgentMessageService(messages, nil))}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return raw
	}
	request := func(method, path, body string, user uuid.UUID, status int, target any) {
		t.Helper()
		req, e := http.NewRequest(method, server.URL+"/api/workspaces/"+ws.String()+path, strings.NewReader(body))
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+tokenFor(user))
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode, method+" "+path)
		if target != nil {
			require.NoError(t, json.NewDecoder(res.Body).Decode(target))
		}
	}

	var page domain.AgentMessagePage
	request("GET", "/agent-messages", "", owner, 200, &page)
	require.Len(t, page.Messages, 1)
	message := page.Messages[0]
	require.True(t, message.CanModerate)
	require.Equal(t, "MSG-2", message.FromIdentifier)
	require.Equal(t, "MSG-3", *message.ToIdentifier)
	request("GET", "/agent-messages?issue_id="+issues[2].String()+"&state=queued&channel=internal", "", member, 200, &page)
	require.Len(t, page.Messages, 1, "members read the registry")
	require.False(t, page.Messages[0].CanModerate)
	request("GET", "/agent-messages?task_id="+created[0].ID.String(), "", member, 200, &page)
	require.Len(t, page.Messages, 1)
	request("GET", "/agent-messages?issue=msg-3", "", member, 200, &page)
	require.Len(t, page.Messages, 1, "an issue identifier matches the recipient")
	request("GET", "/agent-messages?issue=MSG-1", "", member, 200, &page)
	require.Empty(t, page.Messages)
	request("GET", "/agent-messages?issue=not%20an%20issue", "", member, 400, nil)
	request("GET", "/agent-messages", "", guest, 404, nil)
	request("GET", "/agent-messages?state=bogus", "", owner, 400, nil)
	request("GET", "/agent-messages?issue_id=nope", "", owner, 400, nil)

	path := "/agent-messages/" + message.ID.String()
	request("POST", path+"/hold", "", member, 403, nil)
	request("POST", path+"/hold", "", guest, 404, nil)
	var held domain.AgentMessageView
	request("POST", path+"/hold", "", owner, 200, &held)
	require.Equal(t, "held", held.State)
	request("POST", path+"/hold", "", owner, 409, nil)
	request("POST", path+"/explode", "", owner, 404, nil)
	var detail struct {
		Message domain.AgentMessageView     `json:"message"`
		Actions []domain.AgentMessageAction `json:"actions"`
	}
	request("GET", path, "", member, 200, &detail)
	require.Len(t, detail.Actions, 1)
	require.Equal(t, "hold", detail.Actions[0].Action)
	request("GET", "/agent-messages/"+uuid.NewString(), "", owner, 404, nil)

	request("POST", "/agent-message-holds", `{"issue_ids":["`+issues[1].String()+`"],"extra":1}`, owner, 400, nil)
	request("POST", "/agent-message-holds", `{"issue_ids":["`+issues[1].String()+`","`+issues[2].String()+`"]}`, member, 403, nil)
	var hold domain.AgentMessageHold
	request("POST", "/agent-message-holds", `{"issue_ids":["`+issues[1].String()+`","`+issues[2].String()+`"]}`, owner, 201, &hold)
	require.Equal(t, 1, hold.HeldMessages)
	var holds struct {
		Holds []domain.AgentMessageHold `json:"holds"`
	}
	request("GET", "/agent-message-holds", "", member, 200, &holds)
	require.Len(t, holds.Holds, 1)
	require.False(t, holds.Holds[0].CanModerate)
	request("DELETE", "/agent-message-holds/"+hold.ID.String(), "", member, 403, nil)
	request("DELETE", "/agent-message-holds/"+hold.ID.String(), "", owner, 204, nil)
	request("GET", path, "", owner, 200, &detail)
	require.Equal(t, "queued", detail.Message.State, "releasing the conversation releases its held messages")
	var dropped domain.AgentMessageView
	request("POST", path+"/drop", "", owner, 200, &dropped)
	require.Equal(t, "dropped", dropped.State)
}
