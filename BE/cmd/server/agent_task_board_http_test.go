package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// The board answers whoever may list tasks (GET /agent-tasks), with the
// same visibility, and never caches.
func TestAgentTaskBoardHTTP(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	owner, member, guest, outsider, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Board HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Board HTTP',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE)`, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Board HTTP','BRD')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'BRD-1','Board',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, owner)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: owner})
	chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	taskRepo := repository.NewAgentTaskRepository(db)
	task, err := taskRepo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Board", Provider: "codex"})
	require.NoError(t, err)
	// The task's checkout places it on fixture/repo.
	machine, installation, repositoryID := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,$3,$4,'Board HTTP','running','running',1,'','github','','','','','small',1000,2048,20)`, machine, ws, owner, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,$3,'fixture','Organization',$4)`, installation, ws, time.Now().UnixNano(), owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,$4,'fixture/repo','main')`, repositoryID, installation, ws, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/brd-1','/workspace/tasks/brd-1','ready',$6)`, task.CheckoutID, ws, machine, issue, repositoryID, task.ID.String())
	require.NoError(t, err)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "board-http-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db), mw.SSONotEnforced)
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(service.NewAgentTaskService(taskRepo, nil, nil))}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	session := func(id uuid.UUID) string {
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return raw
	}
	token := func(user uuid.UUID, scopes ...string) string {
		raw, hash, prefix, e := domain.GenerateToken()
		require.NoError(t, e)
		require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "Board", Scopes: scopes, TokenHash: hash, TokenPrefix: prefix}))
		return raw
	}
	board := func(query, auth string, status int) []domain.AgentBoardTask {
		t.Helper()
		req, e := http.NewRequest(http.MethodGet, server.URL+"/api/workspaces/"+ws.String()+"/agent-tasks/board"+query, nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode)
		if status != http.StatusOK {
			return nil
		}
		require.Equal(t, "private, no-store", res.Header.Get("Cache-Control"))
		var tasks []domain.AgentBoardTask
		require.NoError(t, json.NewDecoder(res.Body).Decode(&tasks))
		return tasks
	}

	listed := board("?repository=Fixture/Repo", session(owner), http.StatusOK)
	require.Len(t, listed, 1)
	require.Equal(t, task.ID, listed[0].TaskID)
	require.Equal(t, "BRD-1", listed[0].IssueIdentifier)
	require.Equal(t, "fixture/repo", listed[0].Repository)
	require.Equal(t, "main", listed[0].Base)
	require.NotNil(t, listed[0].ChangedFiles)
	require.NotNil(t, listed[0].Overlap)
	require.Empty(t, board("?repository=other/repo", session(owner), http.StatusOK), "another repository's board is empty")
	board("", session(owner), http.StatusBadRequest)
	board("?repository="+strings.Repeat("a", 513), session(owner), http.StatusBadRequest)
	require.Len(t, board("?repository=fixture/repo", token(owner, domain.PermAgentTasksRead), http.StatusOK), 1, "a token with agent_tasks:read reads its owner's board")
	board("?repository=fixture/repo", token(owner, domain.PermIssuesRead), http.StatusForbidden)
	require.Empty(t, board("?repository=fixture/repo", session(member), http.StatusOK), "another member does not see the creator's task")
	board("?repository=fixture/repo", session(guest), http.StatusForbidden)
	board("?repository=fixture/repo", session(outsider), http.StatusForbidden)
}
