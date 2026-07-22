package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceDeleteWaitsForEnvironmentImageCleanup(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, true)
	repository := NewWorkspaceRepository(db)

	insertEnvironment := func(status string) uuid.UUID {
		environmentID := uuid.New()
		_, insertErr := db.Exec(`INSERT INTO dev_machine_environments
			(id,workspace_id,name,image_ref,status,source_machine_id,created_by_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, environmentID, fixture.workspaceID,
			"workspace-cleanup-"+environmentID.String(), "kuayle/dev-environment-"+environmentID.String()+":snapshot",
			status, machineID, fixture.userID)
		require.NoError(t, insertErr)
		return environmentID
	}
	insertOperation := func(environmentID uuid.UUID, status domain.DevMachineOperationStatus, leaseExpiresAt *time.Time) uuid.UUID {
		operation := &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: machineID, EnvironmentID: &environmentID, WorkspaceID: fixture.workspaceID,
			Action: domain.DevMachineOpSnapshotEnvironment, Status: domain.DevMachineOpStatusPending,
			Generation: 1, IdempotencyKey: "workspace-cleanup:" + environmentID.String(), MaxAttempts: 2,
		}
		require.NoError(t, fixture.repository.EnqueueInternalOperation(context.Background(), operation))
		if status == domain.DevMachineOpStatusLeased {
			_, updateErr := db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='workspace-test',lease_expires_at=$2 WHERE id=$1`, operation.ID, leaseExpiresAt)
			require.NoError(t, updateErr)
		}
		return operation.ID
	}

	failedID := insertEnvironment("failed")
	pendingID := insertEnvironment("pending")
	pendingOperationID := insertOperation(pendingID, domain.DevMachineOpStatusPending, nil)
	partialID := insertEnvironment("delete_requested")
	activeID := insertEnvironment("building")
	activeOperationID := insertOperation(activeID, domain.DevMachineOpStatusLeased, dmTimePtr(time.Now().Add(time.Minute)))

	err = repository.Delete(context.Background(), fixture.workspaceID)

	require.ErrorIs(t, err, ErrWorkspaceEnvironmentCleanupPending)
	var workspaceCount int
	require.NoError(t, db.Get(&workspaceCount, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, fixture.workspaceID))
	require.Equal(t, 1, workspaceCount)
	for _, environmentID := range []uuid.UUID{fixture.environmentID, failedID, pendingID, partialID} {
		var status string
		require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_environments WHERE id=$1`, environmentID))
		require.Equal(t, "delete_requested", status)
	}
	var activeStatus string
	require.NoError(t, db.Get(&activeStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, activeID))
	require.Equal(t, "building", activeStatus)
	var pendingOperationStatus, activeOperationStatus domain.DevMachineOperationStatus
	require.NoError(t, db.Get(&pendingOperationStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, pendingOperationID))
	require.NoError(t, db.Get(&activeOperationStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, activeOperationID))
	require.Equal(t, domain.DevMachineOpStatusCancelled, pendingOperationStatus)
	require.Equal(t, domain.DevMachineOpStatusLeased, activeOperationStatus)

	_, err = db.Exec(`UPDATE dev_machine_operations SET lease_expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, activeOperationID)
	require.NoError(t, err)
	require.ErrorIs(t, repository.Delete(context.Background(), fixture.workspaceID), ErrWorkspaceEnvironmentCleanupPending)
	require.NoError(t, db.Get(&activeStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, activeID))
	require.Equal(t, "delete_requested", activeStatus)
	require.NoError(t, db.Get(&activeOperationStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, activeOperationID))
	require.Equal(t, domain.DevMachineOpStatusCancelled, activeOperationStatus)

	_, err = db.Exec(`DELETE FROM dev_machine_environments WHERE workspace_id=$1 AND status='delete_requested'`, fixture.workspaceID)
	require.NoError(t, err)
	require.NoError(t, repository.Delete(context.Background(), fixture.workspaceID))
	require.NoError(t, db.Get(&workspaceCount, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, fixture.workspaceID))
	require.Zero(t, workspaceCount)
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
	existingID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	var existingName string
	require.NoError(t, db.Get(&existingName, `SELECT name FROM dev_machines WHERE id=$1`, existingID))
	machineID := uuid.New()
	routingKey := strings.ReplaceAll(machineID.String(), "-", "")[:16]
	machine := &domain.DevMachine{
		ID: machineID, WorkspaceID: fixture.workspaceID, CreatedByUserID: &fixture.userID,
		RoutingKey: routingKey, Name: existingName, Status: domain.DevMachineStatusQueued,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepoProvider: "github",
		MachineSize: "small", CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20, PidsLimit: 512,
		MaxRuntimeMinutes: 480, ServicesConfig: []byte(`{}`), Labels: []byte(`{}`), ExpiresAt: time.Now().Add(time.Hour),
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpSpawn,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "spawn:1", MaxAttempts: 5,
	}

	err = fixture.repository.CreateBundle(context.Background(), machine, nil, nil, nil, nil, nil, operation)

	require.ErrorIs(t, err, ErrMachineNameConflict)
}
