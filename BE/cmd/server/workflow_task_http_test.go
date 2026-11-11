package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
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

// The task's workflow view carries its agent runs for the timeline: to the
// task's creator only, and without summaries the reader already has.
func TestTaskWorkflowHTTPReturnsRunsToTheCreatorOnly(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err)
	defer db.Close()
	user, other, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Task timeline','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Task timeline',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member'),($1,$3,'admin')`, ws, user, other)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task timeline','TML')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'TML-1','Timeline',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	taskRepo := repository.NewAgentTaskRepository(db)
	task, err := taskRepo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
	require.NoError(t, err)
	machine := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,$3,$4,'Timeline','running','running',1,'','github','','','','','small',1000,2048,20)`, machine, ws, user, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,model,mode,prompt,command_argv,max_runtime_seconds,status,summary,result,encrypted_output_secrets,completed_at) VALUES($1,$2,$3,$4,$5,'codex','gpt-5','autonomous','Secret prompt','["codex"]',60,'succeeded','Search added','{"final_message":true}','sealed',now())`, task.RunID, ws, machine, task.ID, user)
	require.NoError(t, err)

	secret := "task-timeline-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, repository.NewPersonalAccessTokenRepository(db)))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return true })
	registry := agent.NewRegistry(agent.NewCodexProvider("test"))
	machineSvc := service.NewDevMachineService(repository.NewDevMachineRepository(db), registry, true, "machines.test.invalid", make([]byte, 32), time.Minute, service.DevMachineImages{})
	taskSvc := service.NewAgentTaskService(taskRepo, machineSvc, nil)
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(taskSvc), devMachine: handler.NewDevMachineHandler(machineSvc),
		workflow: handler.NewWorkflowHandler(service.NewWorkflowService(repository.NewWorkflowRepository(db), registry, taskSvc))}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	get := func(query string, id uuid.UUID, status int, target any) {
		t.Helper()
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		req, e := http.NewRequest("GET", server.URL+"/api/workspaces/"+ws.String()+"/agent-tasks/"+task.ID.String()+"/workflow"+query, nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+raw)
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode)
		if target != nil {
			require.NoError(t, json.NewDecoder(res.Body).Decode(target))
		}
	}
	var view struct {
		Runs []json.RawMessage `json:"runs"`
	}
	get("", user, 200, &view)
	require.Len(t, view.Runs, 1)
	var run domain.WorkflowStepRun
	require.NoError(t, json.Unmarshal(view.Runs[0], &run))
	require.Equal(t, task.RunID, run.ID)
	require.Equal(t, "codex", run.Provider)
	require.Equal(t, "gpt-5", run.Model)
	require.Equal(t, "Search added", *run.Summary)
	require.NotContains(t, string(view.Runs[0]), "Secret prompt")
	require.NotContains(t, string(view.Runs[0]), "sealed")

	// A reader that has the run asks without the summary it already holds.
	get("?summaries_since="+url.QueryEscape(run.CompletedAt.Format(time.RFC3339Nano)), user, 200, &view)
	require.Len(t, view.Runs, 1)
	require.NotContains(t, string(view.Runs[0]), "Search added")
	get("?summaries_since=yesterday", user, 400, nil)

	// Another member, an admin included, reads nothing of the task.
	get("", other, 404, nil)
}
