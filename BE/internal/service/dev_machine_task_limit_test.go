package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

// The machine's own agent task limit can be changed whatever its status: it
// only decides where later tasks are placed, so nothing restarts.
func TestUpdateSetsAndClearsTheMachineTaskLimit(t *testing.T) {
	for _, status := range []domain.DevMachineStatus{
		domain.DevMachineStatusRunning, domain.DevMachineStatusPaused,
		domain.DevMachineStatusStopped, domain.DevMachineStatusFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			store, svc, workspaceID, ownerID, machineID := newResizeFixture(status)
			limit := 4
			machine, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, dto.UpdateDevMachineRequest{MaxTasks: &limit})
			require.NoError(t, err)
			require.Equal(t, []*int{&limit}, store.maxTasksUpdates)
			require.Equal(t, 4, *machine.MaxTasks)
			require.Nil(t, store.requested, "changing the limit does not queue a resize")

			machine, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, dto.UpdateDevMachineRequest{ClearMaxTasks: true})
			require.NoError(t, err)
			require.Nil(t, machine.MaxTasks, "clearing follows the workspace policy again")
			require.Len(t, store.maxTasksUpdates, 2)
			require.Nil(t, store.maxTasksUpdates[1])
			require.Len(t, store.events, 2)
			require.Equal(t, "machine.task_limit_updated", store.events[0].EventType)
		})
	}
}

func TestUpdateRejectsInvalidMachineTaskLimits(t *testing.T) {
	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	for _, invalid := range []int{0, 11, -1} {
		_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, dto.UpdateDevMachineRequest{MaxTasks: &invalid})
		require.ErrorIs(t, err, ErrInvalidMachineInput, "%d is outside 1 to 10", invalid)
	}
	limit := 3
	_, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, dto.UpdateDevMachineRequest{MaxTasks: &limit, ClearMaxTasks: true})
	require.ErrorIs(t, err, ErrInvalidMachineInput, "setting and clearing at once is ambiguous")
	require.Empty(t, store.maxTasksUpdates)

	// A request that also resizes does not apply the size when the limit is
	// invalid.
	invalid := 0
	request := resizeRequest(4000, 16384, 50)
	request.MaxTasks = &invalid
	_, err = svc.Update(context.Background(), workspaceID, machineID, ownerID, request)
	require.ErrorIs(t, err, ErrInvalidMachineInput)
	require.Nil(t, store.requested, "the resize is not queued")

	_, err = svc.Update(context.Background(), workspaceID, machineID, uuid.New(), dto.UpdateDevMachineRequest{MaxTasks: &limit})
	require.ErrorIs(t, err, ErrMachineNotFound, "only the creator changes the limit")
	require.Empty(t, store.maxTasksUpdates)
}

// One request can resize the machine and change its task limit together.
func TestUpdateResizesAndChangesTheTaskLimitTogether(t *testing.T) {
	store, svc, workspaceID, ownerID, machineID := newResizeFixture(domain.DevMachineStatusRunning)
	limit := 2
	request := resizeRequest(4000, 16384, 50)
	request.MaxTasks = &limit
	machine, err := svc.Update(context.Background(), workspaceID, machineID, ownerID, request)
	require.NoError(t, err)
	require.Equal(t, &domain.DevMachineResize{CPUMillis: 4000, MemoryMB: 16384, DiskGB: 50}, store.requested)
	require.Equal(t, []*int{&limit}, store.maxTasksUpdates)
	require.Equal(t, 2, *machine.MaxTasks)
	require.Equal(t, 16384, *machine.PendingMemoryMB)
}
