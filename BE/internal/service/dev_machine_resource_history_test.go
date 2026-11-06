package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestResourceHistoryAveragesTheChosenWindow(t *testing.T) {
	workspaceID, ownerID, otherID, machineID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		policy: testPolicy(workspaceID),
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &ownerID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning,
		},
		resourceSamples: []domain.DevMachineResourceSample{{WorkspaceID: workspaceID, MachineID: machineID}},
	}
	svc := newTestDevMachineService(store)
	for window, expected := range map[string][2]time.Duration{
		"1h": {time.Hour, 30 * time.Second}, "6h": {6 * time.Hour, time.Minute}, "24h": {24 * time.Hour, 4 * time.Minute},
	} {
		before := time.Now().UTC()
		samples, err := svc.ListResourceHistory(context.Background(), workspaceID, machineID, ownerID, window)
		require.NoError(t, err, window)
		require.Len(t, samples, 1)
		require.Equal(t, machineID, store.historyMachineID)
		require.WithinDuration(t, before.Add(-expected[0]), store.historySince, 5*time.Second, window)
		require.Equal(t, expected[1], store.historyBucket, window)
	}
	_, err := svc.ListResourceHistory(context.Background(), workspaceID, machineID, ownerID, "7d")
	require.ErrorIs(t, err, ErrInvalidMachineInput)
	_, err = svc.ListResourceHistory(context.Background(), workspaceID, machineID, otherID, "1h")
	require.ErrorIs(t, err, ErrMachineNotFound, "only the machine's creator reads its usage")
}
