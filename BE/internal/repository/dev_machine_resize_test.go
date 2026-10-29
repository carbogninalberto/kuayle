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

func resizeOperation(machineID, workspaceID, userID uuid.UUID) *domain.DevMachineOperation {
	id := uuid.New()
	return &domain.DevMachineOperation{
		ID: id, MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpResize,
		Status: domain.DevMachineOpStatusPending, IdempotencyKey: "resize:" + id.String(),
		RequestedByUserID: &userID, MaxAttempts: 3,
	}
}

func TestRequestResizeQueuesOnSettledCreatorMachineOnly(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	size := domain.DevMachineResize{CPUMillis: 4000, MemoryMB: 16384, DiskGB: 40}

	machineID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	outsider := uuid.New()
	machine, err := fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, machineID, outsider, size, resizeOperation(machineID, fixture.workspaceID, outsider))
	require.NoError(t, err)
	require.Nil(t, machine, "only the creator resizes a machine")
	machine, err = fixture.repository.RequestResizeForUser(ctx, uuid.New(), machineID, fixture.userID, size, resizeOperation(machineID, fixture.workspaceID, fixture.userID))
	require.NoError(t, err)
	require.Nil(t, machine, "the machine is scoped to its workspace")

	operation := resizeOperation(machineID, fixture.workspaceID, fixture.userID)
	machine, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, machineID, fixture.userID, size, operation)
	require.NoError(t, err)
	require.NotNil(t, machine)
	require.Equal(t, 4000, *machine.PendingCPUMillis)
	require.Equal(t, 16384, *machine.PendingMemoryMB)
	require.Equal(t, 40, *machine.PendingDiskGB)
	require.Equal(t, 1000, machine.CPUMillis, "the applied size changes only when the manager applies it")
	require.Equal(t, int64(1), operation.Generation, "a resize keeps the machine generation so queued work stays current")
	var action string
	require.NoError(t, db.Get(&action, `SELECT action FROM dev_machine_operations WHERE id=$1`, operation.ID))
	require.Equal(t, "resize", action)

	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, machineID, fixture.userID, size, resizeOperation(machineID, fixture.workspaceID, fixture.userID))
	require.ErrorIs(t, err, ErrResizePending)

	transitioning := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusStopped, false)
	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, transitioning, fixture.userID, size, resizeOperation(transitioning, fixture.workspaceID, fixture.userID))
	require.ErrorIs(t, err, ErrMachineStateConflict, "a machine changing state is not resized")

	stopped := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, false)
	_, err = db.Exec(`INSERT INTO dev_machine_operations(machine_id,workspace_id,action,status,generation,idempotency_key)
		VALUES($1,$2,'start','pending',2,'start:2')`, stopped, fixture.workspaceID)
	require.NoError(t, err)
	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, stopped, fixture.userID, size, resizeOperation(stopped, fixture.workspaceID, fixture.userID))
	require.ErrorIs(t, err, ErrMachineStateConflict, "a queued lifecycle operation would race the resize")
	var pending *int
	require.NoError(t, db.Get(&pending, `SELECT pending_cpu_millis FROM dev_machines WHERE id=$1`, stopped))
	require.Nil(t, pending)
}

func TestResizeLeasesBesideAgentRunButNotBesideLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	leaseOwner := "resize-test-" + uuid.NewString()
	leasedFor := func(machineID uuid.UUID) []domain.DevMachineOperationAction {
		operations, err := fixture.repository.LeaseOperations(ctx, leaseOwner, 500, time.Minute)
		require.NoError(t, err)
		var actions []domain.DevMachineOperationAction
		for _, operation := range operations {
			if operation.MachineID == machineID {
				actions = append(actions, operation.Action)
			}
		}
		return actions
	}
	insertLeased := func(machineID uuid.UUID, action string) {
		_, err := db.Exec(`INSERT INTO dev_machine_operations(machine_id,workspace_id,action,status,generation,idempotency_key,lease_owner,lease_expires_at)
			VALUES($1,$2,$3,'leased',1,$4,'other-manager',NOW()+INTERVAL '5 minutes')`, machineID, fixture.workspaceID, action, action+":"+uuid.NewString())
		require.NoError(t, err)
	}
	size := domain.DevMachineResize{CPUMillis: 2000, MemoryMB: 8192, DiskGB: 20}

	running := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, running, fixture.userID, size, resizeOperation(running, fixture.workspaceID, fixture.userID))
	require.NoError(t, err)
	insertLeased(running, "run_agent")
	require.Equal(t, []domain.DevMachineOperationAction{domain.DevMachineOpResize}, leasedFor(running),
		"a long agent run does not hold back the memory it may need")

	paused := fixture.insertMachine(t, domain.DevMachineStatusPaused, domain.DevMachineStatusPaused, false)
	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, paused, fixture.userID, size, resizeOperation(paused, fixture.workspaceID, fixture.userID))
	require.NoError(t, err)
	insertLeased(paused, "start")
	require.Empty(t, leasedFor(paused), "a resize waits for lifecycle work on the machine")
}

func TestFinishResizeAppliesOrRejectsPendingSize(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	size := domain.DevMachineResize{CPUMillis: 4000, MemoryMB: 16384, DiskGB: 60}

	applied := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`INSERT INTO dev_machine_volumes(machine_id,volume_type,runtime_name,mount_path,size_limit_bytes) VALUES($1,'workspace',$2,'/workspace',$3)`,
		applied, "kuayle-workspace-"+applied.String(), int64(20)<<30)
	require.NoError(t, err)
	require.NoError(t, fixture.repository.RecordResourceLimit(ctx, applied, domain.DevMachineResourceLimitMemory, "Out of memory: the agent container reached its 0.7 GiB memory limit. Resize the machine."))
	limited, err := fixture.repository.GetMachineInternal(ctx, applied)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineResourceLimitMemory, *limited.ResourceLimitKind)
	require.NotNil(t, limited.ResourceLimitAt)
	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, applied, fixture.userID, size, resizeOperation(applied, fixture.workspaceID, fixture.userID))
	require.NoError(t, err)
	require.NoError(t, fixture.repository.FinishResize(ctx, applied, true, nil))
	machine, err := fixture.repository.GetMachineInternal(ctx, applied)
	require.NoError(t, err)
	require.Equal(t, []int{4000, 16384, 60}, []int{machine.CPUMillis, machine.MemoryMB, machine.DiskGB})
	require.Equal(t, "custom", machine.MachineSize)
	require.Nil(t, machine.PendingCPUMillis)
	require.Nil(t, machine.ResourceLimitKind, "resizing answers the recorded limit")
	var limitBytes int64
	require.NoError(t, db.Get(&limitBytes, `SELECT size_limit_bytes FROM dev_machine_volumes WHERE machine_id=$1`, applied))
	require.Equal(t, int64(60)<<30, limitBytes, "the monitored disk limit follows the new size")

	rejected := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = fixture.repository.RequestResizeForUser(ctx, fixture.workspaceID, rejected, fixture.userID, size, resizeOperation(rejected, fixture.workspaceID, fixture.userID))
	require.NoError(t, err)
	reason := "Not enough host memory"
	require.NoError(t, fixture.repository.FinishResize(ctx, rejected, false, &reason))
	machine, err = fixture.repository.GetMachineInternal(ctx, rejected)
	require.NoError(t, err)
	require.Equal(t, []int{1000, 2048, 20}, []int{machine.CPUMillis, machine.MemoryMB, machine.DiskGB})
	require.Nil(t, machine.PendingCPUMillis)
	require.Equal(t, reason, *machine.ResizeError)
}
