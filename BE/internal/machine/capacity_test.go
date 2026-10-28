package machine

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
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

// A machine waiting for host capacity stops the least recently active idle
// machine the store chooses, so an always-on idle machine gives way.
func TestHostCapacityWaitStopsTheChosenIdleMachine(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusStopped, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, CPUMillis: 1000, MemoryMB: 4096, ExpiresAt: time.Now().Add(time.Hour)}
	idle := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 7}
	store := &deferringStoreFake{capacityStoreFake: &capacityStoreFake{
		managerStoreFake: &managerStoreFake{machine: machine, reclaimable: idle},
		allocations:      []domain.DevMachineResourceAllocation{{MachineID: idle.ID, CPUMillis: 1000, MemoryMB: 4096}},
	}}
	runtime := &runtimeFake{host: HostResources{CPUMillis: 4000, MemoryMB: 8192}}
	manager := &Manager{store: store, runtime: runtime, operationSlots: make(chan struct{}, 1)}
	manager.SetCapacity(CapacityConfig{ReservedMemoryMB: 2048, CPUOvercommit: 1})
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	manager.processLeasedOperation(context.Background(), domain.DevMachineOperation{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Action: domain.DevMachineOpStart, Generation: 1})
	require.Equal(t, []string{"host_capacity_wait"}, store.deferred)
	require.Equal(t, []uuid.UUID{machine.ID}, store.reclaimWaitingIDs)
	require.Len(t, store.operations, 1)
	stop := store.operations[0]
	require.Equal(t, domain.DevMachineOpStop, stop.Action)
	require.Equal(t, idle.ID, stop.MachineID)
	require.Equal(t, int64(8), stop.Generation)
	require.Equal(t, "host-capacity:8", stop.IdempotencyKey)
	require.Equal(t, []domain.DevMachineStatus{domain.DevMachineStatusStopped}, store.desired)
	require.Zero(t, runtime.started)
}

func TestCodexThreadIDIsCapturedOnlyForTaskAttempts(t *testing.T) {
	task := uuid.New()
	run := &domain.DevMachineAgentRun{ProviderID: "codex", TaskID: &task}
	started := agent.Event{Type: "thread.started", Payload: []byte(`{"type":"thread.started","thread_id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}`)}
	require.Equal(t, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", codexThreadID(run, started))
	require.Empty(t, codexThreadID(&domain.DevMachineAgentRun{ProviderID: "codex"}, started), "manual runs are not resumed")
	require.Empty(t, codexThreadID(&domain.DevMachineAgentRun{ProviderID: "claude-code", TaskID: &task}, started))
	require.Empty(t, codexThreadID(run, agent.Event{Type: "thread.started", Payload: []byte(`{"thread_id":"--last"}`)}))
	require.Empty(t, codexThreadID(run, agent.Event{Type: "item.completed", Payload: started.Payload}))
}

type purgeRuntimeFake struct {
	*runtimeFake
	purged []uuid.UUID
	image  string
}

func (f *purgeRuntimeFake) PurgeTaskSession(_ context.Context, _ *domain.DevMachine, image string, task uuid.UUID) error {
	f.purged, f.image = append(f.purged, task), image
	return nil
}

func TestPurgeTaskSessionRemovesOnlyTheTasksTranscripts(t *testing.T) {
	task, checkoutID := uuid.New(), uuid.New()
	volume := "workspace"
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusPaused, WorkspaceVolumeName: &volume}
	store := &managerStoreFake{machine: machine, checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, ScopeKey: task.String()}, services: []domain.DevMachineService{{ServiceType: "ide", ImageRef: "developer:fixture"}}}
	runtime := &purgeRuntimeFake{runtimeFake: &runtimeFake{}}
	manager := &Manager{store: store, runtime: runtime}
	require.NoError(t, manager.purgeTaskSession(context.Background(), machine, &domain.DevMachineOperation{CheckoutID: &checkoutID}))
	require.Equal(t, []uuid.UUID{task}, runtime.purged, "works while the machine is paused")
	require.Equal(t, "developer:fixture", runtime.image)
	store.checkout.ScopeKey = ""
	var terminal *terminalOperationError
	require.ErrorAs(t, manager.purgeTaskSession(context.Background(), machine, &domain.DevMachineOperation{CheckoutID: &checkoutID}), &terminal, "a manual checkout is never purged")
	require.Len(t, runtime.purged, 1)
}
