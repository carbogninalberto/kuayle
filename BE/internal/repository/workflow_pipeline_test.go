package repository

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A pipeline is saved as its own kind of workflow: validated by its own
// rules in the saving transaction, and its roster and lead keep their
// profiles in use like a graph's steps do.
func TestPipelineWorkflowIsValidatedAndKeepsItsProfiles(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Owner','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Pipelines',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	repo := NewWorkflowRepository(db)
	_, reviewer := builtinIDs(t, repo, ws)
	profile := func(name, behaviour string) uuid.UUID {
		t.Helper()
		created, err := repo.CreateProfile(ctx, ws, owner, domain.AgentProfileInput{Name: name, Behaviour: behaviour, Harness: "codex"})
		require.NoError(t, err)
		return created.ID
	}
	writer, security, lead := profile("Writer", domain.AgentBehaviourChangesCode), profile("Security", domain.AgentBehaviourReviews), profile("Lead", domain.AgentBehaviourPlans)

	p := domain.TeamPipeline(writer, reviewer)
	p.Roster = append(p.Roster, domain.PipelineMember{Role: "security", ProfileID: security, OnCall: true})
	p.Lead = &domain.PipelineLead{ProfileID: lead}

	bad := p
	bad.Roster = slices.Clone(p.Roster)
	bad.Roster[2].ProfileID = uuid.New()
	bad.Budget.OnExhausted = "explode"
	raw, err := json.Marshal(bad)
	require.NoError(t, err)
	_, err = repo.CreateWorkflow(ctx, ws, owner, "Team", "", raw, "created")
	require.Equal(t, []string{"missing_profile", "invalid_budget"}, workflowIssueCodes(err), "every issue of the pipeline's own rules")

	raw, err = json.Marshal(p)
	require.NoError(t, err)
	created, err := repo.CreateWorkflow(ctx, ws, owner, "Team", "", raw, "created")
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowKindPipeline, domain.WorkflowKind(created.Graph))
	stored, err := domain.ParsePipeline(created.Graph)
	require.NoError(t, err)
	require.Equal(t, p, *stored)

	for _, used := range []uuid.UUID{security, lead} {
		require.ErrorIs(t, repo.ArchiveProfile(ctx, ws, used, owner), ErrAgentProfileInUse)
	}
	_, err = repo.UpdateProfile(ctx, ws, writer, owner, domain.AgentProfileInput{Name: "Writer", Behaviour: domain.AgentBehaviourReviews, Harness: "codex"})
	require.ErrorIs(t, err, ErrAgentProfileBehaviourInUse)
}
