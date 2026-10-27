package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/handler"
	"github.com/kuayle/kuayle-backend/internal/service"
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
	"github.com/kuayle/kuayle-backend/internal/dto"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestAgentTaskHTTPPrivateControlsAndRecovery(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	user, other, guest, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Task HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Task HTTP',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{user: "owner", other: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task HTTP','TASK')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'TASK-1','Task request',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "task-http-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	var demoDenied atomic.Bool
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return !demoDenied.Load() })
	machines := repository.NewDevMachineRepository(db)
	require.NoError(t, machines.UpsertPolicy(ctx, &domain.DevMachineWorkspacePolicy{WorkspaceID: ws, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10, MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 60, MaxDiskGB: 100, IdlePauseMinutes: 15, AllowedProviders: json.RawMessage(`["codex"]`), AllowedRepositories: json.RawMessage(`[]`)}))
	machineSvc := service.NewDevMachineService(machines, agent.NewRegistry(agent.NewCodexProvider("test")), true, "machines.test.invalid", make([]byte, 32), time.Minute, service.DevMachineImages{})
	taskRepo := repository.NewAgentTaskRepository(db)
	taskSvc := service.NewAgentTaskService(taskRepo, machineSvc, nil)
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(taskSvc), devMachine: handler.NewDevMachineHandler(machineSvc)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return raw
	}
	token := tokenFor(user)
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
	path := "/conversations/" + chat.ID.String() + "/tasks"
	body := fmt.Sprintf(`{"request_id":%q,"prompt":"Implement and test","provider":"codex","test_command":["npm","test"]}`, uuid.NewString())
	var task domain.AgentTask
	request("POST", path, body, token, 202, &task)
	require.NotEqual(t, uuid.Nil, task.ID)
	require.JSONEq(t, `["npm","test"]`, string(task.ValidationCommand))
	var duplicate domain.AgentTask
	request("POST", path, body, token, 202, &duplicate)
	require.Equal(t, task.ID, duplicate.ID)
	request("POST", path, strings.Replace(body, "Implement and test", "Different request", 1), token, 409, nil)
	taskPath := "/agent-tasks/" + task.ID.String()
	optionsPath := "/conversations/" + chat.ID.String() + "/task-options"
	var options service.AgentTaskOptions
	request("GET", optionsPath, "", token, 200, &options)
	require.Len(t, options.Providers, 1)
	require.Equal(t, "codex", options.Providers[0].ID)
	var page domain.AgentTaskPage
	request("GET", path, "", token, 200, &page)
	require.Len(t, page.Tasks, 1)
	request("GET", taskPath, "", token, 200, &duplicate)
	require.Equal(t, task.ID, duplicate.ID)
	var history domain.AgentTaskHistory
	request("GET", taskPath+"/history", "", token, 200, &history)
	require.Len(t, history.Transitions, 1)
	var runs domain.AgentTaskRuns
	request("GET", taskPath+"/runs", "", token, 200, &runs)
	require.Empty(t, runs.Runs)
	request("GET", taskPath+"/runs/"+uuid.NewString()+"/logs", "", token, 404, nil)
	for _, suffix := range []string{"/history?after=-1", "/runs?before=invalid"} {
		request("GET", taskPath+suffix, "", token, 400, nil)
	}
	request("POST", path, strings.TrimSuffix(body, "}")+`,"machine_id":"untrusted"}`, token, 400, nil)
	request("POST", path, body+body, token, 400, nil)
	newRequestBody := strings.Replace(body, task.RequestID.String(), uuid.NewString(), 1)
	request("POST", path, strings.Replace(newRequestBody, `["npm","test"]`, `[""]`, 1), token, 400, nil)
	request("POST", path, strings.Replace(body, `["npm","test"]`, `[]`, 1), token, 409, nil)
	request("POST", path, strings.Replace(newRequestBody, `"codex"`, `"unknown"`, 1), token, 403, nil)
	request("POST", path, strings.TrimSuffix(newRequestBody, "}")+`,"model":"not a model\" -c x"}`, token, 400, nil)
	request("POST", path, strings.TrimSuffix(newRequestBody, "}")+`,"model_server_id":"`+uuid.NewString()+`"}`, token, 400, nil)
	// A well-formed model needs a model server to come from.
	request("POST", path, strings.TrimSuffix(newRequestBody, "}")+`,"model":"deepseek-flash"}`, token, 409, nil)
	for _, auth := range []string{tokenFor(other), tokenFor(guest)} {
		request("POST", path, body, auth, 404, nil)
		request("GET", optionsPath, "", auth, 404, nil)
		for _, suffix := range []string{"", "/history", "/runs", "/runs/" + task.RunID.String() + "/logs"} {
			request("GET", taskPath+suffix, "", auth, 404, nil)
		}
		request("POST", taskPath+"/cancel", "", auth, 404, nil)
		request("POST", taskPath+"/retry", `{"request_id":"`+uuid.NewString()+`","generation":1,"prompt":"Retry"}`, auth, 404, nil)
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "Task routes denied", Scopes: []string{"dev_machine:read", "dev_machine:create", "dev_machine:manage", "issues:read"}, TokenHash: hash, TokenPrefix: prefix}))
	guarded := []struct{ method, path, body string }{{"POST", path, body}, {"GET", path, ""}, {"GET", optionsPath, ""}, {"GET", taskPath, ""}, {"GET", taskPath + "/history", ""}, {"GET", taskPath + "/runs", ""}, {"GET", taskPath + "/runs/" + task.RunID.String() + "/logs", ""}, {"POST", taskPath + "/cancel", ""}, {"POST", taskPath + "/retry", `{}`}}
	for _, route := range guarded {
		request(route.method, route.path, route.body, raw, 403, nil)
	}
	demoDenied.Store(true)
	for _, route := range guarded {
		request(route.method, route.path, route.body, token, 403, nil)
	}
	demoDenied.Store(false)
	// Simulate a worker discovering missing repository configuration, then retry
	// through the real HTTP contract. No native work is launched by this fixture.
	_, err = db.Exec(`UPDATE agent_tasks SET state='blocked' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	retry := `{"request_id":"` + uuid.NewString() + `","generation":1,"prompt":"Try configured repository"}`
	var retried domain.AgentTask
	request("POST", taskPath+"/retry", retry, token, 202, &retried)
	require.Equal(t, int64(2), retried.Generation)
	require.NotEqual(t, task.RunID, retried.RunID)
	request("POST", taskPath+"/retry", retry, token, 202, &duplicate)
	require.Equal(t, retried.RunID, duplicate.RunID)
	request("POST", taskPath+"/retry", strings.Replace(retry, "Try configured repository", "Different retry", 1), token, 409, nil)
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=false WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	request("POST", path, body, token, 202, &duplicate)
	require.Equal(t, task.RunID, duplicate.RunID)
	request("POST", taskPath+"/retry", retry, token, 202, &duplicate)
	require.Equal(t, retried.RunID, duplicate.RunID)
	request("POST", path, strings.Replace(body, task.RequestID.String(), uuid.NewString(), 1), token, 403, nil)
	// Disabling new execution must not block cleanup or historical reads.
	request("POST", taskPath+"/cancel", "", token, 204, nil)
	request("POST", taskPath+"/cancel", "", token, 204, nil)
	request("GET", taskPath, "", token, 200, &duplicate)
	require.Equal(t, "cancelling", duplicate.State)

	// Neither session nor PAT calls to older machine reads can bypass task scope.
	machineID := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at) VALUES($1,$2,$3,$4,'Task HTTP','running','running',1,'','github','','','','','small',1000,2048,20,60,now()+interval '1 hour')`, machineID, ws, user, strings.ReplaceAll(machineID.String(), "-", "")[:16])
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,$4,'codex','autonomous','Private task prompt','["codex"]',60,'cancelled')`, retried.RunID, ws, machineID, task.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_log_chunks(workspace_id,machine_id,agent_run_id,stream,sequence,content) VALUES($1,$2,$3,'stdout',1,'Private task output')`, ws, machineID, retried.RunID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_events(workspace_id,machine_id,agent_run_id,source,event_type,payload) VALUES($1,$2,$3,'agent','result','{"text":"Private task event"}')`, ws, machineID, retried.RunID)
	require.NoError(t, err)
	installationID, repoID := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,$3,'fixture','Organization',$4)`, installationID, ws, time.Now().UnixNano(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,$4,'fixture/repo','main')`, repoID, installationID, ws, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/task','/workspace/tasks/task','ready',$6)`, task.CheckoutID, ws, machineID, task.IssueID, repoID, task.ID.String())
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=true WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	manualRunBody := fmt.Sprintf(`{"provider":"codex","mode":"autonomous","prompt":"Manual request","checkout_id":%q}`, task.CheckoutID)
	checkManualScope := func(auth string) {
		var checkouts []domain.DevMachineCheckout
		request("GET", "/dev-machines/"+machineID.String()+"/checkouts", "", auth, 200, &checkouts)
		require.Empty(t, checkouts)
		request("POST", "/dev-machines/"+machineID.String()+"/agent-runs", manualRunBody, auth, 404, nil)
	}
	for _, auth := range []string{token, raw} {
		checkManualScope(auth)
		request("GET", "/agent-runs/"+retried.RunID.String(), "", auth, 404, nil)
		request("GET", "/agent-runs/"+retried.RunID.String()+"/trace", "", auth, 404, nil)
		for _, path := range []string{"/agent-runs", "/dev-machines/" + machineID.String() + "/agent-runs"} {
			var runs dto.ListResponse[domain.DevMachineAgentRun]
			request("GET", path, "", auth, 200, &runs)
			require.Empty(t, runs.Data)
			require.Zero(t, runs.TotalCount)
		}
		var logs []domain.DevMachineLogChunk
		request("GET", "/dev-machines/"+machineID.String()+"/logs", "", auth, 200, &logs)
		require.Empty(t, logs)
		var events []domain.DevMachineEvent
		request("GET", "/dev-machines/"+machineID.String()+"/events", "", auth, 200, &events)
		require.Empty(t, events)
	}
	var taskLogs domain.AgentTaskLogs
	request("GET", taskPath+"/runs/"+retried.RunID.String()+"/logs", "", token, 200, &taskLogs)
	require.Len(t, taskLogs.Logs, 1)
	_, err = db.Exec(`DELETE FROM conversations WHERE id=$1`, task.ConversationID)
	require.NoError(t, err)
	for _, auth := range []string{token, raw} {
		checkManualScope(auth)
	}
	_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, user)
	require.NoError(t, err)
	request("GET", taskPath, "", token, 403, nil)

}
