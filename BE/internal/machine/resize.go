package machine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// resize applies a machine's pending size. A running or paused machine holds
// host resources, so its new size is admitted like a start and then applied
// to every container live; a stopped machine's containers take the new
// limits when they start (start admits them). The workspace disk limit is
// enforced by monitoring, which reads the machine's size after it is stored.
// The size is applied whatever the operation generation: lifecycle changes
// queued after the request keep the containers that get the new limits.
func (m *Manager) resize(ctx context.Context, machine *domain.DevMachine) error {
	if machine.PendingCPUMillis == nil || machine.PendingMemoryMB == nil || machine.PendingDiskGB == nil {
		return nil
	}
	target := *machine
	target.CPUMillis, target.MemoryMB, target.DiskGB = *machine.PendingCPUMillis, *machine.PendingMemoryMB, *machine.PendingDiskGB
	switch machine.Status {
	case domain.DevMachineStatusRunning, domain.DevMachineStatusPaused:
		release, err := m.admit(ctx, &target)
		if err != nil {
			var wait *capacityWaitError
			if errors.As(err, &wait) {
				return &terminalOperationError{code: "host_capacity", message: "The host does not have room for the new size (" +
					strings.TrimPrefix(wait.message, "waiting for ") + "); choose a smaller size or stop another machine"}
			}
			return err
		}
		defer release()
	case domain.DevMachineStatusStopped:
	default:
		return fmt.Errorf("machine_busy: the machine is %s; the resize is retried once it settles", machine.Status)
	}
	if err := m.runtime.UpdateResources(ctx, &target); err != nil {
		if restoreErr := m.runtime.UpdateResources(ctx, machine); restoreErr != nil {
			log.WithField("machine_id", machine.ID).WithError(restoreErr).Warn("restoring container limits after a failed resize failed")
		}
		return fmt.Errorf("resize_failed: %w", err)
	}
	if err := m.store.FinishResize(ctx, machine.ID, true, nil); err != nil {
		return err
	}
	return m.createEvent(ctx, machine, nil, "lifecycle", "machine.resized", map[string]any{
		"from": map[string]int{"cpu_millis": machine.CPUMillis, "memory_mb": machine.MemoryMB, "disk_gb": machine.DiskGB},
		"to":   map[string]int{"cpu_millis": target.CPUMillis, "memory_mb": target.MemoryMB, "disk_gb": target.DiskGB},
	})
}

// resizeFailureMessage is the user-facing reason a resize was not applied.
func resizeFailureMessage(err error) string {
	var terminalErr *terminalOperationError
	if errors.As(err, &terminalErr) {
		return terminalErr.message
	}
	return "The new size could not be applied: " + safeError(err)
}
