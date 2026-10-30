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
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestAgentTaskChecksHTTPRunsTheCheckForTheCreatorOnly(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	user, other, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Task checks','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Task checks',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member'),($1,$3,'member')`, ws, user, other)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task checks','CHK')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'CHK-1','Checks',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	taskRepo := repository.NewAgentTaskRepository(db)
	task, err := taskRepo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
	require.NoError(t, err)
	machines := repository.NewDevMachineRepository(db)
	require.NoError(t, machines.UpsertPolicy(ctx, &domain.DevMachineWorkspacePolicy{WorkspaceID: ws, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10, MaxDailyAgentRuns: 25, MaxDiskGB: 100, AllowedProviders: json.RawMessage(`["codex"]`), AllowedRepositories: json.RawMessage(`[]`)}))
	machine := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,$3,$4,'Checks','running','running',1,'','github','','','','','small',1000,2048,20)`, machine, ws, user, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	installation, repo := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,$3,'fixture','Organization',$4)`, installation, ws, time.Now().UnixNano(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,$4,'fixture/repo','main')`, repo, installation, ws, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/chk-1','/workspace/tasks/chk-1','ready',$6)`, task.CheckoutID, ws, machine, issue, repo, task.ID.String())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,checkout_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,encrypted_output_secrets) VALUES($1,$2,$3,$4,$5,$6,'codex','autonomous','Implement','["codex"]',60,'succeeded','sealed')`, task.RunID, ws, machine, task.ID, user, task.CheckoutID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET machine_id=$2,state='awaiting_review' WHERE id=$1`, task.ID, machine)
	require.NoError(t, err)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "task-checks-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return true })
	machineSvc := service.NewDevMachineService(machines, agent.NewRegistry(agent.NewCodexProvider("test")), true, "machines.test.invalid", make([]byte, 32), time.Minute, service.DevMachineImages{})
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(service.NewAgentTaskService(taskRepo, machineSvc, nil)), devMachine: handler.NewDevMachineHandler(machineSvc)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return raw
	}
	request := func(method, path, body, auth string, status int, target any) {
		t.Helper()
		req, e := http.NewRequest(method, server.URL+"/api/workspaces/"+ws.String()+path, strings.NewReader(body))
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
	checks := "/agent-tasks/" + task.ID.String() + "/checks"
	list := "/agent-tasks/" + task.ID.String() + "/runs/" + task.RunID.String() + "/checks"
	token := tokenFor(user)
	var failure struct {
		Error struct{ Code string } `json:"error"`
	}
	request("POST", checks, "", token, 409, &failure)
	require.Equal(t, "NO_CHECK_COMMAND", failure.Error.Code)
	request("POST", checks, `{"command":[]}`, token, 400, nil)
	request("POST", checks, `{"command":"npm test"}`, token, 400, nil)
	request("POST", checks, `{"command":["npm"],"machine_id":"untrusted"}`, token, 400, nil)
	for _, auth := range []string{tokenFor(other)} {
		request("POST", checks, `{"command":["true"]}`, auth, 404, nil)
		request("GET", list, "", auth, 404, nil)
	}
	var step domain.DevMachineAgentRunStep
	request("POST", checks, `{"command":["sh","-c","npm test -- auth"]}`, token, 202, &step)
	require.Equal(t, "manual_check", step.StepType)
	require.Equal(t, domain.DevMachineAgentRunStepStatusQueued, step.Status)
	require.JSONEq(t, `["sh","-c","npm test -- auth"]`, string(step.CommandArgv))
	request("POST", checks, `{}`, token, 409, &failure)
	require.Equal(t, "MACHINE_BUSY", failure.Error.Code)
	var steps []domain.DevMachineAgentRunStep
	request("GET", list, "", token, 200, &steps)
	require.Len(t, steps, 1)
	require.Equal(t, step.ID, steps[0].ID)
	var current domain.AgentTask
	request("GET", "/agent-tasks/"+task.ID.String(), "", token, 200, &current)
	require.Equal(t, "awaiting_review", current.State, "a check never moves the task")

	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "Checks denied", Scopes: []string{"dev_machine:read", "dev_machine:manage"}, TokenHash: hash, TokenPrefix: prefix}))
	request("POST", checks, `{}`, raw, 403, nil)
	request("GET", list, "", raw, 403, nil)
}
