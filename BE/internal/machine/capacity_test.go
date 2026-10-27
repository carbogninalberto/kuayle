package machine

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type capacityStoreFake struct {
	*managerStoreFake
	allocations []domain.DevMachineResourceAllocation
}

func (f *capacityStoreFake) ListResourceAllocations(context.Context) ([]domain.DevMachineResourceAllocation, error) {
	return f.allocations, nil
}

func TestHostAdmissionReservesCapacityAndQueuesWhatDoesNotFit(t *testing.T) {
	running := uuid.New()
	store := &capacityStoreFake{managerStoreFake: &managerStoreFake{}, allocations: []domain.DevMachineResourceAllocation{{MachineID: running, CPUMillis: 2000, MemoryMB: 4096}}}
	runtime := &runtimeFake{host: HostResources{CPUMillis: 4000, MemoryMB: 10240}}
	manager := &Manager{store: store, runtime: runtime}
	manager.SetCapacity(CapacityConfig{ReservedCPUMillis: 1000, ReservedMemoryMB: 2048, CPUOvercommit: 2})
	machine := func(cpu, memory int) *domain.DevMachine {
		return &domain.DevMachine{ID: uuid.New(), CPUMillis: cpu, MemoryMB: memory}
	}
	// 10240-2048 = 8192 MB for machines; 4096 already held.
	first, err := manager.admit(context.Background(), machine(2000, 4096))
	require.NoError(t, err)
	var wait *capacityWaitError
	_, err = manager.admit(context.Background(), machine(500, 512))
	require.ErrorAs(t, err, &wait, "an admitted machine not yet running still counts")
	require.Contains(t, wait.message, "memory")
	first()
	release, err := manager.admit(context.Background(), machine(500, 512))
	require.NoError(t, err)
	release()
	// CPU: (4000-1000)*2 = 6000 millicores; 2000 held.
	_, err = manager.admit(context.Background(), machine(4500, 1024))
	require.ErrorAs(t, err, &wait)
	require.Contains(t, wait.message, "CPU")
	// A paused machine restarting does not count its own allocation twice.
	restart, err := manager.admit(context.Background(), &domain.DevMachine{ID: running, CPUMillis: 2000, MemoryMB: 8192})
	require.NoError(t, err)
	restart()
	// Unknown host size or disabled admission never blocks.
	runtime.host = HostResources{}
	release, err = manager.admit(context.Background(), machine(64000, 1<<20))
	require.NoError(t, err)
	release()
	disabled := &Manager{store: store, runtime: &runtimeFake{host: HostResources{CPUMillis: 1000, MemoryMB: 1024}}}
	release, err = disabled.admit(context.Background(), machine(64000, 1<<20))
	require.NoError(t, err)
	release()
}

type deferringStoreFake struct {
	*capacityStoreFake
	deferred []string
	failed   int
}

func (f *deferringStoreFake) DeferOperation(_ context.Context, _ uuid.UUID, _, code, _ string, _ time.Duration) error {
	f.deferred = append(f.deferred, code)
	return nil
}
func (f *deferringStoreFake) FailOperation(context.Context, uuid.UUID, string, string, string, bool) (bool, error) {
	f.failed++
	return false, nil
}

func TestSpawnWithoutHostCapacityIsDeferredNotFailed(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusQueued, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, CPUMillis: 1000, MemoryMB: 16384, ExpiresAt: time.Now().Add(time.Hour)}
	store := &deferringStoreFake{capacityStoreFake: &capacityStoreFake{managerStoreFake: &managerStoreFake{machine: machine}}}
	runtime := &runtimeFake{host: HostResources{CPUMillis: 4000, MemoryMB: 8192}}
	manager := &Manager{store: store, runtime: runtime, operationSlots: make(chan struct{}, 1)}
	manager.SetCapacity(CapacityConfig{ReservedMemoryMB: 2048, CPUOvercommit: 1})
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	manager.processLeasedOperation(context.Background(), domain.DevMachineOperation{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Action: domain.DevMachineOpSpawn, Generation: 1})
	require.Equal(t, []string{"host_capacity_wait"}, store.deferred)
	require.Zero(t, store.failed, "waiting for capacity is not a failure")
	require.Zero(t, runtime.spawned)
}
