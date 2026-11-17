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

func runningMachine() domain.DevMachine {
	return domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, DiskGB: 10,
	}
}

// sampleAndWait runs one sampling pass and waits for every machine's sample.
func sampleAndWait(manager *Manager) {
	manager.sampleResources(context.Background())
	manager.operations.Wait()
}

// The sampler records every running machine, and reconcile no longer calls
// Runtime.Stats: the Docker runtime derives CPU from the gap between two
// calls, so a second caller would distort it.
func TestSamplingRecordsUsageAndReconcileDoesNotSample(t *testing.T) {
	machine := runningMachine()
	stopped := runningMachine()
	stopped.Status, stopped.DesiredStatus = domain.DevMachineStatusStopped, domain.DevMachineStatusStopped
	store := &managerStoreFake{machine: &machine, runtimeMachines: []domain.DevMachine{machine, stopped}}
	runtime := &runtimeFake{stats: map[uuid.UUID]ResourceUsage{machine.ID: {CPUPercent: 150, MemoryBytes: 1 << 30}}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Zero(t, runtime.statsCallsOf(machine.ID), "reconcile does not sample")
	require.Empty(t, store.samplesOf(machine.ID))

	sampleAndWait(manager)
	samples := store.samplesOf(machine.ID)
	require.Len(t, samples, 1)
	require.Equal(t, 150.0, samples[0].CPUPercent)
	require.Equal(t, int64(1<<30), samples[0].MemoryBytes)
	require.Empty(t, store.samplesOf(stopped.ID), "a stopped machine has nothing to sample")
	require.Equal(t, uint64(1), manager.Metrics()["resource_samples_recorded_total"])
}

// A reconcile pass stuck on one machine (a recovering machine can take
// minutes) used to stop sampling for every machine.
func TestSamplingKeepsFlowingWhileReconcileIsStuck(t *testing.T) {
	machine := runningMachine()
	store := &managerStoreFake{machine: &machine, runtimeMachines: []domain.DevMachine{machine}}
	runtime := &runtimeFake{
		inspectBlock: make(chan struct{}), inspectEntered: make(chan struct{}, 1),
		stats: map[uuid.UUID]ResourceUsage{machine.ID: {CPUPercent: 10}},
	}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	reconciled := make(chan error, 1)
	go func() { reconciled <- manager.reconcile(context.Background()) }()
	select {
	case <-runtime.inspectEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile never reached the runtime")
	}
	for i := 1; i <= 3; i++ {
		manager.sampleResources(context.Background())
		require.Eventually(t, func() bool { return len(store.samplesOf(machine.ID)) == i }, 5*time.Second, time.Millisecond,
			"sample %d is recorded while reconcile is still blocked", i)
	}
	select {
	case <-reconciled:
		t.Fatal("reconcile should still be blocked")
	default:
	}
	close(runtime.inspectBlock)
	require.NoError(t, <-reconciled)
	manager.operations.Wait()
	require.Zero(t, runtime.statsCallsOf(machine.ID)-3, "only the sampler called Stats")
}

// One machine whose Docker call hangs delays no other machine's samples, and
// is not sampled twice at once.
func TestSamplingKeepsFlowingWhileOneMachineIsSlow(t *testing.T) {
	slow, healthy := runningMachine(), runningMachine()
	store := &managerStoreFake{runtimeMachines: []domain.DevMachine{slow, healthy}}
	release := make(chan struct{})
	runtime := &runtimeFake{
		statsBlock: map[uuid.UUID]chan struct{}{slow.ID: release},
		stats:      map[uuid.UUID]ResourceUsage{healthy.ID: {CPUPercent: 5}, slow.ID: {CPUPercent: 7}},
	}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	for i := 1; i <= 3; i++ {
		manager.sampleResources(context.Background())
		require.Eventually(t, func() bool { return len(store.samplesOf(healthy.ID)) == i }, 5*time.Second, time.Millisecond,
			"the healthy machine is sampled on every pass")
	}
	require.Empty(t, store.samplesOf(slow.ID))
	require.Eventually(t, func() bool { return runtime.statsCallsOf(slow.ID) >= 1 }, 5*time.Second, time.Millisecond)
	require.Equal(t, 1, runtime.statsCallsOf(slow.ID), "a machine still being sampled is skipped, not piled up")

	close(release)
	manager.operations.Wait()
	require.Len(t, store.samplesOf(slow.ID), 1, "the slow machine records once its call returns")
	sampleAndWait(manager)
	require.Len(t, store.samplesOf(slow.ID), 2, "and is sampled again on the next pass")
}

// A failed stats call or insert is logged, counted and retried, not dropped.
func TestSamplingCountsAndRetriesFailures(t *testing.T) {
	machine := runningMachine()
	store := &managerStoreFake{runtimeMachines: []domain.DevMachine{machine}}
	runtime := &runtimeFake{stats: map[uuid.UUID]ResourceUsage{machine.ID: {CPUPercent: 1}}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")
	manager.sampleRetryDelay = time.Millisecond

	runtime.statsFailures = map[uuid.UUID]int{machine.ID: 1}
	sampleAndWait(manager)
	require.Len(t, store.samplesOf(machine.ID), 1, "the missed stats call is retried")
	require.Equal(t, 2, runtime.statsCallsOf(machine.ID))
	metrics := manager.Metrics()
	require.Equal(t, uint64(1), metrics["resource_sample_stats_failed_total"])
	require.Equal(t, uint64(1), metrics["resource_samples_recorded_total"])
	require.Zero(t, metrics["resource_samples_missed_total"])

	store.sampleInsertFailures = 1
	sampleAndWait(manager)
	require.Len(t, store.samplesOf(machine.ID), 2, "the failed insert is retried")
	require.Equal(t, uint64(1), manager.Metrics()["resource_sample_inserts_failed_total"])
	require.Equal(t, 3, runtime.statsCallsOf(machine.ID), "an insert retry does not call Stats again")

	store.sampleInsertFailures = 2
	sampleAndWait(manager)
	require.Len(t, store.samplesOf(machine.ID), 2)
	metrics = manager.Metrics()
	require.Equal(t, uint64(3), metrics["resource_sample_inserts_failed_total"])
	require.Equal(t, uint64(1), metrics["resource_samples_missed_total"], "a sample lost after the retry is counted")

	runtime.statsFailures = map[uuid.UUID]int{machine.ID: 2}
	sampleAndWait(manager)
	metrics = manager.Metrics()
	require.Equal(t, uint64(3), metrics["resource_sample_stats_failed_total"])
	require.Equal(t, uint64(2), metrics["resource_samples_missed_total"])
}

// The sampler also covers machines with a queued operation, so a machine the
// disk quota already queued to stop is not stopped again by every sample.
func TestSamplingDoesNotRepeatTheDiskQuotaStop(t *testing.T) {
	machine := runningMachine()
	store := &managerStoreFake{machine: &machine, runtimeMachines: []domain.DevMachine{machine}}
	runtime := &runtimeFake{stats: map[uuid.UUID]ResourceUsage{machine.ID: {DiskBytes: 10 << 30}}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	sampleAndWait(manager)
	require.Len(t, store.actions, 1)
	store.runtimeMachines[0].DesiredStatus = domain.DevMachineStatusStopped
	sampleAndWait(manager)
	require.Len(t, store.actions, 1, "the queued stop is not enqueued again")
	require.Len(t, store.samplesOf(machine.ID), 2, "the machine is still sampled while it stops")
}

// Retention is a rolling window for every machine: samples of a machine that
// is no longer running used to stay forever.
func TestPruneRemovesExpiredSamplesOfStoppedMachines(t *testing.T) {
	stopped := runningMachine()
	stopped.Status, stopped.DesiredStatus = domain.DevMachineStatusStopped, domain.DevMachineStatusStopped
	store := &managerStoreFake{machine: &stopped}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, nil, nil, "test")

	before := time.Now().UTC()
	manager.pruneResourceSamples(context.Background())
	require.Len(t, store.expiredPrunes, 1)
	require.WithinDuration(t, before.Add(-domain.DevMachineResourceRetention), store.expiredPrunes[0].before, 5*time.Second)
	require.Positive(t, store.expiredPrunes[0].limit)
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
