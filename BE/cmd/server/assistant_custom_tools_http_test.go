package main

import (
	"context"
	"encoding/json"
	"io"
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

// Members read custom tools and their own runs; owners and admins write,
// version, activate, test-run and delete them; guests, outsiders and personal
// access tokens are refused; names of built-in tools are refused.
func TestAssistantCustomToolsHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	owner, admin, member, guest, outsider, ws, machine := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Tools','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Tools',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true)`, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at)
 VALUES($1,$2,$3,$4,'Admin','running','running',1,'','github','','','','','small',1000,2048,20,60,now()+interval '1 hour')`, machine, ws, admin, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	workspaces := repository.NewWorkspaceRepository(db)
	tools := service.NewAssistantCustomToolService(repository.NewAssistantCustomToolRepository(db), workspaces, repository.NewIssueRepository(db), repository.NewAgentTaskRepository(db))

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "tools-http-test-secret-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, &appHandlers{assistantTool: handler.NewAssistantCustomToolHandler(tools)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Tools denied", Scopes: []string{"issues:read", "workspaces:read"}, TokenHash: hash, TokenPrefix: prefix}))
	root := server.URL + "/api/workspaces/" + ws.String()
	do := func(method, path, body, auth string) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, e := http.NewRequest(method, root+path, reader)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
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

	tool := `{"name":"run_e2e","description":"Run a Playwright spec","parameters":{"type":"object","properties":{"spec":{"type":"string","maxLength":200}},"required":["spec"]},"script":"npx playwright test \"$KUAYLE_ARG_SPEC\""}`
	for _, auth := range []string{tokenFor(member), tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("POST", "/assistant-tools", tool, auth)))
	}
	status, created := do("POST", "/assistant-tools", tool, tokenFor(admin))
	require.Equal(t, 201, status)
	require.Equal(t, "active", created["status"])
	require.Equal(t, "approval", created["run_mode"])
	require.Equal(t, "sh", created["interpreter"])
	id := created["id"].(string)
	require.Equal(t, 409, first(do("POST", "/assistant-tools", tool, tokenFor(owner))))
	for _, invalid := range []string{
		strings.Replace(tool, "run_e2e", "run_check", 1),
		strings.Replace(tool, "run_e2e", "Run E2E", 1),
		strings.Replace(tool, `"string"`, `"array"`, 1),
		strings.Replace(tool, `"script":`, `"run_mode":"auto","script":`, 1),
		strings.Replace(tool, `"script":`, `"interpreter":"perl","script":`, 1),
		strings.Replace(tool, `"script":`, `"timeout_seconds":5000,"script":`, 1),
		strings.Replace(tool, `"script":"npx`, `"script":"`+strings.Repeat("a", 65537)+`npx`, 1),
		strings.Replace(tool, `"script":`, `"extra":1,"script":`, 1),
	} {
		require.Equal(t, 400, first(do("POST", "/assistant-tools", invalid, tokenFor(owner))), invalid[:60])
	}

	status, listed := do("GET", "/assistant-tools", "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, false, listed["can_manage"])
	require.Len(t, listed["tools"], 1)
	require.NotContains(t, listed["tools"].([]any)[0].(map[string]any), "script", "lists leave scripts out")
	for _, auth := range []string{tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("GET", "/assistant-tools", "", auth)))
		require.Equal(t, 403, first(do("GET", "/assistant-tools/"+id, "", auth)))
	}
	status, got := do("GET", "/assistant-tools/"+id, "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, `npx playwright test "$KUAYLE_ARG_SPEC"`, got["script"])

	draft := strings.Replace(tool, `"script":`, `"status":"draft","read_only":true,"run_mode":"auto","script":`, 1)
	for _, auth := range []string{tokenFor(member), raw} {
		require.Equal(t, 403, first(do("PUT", "/assistant-tools/"+id, draft, auth)))
		require.Equal(t, 403, first(do("POST", "/assistant-tools/"+id+"/activate", "", auth)))
		require.Equal(t, 403, first(do("PUT", "/assistant-tools/"+id+"/enabled", `{"enabled":false}`, auth)))
		require.Equal(t, 403, first(do("POST", "/assistant-tools/"+id+"/versions/1/restore", "", auth)))
		require.Equal(t, 403, first(do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{"spec":"a"},"machine_id":"`+machine.String()+`"}`, auth)))
		require.Equal(t, 403, first(do("DELETE", "/assistant-tools/"+id, "", auth)))
	}
	status, updated := do("PUT", "/assistant-tools/"+id, draft, tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, float64(1), updated["current_version"])
	require.Equal(t, float64(2), updated["pending_version"])
	require.Equal(t, "approval", updated["run_mode"], "the draft is not in effect yet")

	// An admin test-runs the draft on their own machine; inputs are validated.
	require.Equal(t, 400, first(do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{"spec":5},"machine_id":"`+machine.String()+`"}`, tokenFor(admin))))
	require.Equal(t, 400, first(do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{"spec":"a"}}`, tokenFor(admin))))
	require.Equal(t, 409, first(do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{"spec":"a"},"machine_id":"`+machine.String()+`"}`, tokenFor(owner))), "only on the requester's own machine")
	status, run := do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{"spec":"; rm -rf /"},"machine_id":"`+machine.String()+`"}`, tokenFor(admin))
	require.Equal(t, 202, status)
	require.Equal(t, "queued", run["status"])
	require.Equal(t, float64(2), run["tool_version"], "the newest version, even a draft")
	require.Equal(t, true, run["test"])
	runID := run["id"].(string)
	require.Equal(t, 409, first(do("POST", "/assistant-tools/"+id+"/test-run", `{"inputs":{"spec":"a"},"machine_id":"`+machine.String()+`"}`, tokenFor(admin))), "the machine is busy")
	status, fetched := do("GET", "/assistant-tool-runs/"+runID, "", tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, map[string]any{"spec": "; rm -rf /"}, fetched["inputs"])
	require.Equal(t, 404, first(do("GET", "/assistant-tool-runs/"+runID, "", tokenFor(member))), "members read only their own runs")
	status, runs := do("GET", "/assistant-tools/"+id+"/runs", "", tokenFor(admin))
	require.Equal(t, 200, status)
	require.Len(t, runs["runs"], 1)

	status, activated := do("POST", "/assistant-tools/"+id+"/activate", "", tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, "auto", activated["run_mode"])
	require.Equal(t, 409, first(do("POST", "/assistant-tools/"+id+"/activate", `{"version":1}`, tokenFor(admin))))
	status, restored := do("POST", "/assistant-tools/"+id+"/versions/1/restore", "", tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, float64(3), restored["current_version"])
	require.Equal(t, "approval", restored["run_mode"])
	status, versions := do("GET", "/assistant-tools/"+id+"/versions", "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Len(t, versions["versions"], 3)
	require.Equal(t, 400, first(do("PUT", "/assistant-tools/"+id+"/enabled", `{}`, tokenFor(admin))))
	status, disabled := do("PUT", "/assistant-tools/"+id+"/enabled", `{"enabled":false}`, tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, false, disabled["enabled"])
	require.Equal(t, 204, first(do("DELETE", "/assistant-tools/"+id, "", tokenFor(owner))))
	require.Equal(t, 404, first(do("GET", "/assistant-tools/"+id, "", tokenFor(owner))))
	require.Equal(t, 404, first(do("GET", "/assistant-tool-runs/"+runID, "", tokenFor(owner))), "runs go with their tool")
}
