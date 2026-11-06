package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Research settings start empty; only current owners and admins write them,
// and each workspace keeps its own (and its own search key).
func TestAssistantResearchSettingsAreForAdmins(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, admin, member, guest, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Research','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	ws, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Research',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
	}
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, other, outsider)
	require.NoError(t, err)
	repo := NewAssistantResearchSettingsRepository(db)

	settings, err := repo.Get(ctx, ws)
	require.NoError(t, err)
	require.Nil(t, settings, "no row until an admin saves")

	key := "encrypted-key"
	saved := &domain.AssistantResearchSettings{WorkspaceID: ws, SearchProvider: "tavily", SearchAPIKeyEncrypted: &key, SubagentModel: "small", SubagentMaxParallel: 2}
	require.NoError(t, repo.Save(ctx, saved, admin))
	settings, err = repo.Get(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, "tavily", settings.SearchProvider)
	require.Equal(t, key, *settings.SearchAPIKeyEncrypted)
	require.Equal(t, "small", settings.SubagentModel)
	require.Equal(t, 2, settings.SubagentMaxParallel)
	require.Equal(t, admin, *settings.UpdatedBy)

	for _, user := range []uuid.UUID{member, guest, outsider} {
		attempt := &domain.AssistantResearchSettings{WorkspaceID: ws, SearchProvider: "brave", SubagentMaxParallel: 8}
		require.ErrorIs(t, repo.Save(ctx, attempt, user), ErrAssistantResearchSettingsDenied)
	}
	settings, err = repo.Get(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, "tavily", settings.SearchProvider, "refused writes change nothing")
	none, err := repo.Get(ctx, other)
	require.NoError(t, err)
	require.Nil(t, none, "another workspace keeps its own settings")

	settings.SearchAPIKeyEncrypted = nil
	require.NoError(t, repo.Save(ctx, settings, owner))
	settings, err = repo.Get(ctx, ws)
	require.NoError(t, err)
	require.Nil(t, settings.SearchAPIKeyEncrypted, "the key can be removed")

	_, err = db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, ws, admin)
	require.NoError(t, err)
	require.ErrorIs(t, repo.Save(ctx, settings, admin), ErrAssistantResearchSettingsDenied, "a demoted admin loses the setting in the same statement")
}
