package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Tools are enabled until an owner or admin turns them off; members, guests
// and outsiders cannot change the switches, and each workspace has its own.
func TestAssistantToolSettingsAreWorkspaceSwitchesForAdmins(t *testing.T) {
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
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Tools','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	ws, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Tools',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
	}
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, other, outsider)
	require.NoError(t, err)
	repo := NewAssistantToolSettingsRepository(db)

	disabled, err := repo.Disabled(ctx, ws)
	require.NoError(t, err)
	require.Empty(t, disabled, "every tool starts enabled")

	require.NoError(t, repo.Set(ctx, ws, admin, "create_issue", false))
	require.NoError(t, repo.Set(ctx, ws, owner, "delegate_issue", false))
	disabled, err = repo.Disabled(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"create_issue": true, "delegate_issue": true}, disabled)

	for _, user := range []uuid.UUID{member, guest, outsider} {
		require.ErrorIs(t, repo.Set(ctx, ws, user, "create_issue", true), ErrAssistantToolSettingDenied)
	}
	disabled, err = repo.Disabled(ctx, other)
	require.NoError(t, err)
	require.Empty(t, disabled, "another workspace keeps its own switches")

	require.NoError(t, repo.Set(ctx, ws, owner, "create_issue", true))
	disabled, err = repo.Disabled(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"delegate_issue": true}, disabled)
	var updatedBy uuid.UUID
	require.NoError(t, db.Get(&updatedBy, `SELECT updated_by FROM assistant_tool_settings WHERE workspace_id=$1 AND tool_name='create_issue'`, ws))
	require.Equal(t, owner, updatedBy)
	require.Error(t, repo.Set(ctx, ws, owner, "Not A Tool", false), "tool names are constrained")

	// A demoted admin loses the switch in the same statement that writes it.
	_, err = db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, ws, admin)
	require.NoError(t, err)
	require.ErrorIs(t, repo.Set(ctx, ws, admin, "delegate_issue", true), ErrAssistantToolSettingDenied)
}
