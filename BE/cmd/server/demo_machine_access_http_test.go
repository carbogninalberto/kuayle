package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// In demo mode only instance administrators use Dev Machines. A custom tool's
// test run is not a Dev Machine route, yet it runs a script on a machine: a
// workspace admin who is not an instance administrator gets the same 403 as
// on the guarded routes, and an instance administrator still runs it.
func TestDemoModeRefusesCustomToolTestRun(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	sysadmin, admin, ws := uuid.New(), uuid.New(), uuid.New()
	machines := map[uuid.UUID]uuid.UUID{sysadmin: uuid.New(), admin: uuid.New()}
	for _, id := range []uuid.UUID{sysadmin, admin} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Demo','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Demo',$2,$3)`, ws, ws.String(), sysadmin)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{sysadmin: "owner", admin: "admin"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true)`, ws)
	require.NoError(t, err)
	for user, machine := range machines {
		_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,$5,'running','running',1,'','github','','','','','small',1000,2048,20)`, machine, ws, user, strings.ReplaceAll(machine.String(), "-", "")[:16], "demo-"+machine.String()[:8])
		require.NoError(t, err)
	}
	// The rule as config.DemoDevMachineAllowed applies it with PUBLIC_DEMO_MODE on.
	demoAllowed := func(user uuid.UUID) bool { return user == sysadmin }
	workspaces := repository.NewWorkspaceRepository(db)
	tools := service.NewAssistantCustomToolService(repository.NewAssistantCustomToolRepository(db), workspaces, repository.NewIssueRepository(db), repository.NewAgentTaskRepository(db))
	tools.SetMachineAccess(service.NewMachineAccess(demoAllowed))

	secret := "demo-http-test-secret-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, repository.NewPersonalAccessTokenRepository(db)))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(demoAllowed)
	e := echo.New()
	registerRoutes(e, &appHandlers{assistantTool: handler.NewAssistantCustomToolHandler(tools)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	root := server.URL + "/api/workspaces/" + ws.String()
	do := func(method, path, body string, user uuid.UUID) (int, map[string]any) {
		t.Helper()
		token, e := jwtpkg.GenerateAccessToken(user, secret)
		require.NoError(t, e)
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, e := http.NewRequest(method, root+path, reader)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(res.Body).Decode(&payload)
		return res.StatusCode, payload
	}

	// Writing tools does not use a machine and stays open to workspace admins.
	status, created := do("POST", "/assistant-tools", `{"name":"list_files","description":"List the files","parameters":{"type":"object","properties":{}},"script":"ls"}`, admin)
	require.Equal(t, 201, status)
	id := created["id"].(string)
	testRun := func(user uuid.UUID) (int, map[string]any) {
		return do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{},"machine_id":"`+machines[user].String()+`"}`, user)
	}

	directStatus, direct := do("GET", "/dev-machines", "", admin)
	require.Equal(t, 403, directStatus, "the guarded Dev Machine route refuses the admin")
	status, refused := testRun(admin)
	require.Equal(t, directStatus, status)
	require.Equal(t, direct, refused, "the same error as the direct route")
	var runs int
	require.NoError(t, db.Get(&runs, `SELECT count(*) FROM assistant_tool_runs WHERE workspace_id=$1`, ws))
	require.Zero(t, runs, "nothing is queued for the machine")

	status, run := testRun(sysadmin)
	require.Equal(t, 202, status)
	require.Equal(t, "queued", run["status"])
}
