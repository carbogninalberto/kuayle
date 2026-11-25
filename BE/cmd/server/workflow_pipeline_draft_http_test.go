package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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

// Drafting a pipeline from a graph workflow (KUA-283) answers whoever may
// read the workflow, in its own workspace only, while pipelines are on, and
// saves nothing.
func TestDraftPipelineHTTP(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err)
	defer db.Close()
	owner, member, guest, outsider, ws, other := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Pipeline draft','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	for id, by := range map[uuid.UUID]uuid.UUID{ws: owner, other: outsider} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Pipeline draft',$2,$3)`, id, id.String(), by)
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member'),($1,$4,'guest'),($5,$6,'owner')`, ws, owner, member, guest, other, outsider)
	require.NoError(t, err)

	repo := repository.NewWorkflowRepository(db)
	builtins := func(workspace uuid.UUID) (uuid.UUID, uuid.UUID) {
		require.NoError(t, repo.EnsureBuiltinProfiles(context.Background(), workspace))
		profiles, e := repo.Profiles(context.Background(), workspace)
		require.NoError(t, e)
		var implementer, reviewer uuid.UUID
		for _, p := range profiles {
			if p.Behaviour == domain.AgentBehaviourChangesCode {
				implementer = p.ID
			} else if p.Reviews() {
				reviewer = p.ID
			}
		}
		return implementer, reviewer
	}
	create := func(workspace, actor uuid.UUID, name string, document any) string {
		raw, e := json.Marshal(document)
		require.NoError(t, e)
		created, e := repo.CreateWorkflow(context.Background(), workspace, actor, name, "", raw, "created")
		require.NoError(t, e)
		return created.ID.String()
	}
	implementer, reviewer := builtins(ws)
	reviewLoop := create(ws, owner, "Review loop", domain.ReviewLoopWorkflowGraph(implementer, reviewer))
	fullCycle := create(ws, owner, "Full cycle", domain.FullCycleWorkflowGraph(implementer, reviewer))
	branching := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	branching.Nodes = append(branching.Nodes, domain.WorkflowNode{ID: "fix", Type: domain.WorkflowNodeAgent, Label: "Fix", ProfileID: &implementer})
	for i := range branching.Edges {
		if branching.Edges[i].ID == "review-changes" {
			branching.Edges[i].Target = "fix"
		}
	}
	branching.Edges = append(branching.Edges, domain.WorkflowEdge{ID: "fix-ci", Source: "fix", Target: "ci", Condition: domain.WorkflowAgentFinished})
	branched := create(ws, owner, "Branching", branching)
	team := create(ws, owner, "Team", domain.TeamPipeline(implementer, reviewer))
	otherImplementer, otherReviewer := builtins(other)
	elsewhere := create(other, outsider, "Elsewhere", domain.ReviewLoopWorkflowGraph(otherImplementer, otherReviewer))

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "pipeline-draft-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db), mw.SSONotEnforced)
	tasks := service.NewAgentTaskService(repository.NewAgentTaskRepository(db), nil, nil)
	tasks.SetPipelines(true)
	e := echo.New()
	registerRoutes(e, &appHandlers{workflow: handler.NewWorkflowHandler(service.NewWorkflowService(repo, nil, tasks))}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	session := func(id uuid.UUID) string {
		raw, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return raw
	}
	type draft struct {
		Pipeline *domain.Pipeline       `json:"pipeline"`
		Reasons  []domain.WorkflowIssue `json:"reasons"`
		Error    struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	post := func(workspace uuid.UUID, workflow, auth string, status int) draft {
		t.Helper()
		req, e := http.NewRequest(http.MethodPost, server.URL+"/api/workspaces/"+workspace.String()+"/workflows/"+workflow+"/draft-pipeline", nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode)
		var out draft
		require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
		return out
	}
	phases := func(d draft) []string {
		t.Helper()
		require.NotNil(t, d.Pipeline, d.Reasons)
		require.Empty(t, d.Reasons)
		ids := []string{}
		for _, phase := range d.Pipeline.Phases {
			ids = append(ids, phase.ID)
		}
		return ids
	}
	var before struct{ Workflows, Versions int }
	require.NoError(t, db.Get(&before, `SELECT (SELECT count(*) FROM workflows WHERE workspace_id=$1) AS workflows, (SELECT count(*) FROM workflow_versions WHERE workspace_id=$1) AS versions`, ws))

	// The owner drafts from each template; Standard is the workspace's copy.
	require.Equal(t, []string{"implement", "publish", "review", "merge"}, phases(post(ws, domain.StandardWorkflowID, session(owner), http.StatusOK)))
	require.Equal(t, []string{"implement", "publish", "review", "ci", "ship", "merge"}, phases(post(ws, reviewLoop, session(owner), http.StatusOK)))
	full := post(ws, fullCycle, session(owner), http.StatusOK)
	require.Equal(t, []string{"implement", "publish", "review", "ci", "ship", "merge"}, phases(full))
	require.Equal(t, []domain.PipelineMember{{Role: "implementer", ProfileID: implementer, Writer: true}, {Role: "reviewer", ProfileID: reviewer}}, full.Pipeline.Roster)

	// A branch or a pipeline has no draft, and says why.
	refused := post(ws, branched, session(owner), http.StatusOK)
	require.Nil(t, refused.Pipeline)
	require.Equal(t, "branching", refused.Reasons[0].Code)
	require.Equal(t, "review", refused.Reasons[0].NodeID)
	require.Equal(t, "pipeline", post(ws, team, session(owner), http.StatusOK).Reasons[0].Code)

	// Whoever reads workflows may draft; a guest, an outsider and a token may not.
	require.NotNil(t, post(ws, reviewLoop, session(member), http.StatusOK).Pipeline)
	post(ws, reviewLoop, session(guest), http.StatusForbidden)
	post(ws, reviewLoop, session(outsider), http.StatusForbidden)
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Draft", Scopes: []string{domain.PermWorkflowsRead}, TokenHash: hash, TokenPrefix: prefix}))
	post(ws, reviewLoop, raw, http.StatusForbidden)
	// Another workspace's workflow is not found through this one.
	post(ws, elsewhere, session(owner), http.StatusNotFound)
	post(ws, "not-a-workflow", session(owner), http.StatusNotFound)

	// With pipelines off there is nothing to draft for.
	tasks.SetPipelines(false)
	require.Equal(t, "PIPELINES_DISABLED", post(ws, reviewLoop, session(owner), http.StatusForbidden).Error.Code)

	// Nothing was saved: no workflow, version or Standard copy.
	var after struct{ Workflows, Versions int }
	require.NoError(t, db.Get(&after, `SELECT (SELECT count(*) FROM workflows WHERE workspace_id=$1) AS workflows, (SELECT count(*) FROM workflow_versions WHERE workspace_id=$1) AS versions`, ws))
	require.Equal(t, before, after)
}
