package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceCreateErrorReturnsTypedSlugConflict(t *testing.T) {
	err := workspaceCreateError(&pgconn.PgError{
		Code:           "23505",
		ConstraintName: "workspaces_slug_key",
	})

	require.ErrorIs(t, err, ErrWorkspaceSlugTaken)
}

func TestWorkspaceCreateErrorPreservesOtherErrors(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	otherConstraintErr := &pgconn.PgError{Code: "23505", ConstraintName: "other_key"}

	require.ErrorIs(t, workspaceCreateError(databaseErr), databaseErr)
	require.ErrorIs(t, workspaceCreateError(otherConstraintErr), otherConstraintErr)
}

func TestMembershipRoleError(t *testing.T) {
	roleFK := &pgconn.PgError{Code: "23503", ConstraintName: "workspace_members_workspace_id_role_key_fkey"}
	require.ErrorIs(t, membershipRoleError(roleFK), ErrUnknownRole)
	userFK := &pgconn.PgError{Code: "23503", ConstraintName: "workspace_members_user_id_fkey"}
	require.ErrorIs(t, membershipRoleError(userFK), userFK)
}

func TestDeleteRoleWaitsForRoleChanges(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := context.Background()
	ws, key := uuid.New(), uuid.NewString()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug) VALUES ($1,'Role lock',$2)`, ws, ws.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
		require.NoError(t, err)
	})
	_, err = db.Exec(`INSERT INTO workspace_roles(workspace_id,key,name) VALUES ($1,$2,'Review')`, ws, key)
	require.NoError(t, err)

	tx, err := beginRoleChange(ctx, db, ws)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	repo := NewWorkspaceRepository(db)
	wait, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, repo.DeleteRole(wait, ws, key), context.DeadlineExceeded)
	require.NoError(t, tx.Commit())
	require.NoError(t, repo.DeleteRole(ctx, ws, key))
}

func TestCreateBundleReturnsTypedMachineNameConflict(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	existingID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning)
	var existingName string
	require.NoError(t, db.Get(&existingName, `SELECT name FROM dev_machines WHERE id=$1`, existingID))
	machineID := uuid.New()
	routingKey := strings.ReplaceAll(machineID.String(), "-", "")[:16]
	machine := &domain.DevMachine{
		ID: machineID, WorkspaceID: fixture.workspaceID, CreatedByUserID: &fixture.userID,
		RoutingKey: routingKey, Name: existingName, Status: domain.DevMachineStatusQueued,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepoProvider: "github",
		MachineSize: "small", CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20, PidsLimit: 512,
		Labels: []byte(`{}`),
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpSpawn,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "spawn:1", MaxAttempts: 5,
	}

	err = fixture.repository.CreateBundle(context.Background(), machine, nil, nil, nil, nil, operation)

	require.ErrorIs(t, err, ErrMachineNameConflict)
}
