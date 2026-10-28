package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestModelServersKeepOneDefaultPerWorkspace(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, workspace, other := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,'models@example.test','Models','x')`, user)
	require.NoError(t, err)
	for _, ws := range []uuid.UUID{workspace, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Models',$2,$3)`, ws, ws.String(), user)
		require.NoError(t, err)
	}
	repo := NewDevMachineRepository(db)
	ctx := context.Background()
	openai := "https://models.example.test/v1"
	first := &domain.AgentModelServer{WorkspaceID: workspace, Name: "First", OpenAIBaseURL: &openai, EncryptedAPIKey: "sealed", DefaultModel: "a", Thinking: "high", IsDefault: true, Models: json.RawMessage(`["a"]`)}
	require.NoError(t, repo.SaveModelServer(ctx, first))
	second := &domain.AgentModelServer{WorkspaceID: workspace, Name: "Second", OpenAIBaseURL: &openai, EncryptedAPIKey: "sealed", DefaultModel: "b", Thinking: "low", IsDefault: true}
	require.NoError(t, repo.SaveModelServer(ctx, second))

	current, err := repo.GetModelServer(ctx, workspace, nil)
	require.NoError(t, err)
	require.Equal(t, second.ID, current.ID, "a new default replaces the old one")
	byID, err := repo.GetModelServer(ctx, workspace, &first.ID)
	require.NoError(t, err)
	require.False(t, byID.IsDefault)
	require.Equal(t, []string{"a"}, byID.ModelList())
	none, err := repo.GetModelServer(ctx, other, &first.ID)
	require.NoError(t, err)
	require.Nil(t, none, "servers are scoped to their workspace")
	servers, err := repo.ListModelServers(ctx, workspace)
	require.NoError(t, err)
	require.Equal(t, []string{"Second", "First"}, []string{servers[0].Name, servers[1].Name}, "the default comes first")

	moved := *first
	moved.WorkspaceID = other
	require.ErrorIs(t, repo.SaveModelServer(ctx, &moved), sql.ErrNoRows, "an update cannot move a server to another workspace")
	require.ErrorIs(t, repo.DeleteModelServer(ctx, other, first.ID), sql.ErrNoRows)
	require.NoError(t, repo.DeleteModelServer(ctx, workspace, first.ID))
}
