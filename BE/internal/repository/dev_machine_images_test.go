package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func imageUpdateOperation(machineID, workspaceID uuid.UUID, key string) *domain.DevMachineOperation {
	return &domain.DevMachineOperation{ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, IdempotencyKey: key, MaxAttempts: 3}
}

// An image update recreates containers, so it is queued only for a running
// machine that nobody is using: no other operation, no unfinished run and no
// recent activity.
func TestEnqueueImageUpdateOnlyForIdleRunningMachines(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	repo, ctx, ws := fixture.repository, context.Background(), fixture.workspaceID

	idle := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	operation := imageUpdateOperation(idle, ws, "update-images:1")
	queued, err := repo.EnqueueImageUpdate(ctx, operation)
	require.NoError(t, err)
	require.True(t, queued)
	var action, status string
	var generation int64
	require.NoError(t, db.QueryRow(`SELECT action, status, generation FROM dev_machine_operations WHERE id=$1`, operation.ID).Scan(&action, &status, &generation))
	require.Equal(t, "update_images", action)
	require.Equal(t, "pending", status)
	require.Equal(t, int64(1), generation, "the update keeps the machine generation")

	busy, err := repo.MachineBusyForImageUpdate(ctx, idle, operation.ID)
	require.NoError(t, err)
	require.False(t, busy, "the update operation itself does not make the machine busy")
	busy, err = repo.MachineBusyForImageUpdate(ctx, idle, uuid.Nil)
	require.NoError(t, err)
	require.True(t, busy, "any other queued operation does")

	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE id=$1`, operation.ID)
	require.NoError(t, err)
	queued, err = repo.EnqueueImageUpdate(ctx, imageUpdateOperation(idle, ws, "update-images:1"))
	require.NoError(t, err)
	require.False(t, queued, "an update with the same key is not queued twice")

	used := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`UPDATE dev_machines SET last_activity_at=NOW() - INTERVAL '5 minutes' WHERE id=$1`, used)
	require.NoError(t, err)
	queued, err = repo.EnqueueImageUpdate(ctx, imageUpdateOperation(used, ws, "update-images:1"))
	require.NoError(t, err)
	require.False(t, queued, "a machine used in the last 15 minutes is left alone")
	_, err = db.Exec(`UPDATE dev_machines SET last_activity_at=NOW() - INTERVAL '20 minutes' WHERE id=$1`, used)
	require.NoError(t, err)
	queued, err = repo.EnqueueImageUpdate(ctx, imageUpdateOperation(used, ws, "update-images:1"))
	require.NoError(t, err)
	require.True(t, queued)

	working := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`INSERT INTO dev_machine_operations(machine_id,workspace_id,action,status,generation,idempotency_key)
		VALUES($1,$2,'run_agent','leased',1,'run:1')`, working, ws)
	require.NoError(t, err)
	queued, err = repo.EnqueueImageUpdate(ctx, imageUpdateOperation(working, ws, "update-images:1"))
	require.NoError(t, err)
	require.False(t, queued, "a machine running an operation is left alone")

	for _, state := range [][2]domain.DevMachineStatus{
		{domain.DevMachineStatusStopped, domain.DevMachineStatusStopped},
		{domain.DevMachineStatusRunning, domain.DevMachineStatusStopped},
		{domain.DevMachineStatusPaused, domain.DevMachineStatusPaused},
	} {
		machine := fixture.insertMachine(t, state[0], state[1], false)
		queued, err = repo.EnqueueImageUpdate(ctx, imageUpdateOperation(machine, ws, "update-images:1"))
		require.NoError(t, err)
		require.False(t, queued, "%s machine wanting %s", state[0], state[1])
	}
	queued, err = repo.EnqueueImageUpdate(ctx, imageUpdateOperation(idle, uuid.New(), "update-images:2"))
	require.NoError(t, err)
	require.False(t, queued, "the machine is scoped to its workspace")
}
