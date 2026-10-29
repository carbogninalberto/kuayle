package machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type resizeStoreFake struct {
	*capacityStoreFake
	finished      []bool
	finishFailure *string
	limits        []string
	failRetryable *bool
}

func (f *resizeStoreFake) FinishResize(_ context.Context, _ uuid.UUID, applied bool, failure *string) error {
	f.finished = append(f.finished, applied)
	f.finishFailure = failure
	if applied && f.machine.PendingCPUMillis != nil {
		f.machine.CPUMillis, f.machine.MemoryMB, f.machine.DiskGB = *f.machine.PendingCPUMillis, *f.machine.PendingMemoryMB, *f.machine.PendingDiskGB
	}
	f.machine.PendingCPUMillis, f.machine.PendingMemoryMB, f.machine.PendingDiskGB = nil, nil, nil
	return nil
}

func (f *resizeStoreFake) RecordResourceLimit(_ context.Context, _ uuid.UUID, kind, message string) error {
	f.limits = append(f.limits, kind+": "+message)
	return nil
}

func (f *resizeStoreFake) FailOperation(ctx context.Context, id uuid.UUID, owner, code, message string, retryable bool) (bool, error) {
	f.failRetryable = &retryable
	_, _ = f.managerStoreFake.FailOperation(ctx, id, owner, code, message, retryable)
	return false, nil
}

type resizeRuntimeFake struct {
	*runtimeFake
	updates   []domain.DevMachineResize
	updateErr error
}

func (f *resizeRuntimeFake) UpdateResources(_ context.Context, machine *domain.DevMachine) error {
	f.updates = append(f.updates, domain.DevMachineResize{CPUMillis: machine.CPUMillis, MemoryMB: machine.MemoryMB, DiskGB: machine.DiskGB})
	if f.updateErr != nil && len(f.updates) == 1 {
		return f.updateErr
	}
	return nil
}

func pendingResizeMachine(status domain.DevMachineStatus, cpu, memory, disk int) *domain.DevMachine {
	return &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: status, DesiredStatus: status, Generation: 4,
		CPUMillis: 2000, MemoryMB: 4096, DiskGB: 20, ExpiresAt: time.Now().Add(time.Hour),
		PendingCPUMillis: &cpu, PendingMemoryMB: &memory, PendingDiskGB: &disk,
	}
}

func newResizeManager(machine *domain.DevMachine, host HostResources, allocations ...domain.DevMachineResourceAllocation) (*Manager, *resizeStoreFake, *resizeRuntimeFake) {
	store := &resizeStoreFake{capacityStoreFake: &capacityStoreFake{managerStoreFake: &managerStoreFake{machine: machine}, allocations: allocations}}
	runtime := &resizeRuntimeFake{runtimeFake: &runtimeFake{host: host}}
	manager := &Manager{store: store, runtime: runtime, operationSlots: make(chan struct{}, 1)}
	manager.SetCapacity(CapacityConfig{ReservedMemoryMB: 2048, CPUOvercommit: 1})
	return manager, store, runtime
}

func processResize(manager *Manager, machine *domain.DevMachine, generation int64) {
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	manager.processLeasedOperation(context.Background(), domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Action: domain.DevMachineOpResize, Generation: generation,
	})
}

func TestResizeAppliesPendingSizeToRunningContainersLive(t *testing.T) {
	for _, status := range []domain.DevMachineStatus{domain.DevMachineStatusRunning, domain.DevMachineStatusPaused} {
		t.Run(string(status), func(t *testing.T) {
			machine := pendingResizeMachine(status, 4000, 8192, 50)
			manager, store, runtime := newResizeManager(machine, HostResources{CPUMillis: 8000, MemoryMB: 16384})
			processResize(manager, machine, 4)
			require.Equal(t, []domain.DevMachineResize{{CPUMillis: 4000, MemoryMB: 8192, DiskGB: 50}}, runtime.updates)
			require.Equal(t, []bool{true}, store.finished)
			require.Equal(t, 1, store.completedOperations)
			require.Zero(t, runtime.stopped+runtime.started, "the machine keeps running")
			require.Len(t, store.events, 1)
			require.Equal(t, "machine.resized", store.events[0].EventType)
		})
	}
}

func TestResizeOfStoppedMachineUpdatesContainersWithoutAdmission(t *testing.T) {
	machine := pendingResizeMachine(domain.DevMachineStatusStopped, 8000, 32768, 20)
	// Starting admits the size against the host later.
	manager, store, runtime := newResizeManager(machine, HostResources{CPUMillis: 2000, MemoryMB: 4096})
	processResize(manager, machine, 4)
	require.Equal(t, []domain.DevMachineResize{{CPUMillis: 8000, MemoryMB: 32768, DiskGB: 20}}, runtime.updates)
	require.Equal(t, []bool{true}, store.finished)
}

func TestResizeBeyondHostCapacityIsRejectedWithReason(t *testing.T) {
	machine := pendingResizeMachine(domain.DevMachineStatusRunning, 4000, 16384, 20)
	other := domain.DevMachineResourceAllocation{MachineID: uuid.New(), CPUMillis: 2000, MemoryMB: 8192}
	// 20480 MB host - 2048 reserved = 18432 for machines; 8192 is held by another.
	manager, store, runtime := newResizeManager(machine, HostResources{CPUMillis: 16000, MemoryMB: 20480}, other)
	processResize(manager, machine, 4)
	require.Empty(t, runtime.updates, "containers keep their limits")
	require.Equal(t, []bool{false}, store.finished)
	require.NotNil(t, store.finishFailure)
	require.Contains(t, *store.finishFailure, "host memory")
	require.NotNil(t, store.failRetryable)
	require.False(t, *store.failRetryable, "waiting would hold the size indefinitely")
	require.Zero(t, store.completedOperations)
}

func TestResizeRestoresPreviousLimitsWhenContainerUpdateFails(t *testing.T) {
	machine := pendingResizeMachine(domain.DevMachineStatusRunning, 4000, 8192, 20)
	manager, store, runtime := newResizeManager(machine, HostResources{CPUMillis: 16000, MemoryMB: 65536})
	runtime.updateErr = errors.New("docker update failed")
	processResize(manager, machine, 4)
	require.Equal(t, []domain.DevMachineResize{{CPUMillis: 4000, MemoryMB: 8192, DiskGB: 20}, {CPUMillis: 2000, MemoryMB: 4096, DiskGB: 20}}, runtime.updates)
	require.Equal(t, []bool{false}, store.finished, "the final failure clears the pending size")
	require.Equal(t, 2000, machine.CPUMillis)
}

func TestResizeAppliesAfterNewerLifecycleGeneration(t *testing.T) {
	machine := pendingResizeMachine(domain.DevMachineStatusRunning, 4000, 8192, 20)
	machine.Generation = 6
	manager, store, runtime := newResizeManager(machine, HostResources{CPUMillis: 16000, MemoryMB: 65536})
	processResize(manager, machine, 4)
	require.Len(t, runtime.updates, 1, "a stale generation does not drop a requested size")
	require.Equal(t, []bool{true}, store.finished)
}
