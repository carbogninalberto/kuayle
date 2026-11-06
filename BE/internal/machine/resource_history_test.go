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

// The usage history behind the machine charts keeps one day per machine.
func TestReconcileRecordsUsageAndKeepsOneDayOfHistory(t *testing.T) {
	machine := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, DiskGB: 10,
	}
	store := &managerStoreFake{machine: &machine, runtimeMachines: []domain.DevMachine{machine}}
	runtime := &runtimeFake{stats: map[uuid.UUID]ResourceUsage{machine.ID: {CPUPercent: 150, MemoryBytes: 1 << 30}}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	before := time.Now().UTC()
	require.NoError(t, manager.reconcile(context.Background()))
	require.Len(t, store.resourceSamples, 1)
	require.Equal(t, 150.0, store.resourceSamples[0].CPUPercent)
	require.Len(t, store.prunedSamples, 1)
	require.Equal(t, machine.ID, store.prunedSamples[0].machineID)
	require.WithinDuration(t, before.Add(-domain.DevMachineResourceRetention), store.prunedSamples[0].before, 5*time.Second)
}

func TestTeardownWipesTheUsageHistory(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning},
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), make([]byte, 32), "test")
	require.NoError(t, manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpTeardown}))
	require.Equal(t, domain.DevMachineStatusDestroyed, store.machine.Status)
	require.Equal(t, []uuid.UUID{machineID}, store.wipedSampleMachines)

	runtime.teardownErr = context.DeadlineExceeded
	store.machine.Status, store.wipedSampleMachines = domain.DevMachineStatusRunning, nil
	require.Error(t, manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpTeardown}))
	require.Empty(t, store.wipedSampleMachines, "history stays while the runtime still exists")
}
