package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type environmentStoreFake struct {
	*devMachineStoreFake
	check    *domain.DevMachineEnvironmentCheck
	requests []string
}

func (f *environmentStoreFake) GetEnvironmentCheck(context.Context, uuid.UUID) (*domain.DevMachineEnvironmentCheck, error) {
	return f.check, nil
}

func (f *environmentStoreFake) RequestPackageIndexRefresh(_ context.Context, _, _ uuid.UUID, container string, _ uuid.UUID) (bool, error) {
	if f.machine.Status != domain.DevMachineStatusRunning {
		return false, repository.ErrMachineNotRunning
	}
	if f.check != nil && f.check.ContainerID == container && (f.check.RefreshState == domain.PackageIndexRefreshQueued || f.check.RefreshState == domain.PackageIndexRefreshRunning) {
		return false, nil
	}
	f.requests = append(f.requests, container)
	if f.check == nil || f.check.ContainerID != container {
		f.check = &domain.DevMachineEnvironmentCheck{ContainerID: container, PackageIndexState: domain.PackageIndexUnknown, CheckedAt: time.Unix(0, 0)}
	}
	f.check.RefreshState = domain.PackageIndexRefreshQueued
	return true, nil
}

func newEnvironmentService(t *testing.T) (*DevMachineService, *environmentStoreFake, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	svc, ports, workspaceID, userID, machineID, _ := newPortService(t)
	store := &environmentStoreFake{devMachineStoreFake: ports.devMachineStoreFake}
	svc.store = store
	return svc, store, workspaceID, userID, machineID
}

func TestEnvironmentStatusReadsTheCachedCheckOfTheRunningContainer(t *testing.T) {
	svc, store, workspaceID, userID, machineID := newEnvironmentService(t)
	ctx := context.Background()

	status, err := svc.EnvironmentStatus(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageIndexUnknown, status.PackageIndex.State, "not probed yet")
	require.Nil(t, status.PackageIndex.CheckedAt)

	checked := time.Now().Add(-10 * time.Second)
	store.check = &domain.DevMachineEnvironmentCheck{ContainerID: "developer", PackageIndexState: domain.PackageIndexAbsent, CheckedAt: checked, RefreshState: domain.PackageIndexRefreshIdle}
	status, err = svc.EnvironmentStatus(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageIndexAbsent, status.PackageIndex.State)
	require.Equal(t, checked, *status.PackageIndex.CheckedAt)
	require.False(t, status.PackageIndex.SudoAvailable)
	require.Nil(t, status.PackageIndex.Refresh)

	mtime := time.Now().Add(-time.Hour)
	exit, output := 0, "Reading package lists..."
	store.check.PackageIndexState, store.check.PackageIndexMtime = domain.PackageIndexPresent, &mtime
	store.check.RefreshState, store.check.RefreshExitCode, store.check.RefreshOutput = domain.PackageIndexRefreshSucceeded, &exit, &output
	store.machine.SudoEnabled = true
	status, err = svc.EnvironmentStatus(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageIndexPresent, status.PackageIndex.State)
	require.Equal(t, mtime, *status.PackageIndex.IndexMtime)
	require.True(t, status.PackageIndex.SudoAvailable)
	require.Equal(t, domain.PackageIndexRefreshSucceeded, status.PackageIndex.Refresh.State)
	require.Equal(t, 0, *status.PackageIndex.Refresh.ExitCode)
	require.Equal(t, output, *status.PackageIndex.Refresh.Output)

	_, err = svc.EnvironmentStatus(ctx, workspaceID, machineID, uuid.New())
	require.ErrorIs(t, err, ErrMachineNotFound, "only the machine's owner reads it")
}

func TestEnvironmentStatusIsUnknownForAStoppedMachineOrAnotherContainer(t *testing.T) {
	svc, store, workspaceID, userID, machineID := newEnvironmentService(t)
	ctx := context.Background()
	store.check = &domain.DevMachineEnvironmentCheck{ContainerID: "developer-old", PackageIndexState: domain.PackageIndexPresent, CheckedAt: time.Now()}
	status, err := svc.EnvironmentStatus(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageIndexUnknown, status.PackageIndex.State, "a check of a recreated container is stale")

	store.check.ContainerID = "developer"
	store.machine.Status = domain.DevMachineStatusStopped
	status, err = svc.EnvironmentStatus(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageIndexUnknown, status.PackageIndex.State)
}

func TestRefreshPackageIndexIsRefusedWithoutSudo(t *testing.T) {
	svc, store, workspaceID, userID, machineID := newEnvironmentService(t)
	_, err := svc.RefreshPackageIndex(context.Background(), workspaceID, machineID, userID)
	require.ErrorIs(t, err, ErrSudoDisabled)
	require.Empty(t, store.requests, "nothing is queued")
}

func TestRefreshPackageIndexIsOwnerOnlyAndNeedsARunningMachine(t *testing.T) {
	svc, store, workspaceID, userID, machineID := newEnvironmentService(t)
	store.machine.SudoEnabled = true
	ctx := context.Background()

	_, err := svc.RefreshPackageIndex(ctx, workspaceID, machineID, uuid.New())
	require.ErrorIs(t, err, ErrMachineNotFound)
	_, err = svc.RefreshPackageIndex(ctx, uuid.New(), machineID, userID)
	require.Error(t, err, "never across workspaces")

	store.machine.Status = domain.DevMachineStatusStopped
	_, err = svc.RefreshPackageIndex(ctx, workspaceID, machineID, userID)
	require.ErrorIs(t, err, ErrMachineNotRunning)
	require.Empty(t, store.requests)
}

func TestRefreshPackageIndexIsQueuedOnce(t *testing.T) {
	svc, store, workspaceID, userID, machineID := newEnvironmentService(t)
	store.machine.SudoEnabled = true
	ctx := context.Background()

	status, err := svc.RefreshPackageIndex(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageIndexRefreshQueued, status.PackageIndex.Refresh.State)
	require.Equal(t, domain.PackageIndexUnknown, status.PackageIndex.State)
	_, err = svc.RefreshPackageIndex(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, []string{"developer"}, store.requests, "a queued refresh is not queued twice")
}
