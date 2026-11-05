package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// machineImageUpdateIdle is how long a machine must go without use through
// the gateway or the API before its containers are recreated.
const machineImageUpdateIdle = 15 * time.Minute

// machineBusyForImageUpdate reports whether recreating the machine's
// containers would interrupt work: another operation is queued or running, an
// agent run is unfinished, a task preview runs, or someone used the machine
// recently. except is the image update operation itself, once it is leased.
const machineBusyForImageUpdate = `SELECT
	EXISTS(SELECT 1 FROM dev_machine_operations WHERE machine_id=$1 AND id<>$2 AND status IN ('pending','leased'))
	OR EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE machine_id=$1
		AND status NOT IN ('succeeded','failed','cancelled','timeout'))
	OR EXISTS(SELECT 1 FROM agent_tasks WHERE machine_id=$1 AND preview_state IN ('starting','running'))
	OR EXISTS(SELECT 1 FROM dev_machines WHERE id=$1 AND last_activity_at > NOW() - make_interval(secs => $3))`

// EnqueueImageUpdate queues an update_images operation for a running machine
// that is idle. It returns whether the operation was queued; an operation with
// the same idempotency key is not queued twice.
func (r *DevMachineRepository) EnqueueImageUpdate(ctx context.Context, operation *domain.DevMachineOperation) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var machine domain.DevMachine
	err = tx.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, operation.WorkspaceID, operation.MachineID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if machine.Status != domain.DevMachineStatusRunning || machine.DesiredStatus != domain.DevMachineStatusRunning || machine.DeleteRequestedAt != nil {
		return false, nil
	}
	var busy bool
	if err := tx.GetContext(ctx, &busy, machineBusyForImageUpdate, machine.ID, uuid.Nil, machineImageUpdateIdle.Seconds()); err != nil {
		return false, err
	}
	if busy {
		return false, nil
	}
	operation.Action = domain.DevMachineOpUpdateImages
	operation.Status = domain.DevMachineOpStatusPending
	operation.Generation = machine.Generation
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return false, err
	}
	if operation.Status != domain.DevMachineOpStatusPending {
		// An earlier operation with this key already ran.
		return false, nil
	}
	return true, tx.Commit()
}

// MachineBusyForImageUpdate re-checks, when the update_images operation runs,
// that recreating the machine's containers interrupts no work.
func (r *DevMachineRepository) MachineBusyForImageUpdate(ctx context.Context, machineID, operationID uuid.UUID) (bool, error) {
	var busy bool
	err := r.db.GetContext(ctx, &busy, machineBusyForImageUpdate, machineID, operationID, machineImageUpdateIdle.Seconds())
	return busy, err
}

// ReplaceServiceImage moves a service to a new image. Its container is gone,
// so the service waits for the next provisioning to create one.
func (r *DevMachineRepository) ReplaceServiceImage(ctx context.Context, serviceID uuid.UUID, imageRef string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_services SET image_ref=$2, container_id=NULL,
		status='pending', health_status='unknown', health_message=NULL WHERE id=$1`, serviceID, imageRef)
	return err
}

// UpdateProviderImage records the image a machine's agent runs now use.
func (r *DevMachineRepository) UpdateProviderImage(ctx context.Context, providerRecordID uuid.UUID, imageRef string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_agent_providers SET image_ref=$2 WHERE id=$1 AND image_ref<>$2`, providerRecordID, imageRef)
	return err
}
