package repository

import (
	"context"
	"database/sql"
	"errors"

	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

// withdrawTaskMachineStart stops a cancelled task from waiting for its own
// machine to start. A spawn or start the manager has not picked up, and that
// made no attempt (a host capacity wait gives its attempt back), has touched
// no runtime: it is cancelled. A machine that never ran is then retired by the
// ordinary permanent deletion; one that ran before keeps its workspace and
// stays as it was. Machines the task did not create, machines another task or
// run uses, and starts already underway are left alone, and cancellation keeps
// waiting for them, as does a start the user requested. The caller holds the
// task lock and commits.
func withdrawTaskMachineStart(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.MachineID == nil || *task.MachineID != task.ReservedMachineID {
		return nil
	}
	// The same workspace lock as reservations and deletion, taken before the
	// machine row; it keeps another task from reserving the machine meanwhile.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, task.WorkspaceID); err != nil {
		return err
	}
	var machine domain.DevMachine
	err := tx.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE id=$1 AND workspace_id=$2 AND created_by_user_id=$3 AND delete_requested_at IS NULL FOR UPDATE`, task.ReservedMachineID, task.WorkspaceID, task.CreatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var shared bool
	if err = tx.GetContext(ctx, &shared, `SELECT EXISTS(SELECT 1 FROM agent_tasks WHERE machine_id=$1 AND id<>$2 AND state NOT IN ('completed','cancelled','failed','blocked','awaiting_review'))
 OR EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE machine_id=$1 AND status IN ('queued','starting','running','waiting_input'))`, machine.ID, task.ID); err != nil {
		return err
	}
	if shared {
		return nil
	}
	// The machine is locked, so the manager cannot lease these meanwhile.
	var operations []domain.DevMachineOperation
	if err = tx.SelectContext(ctx, &operations, `SELECT * FROM dev_machine_operations WHERE machine_id=$1 AND status IN ('pending','leased') FOR UPDATE`, machine.ID); err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	neverRan := machine.StartedAt == nil && machine.DockerNetworkName == nil && machine.WorkspaceVolumeName == nil &&
		(machine.Status == domain.DevMachineStatusQueued || machine.Status == domain.DevMachineStatusSpawning || machine.Status == domain.DevMachineStatusFailed)
	if !neverRan && machine.Status != domain.DevMachineStatusStopped && machine.Status != domain.DevMachineStatusPaused {
		return nil
	}
	// Only the task's own operations are withdrawn: the spawn that created the
	// machine for its reservation, and the starts the task queued. A start the
	// user requested keeps the cancellation waiting.
	ids := make([]uuid.UUID, 0, len(operations))
	for _, operation := range operations {
		owned := strings.HasPrefix(operation.IdempotencyKey, "task-start:"+task.ID.String()+":") ||
			(neverRan && operation.Action == domain.DevMachineOpSpawn && strings.HasPrefix(operation.IdempotencyKey, "spawn:"))
		if !owned || operation.Status != domain.DevMachineOpStatusPending || operation.Attempts != 0 || (operation.Action != domain.DevMachineOpSpawn && operation.Action != domain.DevMachineOpStart) {
			return nil
		}
		ids = append(ids, operation.ID)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='cancelled',completed_at=now(),updated_at=now() WHERE id=ANY($1) AND status='pending' AND attempts=0`, pq.Array(ids)); err != nil {
		return err
	}
	if !neverRan {
		_, err = tx.ExecContext(ctx, `UPDATE dev_machines SET desired_status=status WHERE id=$1`, machine.ID)
		return err
	}
	_, err = requestPermanentDelete(ctx, tx, task.WorkspaceID, machine.ID, nil)
	return err
}
