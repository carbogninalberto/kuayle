package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// resizeDiskHeadroomPercent is the most of a smaller disk that current usage
// may fill: the manager warns at 90% and stops the machine at 100%.
const resizeDiskHeadroomPercent = 90

// SetDiskQuotaMode tells the service how workspace disks are limited. Only
// monitored limits can change after the volume exists; unset means hard.
func (s *DevMachineService) SetDiskQuotaMode(mode string) {
	s.diskQuotaMode = mode
}

func (s *DevMachineService) monitoredDiskQuota() bool {
	return s.diskQuotaMode == "monitored"
}

// resize validates a new size for the creator's machine and queues it. The
// size uses the same options and workspace disk limit as machine creation;
// host capacity is admitted by the manager, which applies CPU and memory to
// every machine container live and the disk limit to its monitoring.
func (s *DevMachineService) resize(ctx context.Context, workspaceID, machineID, userID uuid.UUID, request dto.UpdateDevMachineRequest) (*domain.DevMachine, error) {
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	size := domain.DevMachineResize{CPUMillis: machine.CPUMillis, MemoryMB: machine.MemoryMB, DiskGB: machine.DiskGB}
	if request.CPUMillis != nil {
		size.CPUMillis = *request.CPUMillis
	}
	if request.MemoryMB != nil {
		size.MemoryMB = *request.MemoryMB
	}
	if request.DiskGB != nil {
		size.DiskGB = *request.DiskGB
	}
	if machine.PendingCPUMillis == nil && size.CPUMillis == machine.CPUMillis && size.MemoryMB == machine.MemoryMB && size.DiskGB == machine.DiskGB {
		return machine, nil
	}
	if !domain.DevMachineResizable(machine) {
		return nil, fmt.Errorf("%w: a machine can be resized only while it is running, paused or stopped", ErrInvalidOperation)
	}
	if err := domain.DevMachineCustomSize(size.CPUMillis, size.MemoryMB, size.DiskGB); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMachineInput, err)
	}
	if size.DiskGB != machine.DiskGB {
		if err := s.validateDiskResize(ctx, machine, size.DiskGB); err != nil {
			return nil, err
		}
	}
	operationID := uuid.New()
	operation := &domain.DevMachineOperation{
		ID: operationID, MachineID: machine.ID, WorkspaceID: workspaceID, Action: domain.DevMachineOpResize,
		Status: domain.DevMachineOpStatusPending, IdempotencyKey: "resize:" + operationID.String(),
		RequestedByUserID: &userID, MaxAttempts: 3,
	}
	resized, err := s.store.RequestResizeForUser(ctx, workspaceID, machineID, userID, size, operation)
	switch {
	case errors.Is(err, repository.ErrResizePending):
		return nil, fmt.Errorf("%w: %v", ErrInvalidOperation, err)
	case errors.Is(err, repository.ErrMachineStateConflict):
		return nil, fmt.Errorf("%w: the machine is changing state; try again when it has settled", ErrInvalidOperation)
	case err != nil:
		return nil, err
	case resized == nil:
		return nil, ErrMachineNotFound
	}
	s.emit(ctx, resized, nil, &userID, "lifecycle", "machine.resize_requested", map[string]any{
		"operation_id": operation.ID,
		"from":         map[string]int{"cpu_millis": machine.CPUMillis, "memory_mb": machine.MemoryMB, "disk_gb": machine.DiskGB},
		"to":           map[string]int{"cpu_millis": size.CPUMillis, "memory_mb": size.MemoryMB, "disk_gb": size.DiskGB},
	})
	return resized, nil
}

func (s *DevMachineService) validateDiskResize(ctx context.Context, machine *domain.DevMachine, diskGB int) error {
	if !s.monitoredDiskQuota() {
		return fmt.Errorf("%w: this server enforces hard disk quotas, which are fixed when the machine is created; resize CPU and memory only", ErrInvalidOperation)
	}
	policy, err := s.enabledPolicy(ctx, machine.WorkspaceID)
	if err != nil {
		return err
	}
	if diskGB > policy.MaxDiskGB {
		return newMachineQuotaError(fmt.Sprintf(
			"machine disk request of %d GB exceeds the workspace limit of %d GB",
			diskGB, policy.MaxDiskGB,
		))
	}
	if diskGB > machine.DiskGB {
		return nil
	}
	samples, err := s.store.ListResourceSamples(ctx, machine.WorkspaceID, machine.ID, 1)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return fmt.Errorf("%w: disk usage has not been measured yet; start the machine before making its disk smaller", ErrInvalidOperation)
	}
	usedBytes := samples[0].DiskBytes
	allowedBytes := int64(diskGB) * 1024 * 1024 * 1024 * resizeDiskHeadroomPercent / 100
	if usedBytes > allowedBytes {
		return fmt.Errorf("%w: the workspace uses %.1f GB, more than %d%% of a %d GB disk; free space or choose a larger disk",
			ErrInvalidOperation, float64(usedBytes)/(1024*1024*1024), resizeDiskHeadroomPercent, diskGB)
	}
	return nil
}
