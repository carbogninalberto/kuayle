package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type resizeStoreFake struct {
	*devMachineStoreFake
	requested  *domain.DevMachineResize
	operation  *domain.DevMachineOperation
	requestErr error
}

func (f *resizeStoreFake) RequestResizeForUser(_ context.Context, workspaceID, machineID, userID uuid.UUID, size domain.DevMachineResize, operation *domain.DevMachineOperation) (*domain.DevMachine, error) {
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	machine, _ := f.GetMachineForUser(context.Background(), workspaceID, machineID, userID)
	if machine == nil {
		return nil, nil
	}
	f.requested, f.operation = &size, operation
	operation.Generation = machine.Generation
	machine.PendingCPUMillis, machine.PendingMemoryMB, machine.PendingDiskGB = &size.CPUMillis, &size.MemoryMB, &size.DiskGB
	return machine, nil
}

func newResizeFixture(status domain.DevMachineStatus) (*resizeStoreFake, *DevMachineService, uuid.UUID, uuid.UUID, uuid.UUID) {
	workspaceID, ownerID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &resizeStoreFake{devMachineStoreFake: &devMachineStoreFake{
		policy: testPolicy(workspaceID),
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &ownerID, Status: status, DesiredStatus: status,
			Generation: 3, MachineSize: "small", CPUMillis: 2000, MemoryMB: 4096, DiskGB: 40,
		},
	}}
	svc := NewDevMachineService(store, nil, true, "machines.example.test", make([]byte, 32), time.Minute, DevMachineImages{})
	svc.SetDiskQuotaMode("monitored")
	return store, svc, workspaceID, ownerID, machineID
}

func resizeRequest(cpu, memory, disk int) dto.UpdateDevMachineRequest {
	return dto.UpdateDevMachineRequest{CPUMillis: &cpu, MemoryMB: &memory, DiskGB: &disk}
}

func TestResizeQueuesValidatedSizeForOwnerOfSettledMachine(t *testing.T) {
	for _, status := range []domain.DevMachineStatus{domain.DevMachineStatusRunning, domain.DevMachineStatusPaused, domain.DevMachineStatusStopped} {
		t.Run(string(status), func(t *testing.T) {
			store, svc, workspaceID, ownerID, machineID := newResizeFixture(status)
			machine, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(4000, 16384, 50))
			require.NoError(t, err)
			require.Equal(t, &domain.DevMachineResize{CPUMillis: 4000, MemoryMB: 16384, DiskGB: 50}, store.requested)
			require.Equal(t, domain.DevMachineOpResize, store.operation.Action)
			require.Equal(t, &ownerID, store.operation.RequestedByUserID)
			require.Equal(t, 16384, *machine.PendingMemoryMB)
			require.Equal(t, 4096, machine.MemoryMB, "the manager applies the size")
			require.Len(t, store.events, 1)
			require.Equal(t, "machine.resize_requested", store.events[0].EventType)
		})
	}
}

func TestResizeKeepsUnspecifiedResources(t *testing.T) {
	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	memory := 8192
	_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, dto.UpdateDevMachineRequest{MemoryMB: &memory})
	require.NoError(t, err)
	require.Equal(t, &domain.DevMachineResize{CPUMillis: 2000, MemoryMB: 8192, DiskGB: 40}, store.requested)

	store.requested = nil
	store.machine.PendingCPUMillis = nil
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(2000, 4096, 40))
	require.NoError(t, err)
	require.Nil(t, store.requested, "the current size is not queued again")
}

func TestResizeRejectsOtherUsersAndUnsettledMachines(t *testing.T) {
	store, svc, workspaceID, _, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	_, err := svc.Update(context.Background(), workspaceID, machineID, uuid.New(), resizeRequest(4000, 8192, 40))
	require.ErrorIs(t, err, ErrMachineNotFound)
	require.Nil(t, store.requested)

	for _, machine := range []domain.DevMachine{
		{Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusStopped},
		{Status: domain.DevMachineStatusSpawning, DesiredStatus: domain.DevMachineStatusRunning},
		{Status: domain.DevMachineStatusDestroyed, DesiredStatus: domain.DevMachineStatusDestroyed},
		{Status: domain.DevMachineStatusFailed, DesiredStatus: domain.DevMachineStatusFailed},
	} {
		store, svc, workspaceID, ownerID, machineID := newResizeFixture(machine.Status)
		store.machine.DesiredStatus = machine.DesiredStatus
		_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(4000, 8192, 40))
		require.ErrorIs(t, err, ErrInvalidOperation, "%s -> %s", machine.Status, machine.DesiredStatus)
		require.Nil(t, store.requested)
	}

	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	store.requestErr = repository.ErrResizePending
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(4000, 8192, 40))
	require.ErrorIs(t, err, ErrInvalidOperation)
	store.requestErr = repository.ErrMachineStateConflict
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(4000, 8192, 40))
	require.ErrorIs(t, err, ErrInvalidOperation)
}

func TestResizeValidatesOptionsAndWorkspaceDiskLimit(t *testing.T) {
	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(1500, 8192, 40))
	require.ErrorIs(t, err, ErrInvalidMachineInput, "CPU comes from the supported options")
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(8000, 4096, 40))
	require.ErrorIs(t, err, ErrInvalidMachineInput, "1 GiB per vCPU")
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(2000, 4096, 150))
	require.ErrorIs(t, err, ErrMachineQuota, "the workspace limits disk at 100 GB")
	require.Nil(t, store.requested)

	// A machine created before the policy limit was lowered keeps its disk.
	store.policy.MaxDiskGB = 30
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(4000, 8192, 40))
	require.NoError(t, err)
}

func TestResizeChangesDiskOnlyWithMonitoredQuotas(t *testing.T) {
	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusStopped)
	svc.SetDiskQuotaMode("hard")
	_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(2000, 4096, 50))
	require.ErrorIs(t, err, ErrInvalidOperation, "a hard quota is fixed when the volume is created")
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(4000, 8192, 40))
	require.NoError(t, err, "CPU and memory still resize")
	require.Equal(t, 40, store.requested.DiskGB)
}

func TestResizeShrinksDiskOnlyWhenUsageFits(t *testing.T) {
	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(2000, 4096, 20))
	require.ErrorIs(t, err, ErrInvalidOperation, "unknown usage cannot be shrunk safely")

	store.resourceSamples = []domain.DevMachineResourceSample{{DiskBytes: 19 << 30}}
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(2000, 4096, 20))
	require.ErrorIs(t, err, ErrInvalidOperation, "19 GB leaves no room in a 20 GB disk")
	require.Nil(t, store.requested)

	store.resourceSamples = []domain.DevMachineResourceSample{{DiskBytes: 5 << 30}}
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, resizeRequest(2000, 4096, 20))
	require.NoError(t, err)
	require.Equal(t, 20, store.requested.DiskGB)
}
