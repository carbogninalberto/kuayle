package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// The room of a pipeline task is its creator's: session-only, behind the
// demo guard, never cached, refused for a task without a pipeline, and
// closed to every other person, in or out of the workspace.
func TestTaskRoomHTTP(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	creator, owner, member, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ws, otherWS, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{creator, owner, member, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Room HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	for id, head := range map[uuid.UUID]uuid.UUID{ws: owner, otherWS: outsider} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Room HTTP',$2,$3)`, id, id.String(), head)
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, id)
	}
	for _, m := range []struct {
		ws, user uuid.UUID
		role     string
	}{{ws, owner, "owner"}, {ws, creator, "member"}, {ws, member, "member"}, {otherWS, outsider, "owner"}, {otherWS, creator, "member"}} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, m.ws, m.user, m.role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Room HTTP','ROOM')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'ROOM-1','Room',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, creator)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: creator})
	chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	machines := repository.NewDevMachineRepository(db)
	require.NoError(t, machines.UpsertPolicy(ctx, &domain.DevMachineWorkspacePolicy{WorkspaceID: ws, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10, MaxDailyAgentRuns: 25, MaxDiskGB: 100, AllowedProviders: json.RawMessage(`["codex"]`), AllowedRepositories: json.RawMessage(`[]`)}))
	taskRepo := repository.NewAgentTaskRepository(db)
	plain, err := taskRepo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Plain", Provider: "codex"})
	require.NoError(t, err)
	task, err := taskRepo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Team", Provider: "codex"})
	require.NoError(t, err)
	workflows := repository.NewWorkflowRepository(db)
	require.NoError(t, workflows.EnsureBuiltinProfiles(ctx, ws))
	profiles, err := workflows.Profiles(ctx, ws)
	require.NoError(t, err)
	builtin := map[string]uuid.UUID{}
	for _, p := range profiles {
		if p.BuiltinKey != nil {
			builtin[*p.BuiltinKey] = p.ID
		}
	}
	raw, err := json.Marshal(domain.TeamPipeline(builtin[domain.AgentBuiltinImplementer], builtin[domain.AgentBuiltinReviewer]))
	require.NoError(t, err)
	pipeline, err := workflows.CreateWorkflow(ctx, ws, owner, "Team", "", raw, "created")
	require.NoError(t, err)
	// Tasks do not start on pipelines yet: the pin is written directly.
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_version_id=(SELECT id FROM workflow_versions WHERE workflow_id=$2),workflow_node='implement',workflow_status='running' WHERE id=$1`, task.ID, pipeline.ID)
	require.NoError(t, err)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "room-http-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db), mw.SSONotEnforced)
	var demoDenied atomic.Bool
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return !demoDenied.Load() })
	machineSvc := service.NewDevMachineService(machines, agent.NewRegistry(agent.NewCodexProvider("test")), true, "machines.test.invalid", make([]byte, 32), time.Minute, service.DevMachineImages{})
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(service.NewAgentTaskService(taskRepo, machineSvc, nil))}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		raw, err := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, err)
		return raw
	}
	type errorBody struct {
		Error struct{ Code, Message string }
	}
	call := func(method, path, body, auth string, status int, target any) http.Header {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+"/api/workspaces/"+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode, "%s %s", method, path)
		if target != nil {
			require.NoError(t, json.NewDecoder(res.Body).Decode(target))
		}
		return res.Header
	}
	room := ws.String() + "/agent-tasks/" + task.ID.String() + "/room"
	token := tokenFor(creator)
	var listed struct {
		Messages []domain.TaskMessage `json:"messages"`
	}
	headers := call("GET", room, "", token, 200, &listed)
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Empty(t, listed.Messages)

	// The creator says, asks, requests, waives and grants.
	request := uuid.NewString()
	posts := []string{
		`{"kind":"say","body":"  Use the new API "}`,
		`{"kind":"ask","body":"Ready for review?","to":["implementer"],"reply_to":1}`,
		`{"kind":"request","data":{"action":"merge"},"request_id":"` + request + `"}`,
		`{"kind":"waiver","body":"Flaky job","data":{"check":"ci"}}`,
		`{"kind":"grant","data":{"check":"owner"}}`,
	}
	var sent []domain.TaskMessage
	for _, body := range posts {
		var message domain.TaskMessage
		call("POST", room, body, token, 201, &message)
		require.Equal(t, domain.RoomAuthorHuman, message.AuthorKind)
		require.Equal(t, creator, *message.AuthorUserID)
		sent = append(sent, message)
	}
	require.Equal(t, "Use the new API", sent[0].Body)
	require.Equal(t, []string{"implementer"}, []string(sent[1].ToRoles))
	require.Equal(t, "merge", sent[2].Data.Action)
	var replay domain.TaskMessage
	call("POST", room, posts[2], token, 201, &replay)
	require.Equal(t, sent[2].ID, replay.ID, "a repeated request ID returns the first post")
	// What only roles or Kuayle post, and malformed posts, are refused.
	for _, body := range []string{
		`{"kind":"verdict","data":{"verdict":"approve","commit":"abc1234"}}`,
		`{"kind":"phase","data":{"phase":"review"}}`,
		`{"kind":"fact","data":{"fact":"ci_green"}}`,
		`{"kind":"summary","body":"Digest"}`,
		`{"kind":"say","body":""}`,
		`{"kind":"say","body":"Hi","to":["ghost"]}`,
		`{"kind":"say","body":"Hi","reply_to":40}`,
	} {
		var refusal errorBody
		call("POST", room, body, token, 400, &refusal)
		require.Equal(t, "ROOM_POST_REFUSED", refusal.Error.Code, body)
	}
	for _, body := range []string{`{"kind":"say","body":"Hi","author_kind":"kuayle"}`, `{"kind":"say","body":"Hi","data":{"extra":1}}`, `not json`} {
		call("POST", room, body, token, 400, nil)
	}
	call("GET", room+"?after=3", "", token, 200, &listed)
	require.Len(t, listed.Messages, 2)
	require.Equal(t, int64(4), listed.Messages[0].Seq)
	call("GET", room+"?after=-1", "", token, 400, nil)

	// Anyone else gets nothing: the workspace's owner, another member, a
	// person outside it, and the creator or another owner through another
	// workspace.
	for _, other := range []struct {
		path, auth string
		status     int
	}{
		{room, tokenFor(owner), 404},
		{room, tokenFor(member), 404},
		{room, tokenFor(outsider), 403},
		{strings.Replace(room, ws.String(), otherWS.String(), 1), token, 404},
		{strings.Replace(room, ws.String(), otherWS.String(), 1), tokenFor(outsider), 404},
	} {
		call("GET", other.path, "", other.auth, other.status, nil)
		call("POST", other.path, posts[0], other.auth, other.status, nil)
	}
	// Tokens never reach the room, whatever their scopes.
	pat, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: creator, Name: "Room", Scopes: []string{domain.PermAgentTasksRead, "dev_machine:manage"}, TokenHash: hash, TokenPrefix: prefix}))
	call("GET", room, "", pat, 403, nil)
	call("POST", room, posts[0], pat, 403, nil)
	// The demo guard covers it like every task route.
	demoDenied.Store(true)
	call("GET", room, "", token, 403, nil)
	call("POST", room, posts[0], token, 403, nil)
	demoDenied.Store(false)

	// A task without a pipeline has no room.
	plainRoom := ws.String() + "/agent-tasks/" + plain.ID.String() + "/room"
	var refusal errorBody
	call("GET", plainRoom, "", token, 409, &refusal)
	require.Equal(t, "ROOM_UNAVAILABLE", refusal.Error.Code)
	call("POST", plainRoom, posts[0], token, 409, nil)

	// The creator's hourly budget is that of live messages.
	_, err = db.Exec(`INSERT INTO task_messages(workspace_id,task_id,seq,author_kind,author_user_id,kind,body) SELECT $1,$2,100+n,'human',$3,'say','Earlier' FROM generate_series(1,$4::int) n`, ws, task.ID, creator, domain.AgentLiveMessagesPerHour)
	require.NoError(t, err)
	call("POST", room, posts[0], token, 429, &refusal)
	require.Equal(t, "ROOM_MESSAGE_LIMIT", refusal.Error.Code)
}
