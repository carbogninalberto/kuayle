package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAgentModelDefaultsArePerWorkspaceAndAgent(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, workspace, other := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,'defaults@example.test','Defaults','x')`, user)
	require.NoError(t, err)
	for _, ws := range []uuid.UUID{workspace, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Defaults',$2,$3)`, ws, ws.String(), user)
		require.NoError(t, err)
	}
	repo := NewDevMachineRepository(db)
	ctx := context.Background()

	none, err := repo.GetAgentModelDefault(ctx, workspace, "claude-code")
	require.NoError(t, err)
	require.Nil(t, none)

	claude := &domain.AgentModelDefault{WorkspaceID: workspace, Provider: "claude-code", Model: "claude-opus-5-5", Thinking: "medium", UpdatedBy: &user}
	require.NoError(t, repo.SaveAgentModelDefault(ctx, claude))
	require.False(t, claude.UpdatedAt.IsZero())
	require.NoError(t, repo.SaveAgentModelDefault(ctx, &domain.AgentModelDefault{WorkspaceID: workspace, Provider: "codex", Thinking: "xhigh"}))
	require.NoError(t, repo.SaveAgentModelDefault(ctx, &domain.AgentModelDefault{WorkspaceID: workspace, Provider: "claude-code", Model: "claude-opus-5-5", Thinking: "high"}),
		"saving again replaces the agent's default")

	got, err := repo.GetAgentModelDefault(ctx, workspace, "claude-code")
	require.NoError(t, err)
	require.Equal(t, "high", got.Thinking)
	require.Nil(t, got.UpdatedBy)
	elsewhere, err := repo.GetAgentModelDefault(ctx, other, "claude-code")
	require.NoError(t, err)
	require.Nil(t, elsewhere, "defaults are scoped to their workspace")
	listed, err := repo.ListAgentModelDefaults(ctx, workspace)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	// The database enforces the same values the API accepts.
	for _, invalid := range []domain.AgentModelDefault{
		{WorkspaceID: workspace, Provider: "opencode", Model: "m"},
		{WorkspaceID: workspace, Provider: "codex", Thinking: "ultra"},
		{WorkspaceID: workspace, Provider: "codex", Model: "bad model"},
	} {
		require.Error(t, repo.SaveAgentModelDefault(ctx, &invalid), invalid)
	}

	// Run and model server rows accept the new effort levels too.
	_, err = db.Exec(`INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,encrypted_api_key,default_model,thinking) VALUES($1,'Medium','https://models.example.test/v1','sealed','m','medium')`, workspace)
	require.NoError(t, err)
	var runCheck string
	require.NoError(t, db.Get(&runCheck, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='dev_machine_agent_runs_thinking_check'`))
	require.Contains(t, runCheck, "medium")
	require.Contains(t, runCheck, "xhigh")

	require.NoError(t, repo.SaveAgentModelDefault(ctx, &domain.AgentModelDefault{WorkspaceID: workspace, Provider: "claude-code"}))
	got, err = repo.GetAgentModelDefault(ctx, workspace, "claude-code")
	require.NoError(t, err)
	require.Nil(t, got, "an empty default is removed")
	_, err = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace)
	require.NoError(t, err)
	listed, err = repo.ListAgentModelDefaults(ctx, workspace)
	require.NoError(t, err)
	require.Empty(t, listed, "defaults go with their workspace")
}
