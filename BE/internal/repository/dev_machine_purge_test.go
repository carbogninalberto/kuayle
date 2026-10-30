package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func purgeFixture(t *testing.T) (environmentRaceFixture, *sqlx.DB) {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return newEnvironmentRaceFixture(t, db), db
}

func machineExists(t *testing.T, db *sqlx.DB, machineID uuid.UUID) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.Get(&exists, `SELECT EXISTS(SELECT 1 FROM dev_machines WHERE id=$1)`, machineID))
	return exists
}

// An explicit purge removes machines destroyed moments ago; the default age
// guard still keeps recent ones, and machines that cannot be removed yet are
// reported with the reason instead of disappearing from the count silently.
func TestBulkPurgeMachinesHonoursAgeGuardAndReportsSkips(t *testing.T) {
	fixture, db := purgeFixture(t)
	ctx := context.Background()

	justDestroyed := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)
	oldDestroyed := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)
	recentExpired := fixture.insertMachine(t, domain.DevMachineStatusExpired, domain.DevMachineStatusExpired, false)
	failedWithRuntime := fixture.insertMachine(t, domain.DevMachineStatusFailed, domain.DevMachineStatusRunning, false)
	destroyedBusy := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)
	running := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err := db.Exec(`UPDATE dev_machines SET destroyed_at=now() WHERE id=$1`, justDestroyed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET destroyed_at=now()-interval '30 days',updated_at=now()-interval '30 days' WHERE id IN ($1,$2)`, oldDestroyed, destroyedBusy)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET docker_network_name='kuayle-net',updated_at=now()-interval '30 days',created_at=now()-interval '30 days' WHERE id=$1`, failedWithRuntime)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_operations(machine_id,workspace_id,action,status,generation,idempotency_key)
		VALUES($1,$2,'teardown','pending',2,'teardown:2')`, destroyedBusy, fixture.workspaceID)
	require.NoError(t, err)

	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	result, err := fixture.repository.BulkPurgeMachines(ctx, fixture.workspaceID, domain.DevMachinePurge{
		OlderThan: &cutoff, IncludeFailed: true, IncludeExpired: true,
	})
	require.NoError(t, err)
	require.Equal(t, domain.DevMachinePurgeResult{Removed: 1, SkippedRecent: 2, SkippedActive: 1, SkippedRuntime: 1}, result)
	require.False(t, machineExists(t, db, oldDestroyed))
	require.True(t, machineExists(t, db, justDestroyed), "the retention guard keeps recently destroyed machines")

	result, err = fixture.repository.BulkPurgeMachines(ctx, fixture.workspaceID, domain.DevMachinePurge{IncludeFailed: true, IncludeExpired: true})
	require.NoError(t, err)
	require.Equal(t, domain.DevMachinePurgeResult{Removed: 2, SkippedActive: 1, SkippedRuntime: 1}, result)
	require.False(t, machineExists(t, db, justDestroyed), "an explicit purge removes a machine destroyed moments ago")
	require.False(t, machineExists(t, db, recentExpired))
	require.True(t, machineExists(t, db, failedWithRuntime), "runtime resources must be torn down first")
	require.True(t, machineExists(t, db, destroyedBusy), "a queued operation still owns the machine")
	require.True(t, machineExists(t, db, running), "live machines are never purged")

	// Selected IDs narrow the purge to those machines in this workspace.
	other := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)
	kept := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)
	result, err = fixture.repository.BulkPurgeMachines(ctx, uuid.New(), domain.DevMachinePurge{MachineIDs: []uuid.UUID{other}})
	require.NoError(t, err)
	require.Zero(t, result.Removed, "another workspace cannot purge these machines")
	result, err = fixture.repository.BulkPurgeMachines(ctx, fixture.workspaceID, domain.DevMachinePurge{MachineIDs: []uuid.UUID{other}})
	require.NoError(t, err)
	require.Equal(t, 1, result.Removed)
	require.False(t, machineExists(t, db, other))
	require.True(t, machineExists(t, db, kept))
}

// Deleting a destroyed machine needs no teardown, so the request and the purge
// complete together instead of waiting for an operation that never runs.
func TestPermanentDeleteOfDestroyedMachinePurgesWithoutTeardown(t *testing.T) {
	fixture, db := purgeFixture(t)
	ctx := context.Background()
	machineID := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)

	operation, err := fixture.repository.RequestPermanentDelete(ctx, fixture.workspaceID, machineID, &fixture.userID)
	require.NoError(t, err)
	require.Nil(t, operation, "a destroyed machine has nothing to tear down")
	require.NoError(t, fixture.repository.PurgePermanentDeleteRequest(ctx, fixture.workspaceID, machineID))
	require.False(t, machineExists(t, db, machineID))
	var operations int
	require.NoError(t, db.Get(&operations, `SELECT count(*) FROM dev_machine_operations WHERE machine_id=$1`, machineID))
	require.Zero(t, operations)
}
