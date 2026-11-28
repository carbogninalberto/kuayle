package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

// Creating a workflow from a pipeline template (KUA-290) through
// POST /workflows: a workspace manager gets the pipeline version of
// Standard, Review loop or Full cycle while pipelines are on; a member may
// not create workflows; with pipelines off nothing is created.
func TestCreateFromPipelineTemplateHTTP(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err)
	defer db.Close()
	owner, member, ws := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Pipeline template','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Pipeline template',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, ws, owner, member)
	require.NoError(t, err)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "pipeline-template-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db), mw.SSONotEnforced)
	// A save checks every role's profile can start a task, as main wires it:
	// the template's built-in profiles run Claude Code.
	registry := agent.NewRegistry(agent.NewClaudeCodeProvider("test"))
	machines := service.NewDevMachineService(repository.NewDevMachineRepository(db), registry, true, "", nil, time.Minute, service.DevMachineImages{})
	tasks := service.NewAgentTaskService(repository.NewAgentTaskRepository(db), machines, nil)
	e := echo.New()
	registerRoutes(e, &appHandlers{workflow: handler.NewWorkflowHandler(service.NewWorkflowService(repository.NewWorkflowRepository(db), registry, tasks))}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	session := func(id uuid.UUID) string {
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return raw
	}
	type created struct {
		Name  string          `json:"name"`
		Graph json.RawMessage `json:"graph"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	post := func(user uuid.UUID, template string, status int) created {
		t.Helper()
		body, e := json.Marshal(map[string]string{"name": template, "template": template})
		require.NoError(t, e)
		req, e := http.NewRequest(http.MethodPost, server.URL+"/api/workspaces/"+ws.String()+"/workflows", bytes.NewReader(body))
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+session(user))
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode, template)
		var out created
		require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
		return out
	}
	count := func() int {
		var n int
		require.NoError(t, db.Get(&n, `SELECT count(*) FROM workflows WHERE workspace_id=$1`, ws))
		return n
	}
	templates := map[string][]string{
		"standard_pipeline":    {"implement", "publish", "review", "merge"},
		"review_loop_pipeline": {"implement", "publish", "review", "ci", "ship", "merge"},
		"full_cycle_pipeline":  {"implement", "publish", "review", "ci", "ship", "merge"},
	}

	// Pipelines off: refused, nothing stored.
	for template := range templates {
		require.Equal(t, "PIPELINES_DISABLED", post(owner, template, http.StatusForbidden).Error.Code)
	}
	require.Zero(t, count())

	tasks.SetPipelines(true)
	for template, phases := range templates {
		out := post(owner, template, http.StatusCreated)
		require.Equal(t, template, out.Name)
		p, err := domain.ParsePipeline(out.Graph)
		require.NoError(t, err, template)
		ids := []string{}
		for _, phase := range p.Phases {
			ids = append(ids, phase.ID)
		}
		require.Equal(t, phases, ids, template)
	}
	require.Equal(t, len(templates), count())

	// Only a manager creates workflows; an unknown pipeline template is refused.
	post(member, "review_loop_pipeline", http.StatusForbidden)
	require.Equal(t, "INVALID_WORKFLOW", post(owner, "blank_pipeline", http.StatusBadRequest).Error.Code)
	require.Equal(t, len(templates), count())
}
