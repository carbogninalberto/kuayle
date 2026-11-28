package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	// ErrSudoDisabled refuses a package index refresh of a machine created
	// without sudo.
	ErrSudoDisabled = errors.New("sudo is not enabled for this machine")
	// ErrMachineNotRunning refuses work that needs the running developer
	// container.
	ErrMachineNotRunning = errors.New("the machine is not running")
)

// runningDeveloperContainer returns the container of the machine's running
// developer terminal, or "". It matches the container the machine manager
// probes.
func runningDeveloperContainer(services []domain.DevMachineService) string {
	for _, service := range services {
		if service.ServiceType == "terminal" && service.AgentRunID == nil && service.Status == "running" && service.ContainerID != nil {
			return *service.ContainerID
		}
	}
	return ""
}

// EnvironmentStatus returns the cached environment check of the machine's
// running developer container. It never runs anything in the container: the
// machine manager probes it. A machine that is not running, a container not
// probed yet and a check of an earlier container all read unknown.
func (s *DevMachineService) EnvironmentStatus(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*dto.EnvironmentStatus, error) {
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	status := &dto.EnvironmentStatus{PackageIndex: dto.PackageIndexStatus{State: domain.PackageIndexUnknown, SudoAvailable: machine.SudoEnabled}}
	if machine.Status != domain.DevMachineStatusRunning {
		return status, nil
	}
	services, err := s.store.ListServices(ctx, workspaceID, machine.ID)
	if err != nil {
		return nil, err
	}
	container := runningDeveloperContainer(services)
	if container == "" {
		return status, nil
	}
	check, err := s.store.GetEnvironmentCheck(ctx, machine.ID)
	if err != nil {
		return nil, err
	}
	if check == nil || check.ContainerID != container {
		return status, nil
	}
	index := &status.PackageIndex
	if !check.CheckedAt.IsZero() && check.CheckedAt.Unix() > 0 {
		checkedAt := check.CheckedAt
		index.CheckedAt = &checkedAt
		index.State = check.PackageIndexState
		if check.PackageIndexState == domain.PackageIndexPresent {
			index.IndexMtime = check.PackageIndexMtime
		}
	}
	if check.RefreshState != "" && check.RefreshState != domain.PackageIndexRefreshIdle {
		index.Refresh = &dto.PackageIndexRefresh{State: check.RefreshState, ExitCode: check.RefreshExitCode, Output: check.RefreshOutput,
			StartedAt: check.RefreshStartedAt, FinishedAt: check.RefreshFinishedAt}
	}
	return status, nil
}

// RefreshPackageIndex queues one "sudo apt-get update" in the machine's
// running developer container for its creator, the only user the machine
// admits (as for app ports). It is refused when the machine has no sudo.
// A refresh already queued or running is not queued twice. The machine
// manager runs it and records the exit code and output tail, read back with
// EnvironmentStatus.
func (s *DevMachineService) RefreshPackageIndex(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*dto.EnvironmentStatus, error) {
	machine, err := s.portOwner(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if !machine.SudoEnabled {
		return nil, ErrSudoDisabled
	}
	if machine.Status != domain.DevMachineStatusRunning {
		return nil, ErrMachineNotRunning
	}
	services, err := s.store.ListServices(ctx, workspaceID, machine.ID)
	if err != nil {
		return nil, err
	}
	container := runningDeveloperContainer(services)
	if container == "" {
		return nil, ErrMachineNotRunning
	}
	if _, err := s.store.RequestPackageIndexRefresh(ctx, workspaceID, machine.ID, container, userID); err != nil {
		if errors.Is(err, repository.ErrMachineNotRunning) {
			return nil, ErrMachineNotRunning
		}
		return nil, err
	}
	return s.EnvironmentStatus(ctx, workspaceID, machineID, userID)
}
