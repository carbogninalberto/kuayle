package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrResizePending means the machine already has a size waiting to be applied.
var ErrResizePending = errors.New("a resize is already in progress")

// RequestResizeForUser records the requested size on the creator's machine and
// queues the resize operation in one transaction. The machine must be settled
// (running, paused or stopped, not changing state) with no lifecycle operation
// queued or running, so the manager applies the size to a known runtime.
func (r *DevMachineRepository) RequestResizeForUser(ctx context.Context, workspaceID, machineID, userID uuid.UUID, size domain.DevMachineResize, operation *domain.DevMachineOperation) (*domain.DevMachine, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, workspaceID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, machineID); err != nil {
		return nil, err
	}
	var machine domain.DevMachine
	err = tx.GetContext(ctx, &machine, `SELECT * FROM dev_machines
		WHERE workspace_id=$1 AND id=$2 AND created_by_user_id=$3 FOR UPDATE`, workspaceID, machineID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if machine.PendingCPUMillis != nil {
		return nil, ErrResizePending
	}
	if !domain.DevMachineResizable(&machine) {
		return nil, ErrMachineStateConflict
	}
	var busy bool
	if err := tx.GetContext(ctx, &busy, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations
		WHERE machine_id=$1 AND status IN ('pending','leased')
		AND action IN ('spawn','start','stop','pause','teardown','snapshot_environment','resize'))`, machineID); err != nil {
		return nil, err
	}
	if busy {
		return nil, ErrMachineStateConflict
	}
	if err := tx.GetContext(ctx, &machine, `UPDATE dev_machines
		SET pending_cpu_millis=$3, pending_memory_mb=$4, pending_disk_gb=$5, resize_error=NULL
		WHERE workspace_id=$1 AND id=$2 RETURNING *`,
		workspaceID, machineID, size.CPUMillis, size.MemoryMB, size.DiskGB); err != nil {
		return nil, err
	}
	operation.Generation = machine.Generation
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &machine, nil
}

// FinishResize ends the machine's pending resize. An applied resize becomes
// the machine's size (and its volumes' limits) and clears the recorded
// resource limit; a rejected one keeps the size and records why.
func (r *DevMachineRepository) FinishResize(ctx context.Context, machineID uuid.UUID, applied bool, failure *string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !applied {
		if _, err := tx.ExecContext(ctx, `UPDATE dev_machines
			SET pending_cpu_millis=NULL, pending_memory_mb=NULL, pending_disk_gb=NULL, resize_error=$2
			WHERE id=$1`, machineID, failure); err != nil {
			return err
		}
		return tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `UPDATE dev_machines
		SET cpu_millis=pending_cpu_millis, memory_mb=pending_memory_mb, disk_gb=pending_disk_gb,
			machine_size='custom', pending_cpu_millis=NULL, pending_memory_mb=NULL, pending_disk_gb=NULL,
			resize_error=NULL, resource_limit_kind=NULL, resource_limit_message=NULL, resource_limit_at=NULL
		WHERE id=$1 AND pending_cpu_millis IS NOT NULL`, machineID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_volumes v
		SET size_limit_bytes=m.disk_gb::bigint*1024*1024*1024
		FROM dev_machines m WHERE m.id=$1 AND v.machine_id=m.id AND v.deleted_at IS NULL`, machineID); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordResourceLimit keeps the latest resource limit a machine container hit.
func (r *DevMachineRepository) RecordResourceLimit(ctx context.Context, machineID uuid.UUID, kind, message string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machines
		SET resource_limit_kind=$2, resource_limit_message=$3, resource_limit_at=NOW() WHERE id=$1`, machineID, kind, message)
	return err
}
