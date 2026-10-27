package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// EnsureValidation queues a single independent check for the accepted attempt.
// The task lease, command snapshot, native outcome and operation commit together.
func (r *AgentTaskRepository) EnsureValidation(ctx context.Context, claimed domain.AgentTask) (*domain.DevMachineAgentRunStep, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return nil, err
	}
	if task.State != "validating" || task.MachineID == nil {
		return nil, ErrAgentTaskConflict
	}
	var run domain.DevMachineAgentRun
	if err = tx.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND machine_id=$3 AND status='succeeded'`, task.RunID, task.ID, task.MachineID); err != nil {
		return nil, err
	}
	step := &domain.DevMachineAgentRunStep{}
	err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=0 AND step_type='validation'`, task.RunID)
	if err == nil {
		// An exhausted infrastructure operation is not a passed or indefinitely queued check.
		var failed bool
		if err = tx.GetContext(ctx, &failed, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE agent_run_id=$1 AND action='validate_agent' AND status='failed')`, task.RunID); err != nil {
			return nil, err
		}
		if failed && step.Status != domain.DevMachineAgentRunStepStatusFailed && step.Status != domain.DevMachineAgentRunStepStatusCancelled {
			if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='failed',completed_at=now(),summary='Independent validation could not finish' WHERE id=$1 RETURNING *`, step.ID); err != nil {
				return nil, err
			}
			if err = validationEvent(ctx, tx, *task, step); err != nil {
				return nil, err
			}
		}
		return step, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var command []string
	if json.Unmarshal(task.ValidationCommand, &command) != nil || command == nil || domain.ValidateTestCommand(command) != nil {
		return nil, ErrConversationInvalid
	}
	status, summary := "queued", "Waiting for independent validation"
	if len(command) == 0 {
		status, summary = "skipped", "No test command is configured; validation is unavailable"
	}
	// Match native-run creation's lock order to exclude competing writers.
	for _, id := range []uuid.UUID{task.WorkspaceID, *task.MachineID} {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, id); err != nil {
			return nil, err
		}
	}
	var generation int64
	if err = tx.GetContext(ctx, &generation, `SELECT generation FROM dev_machines WHERE id=$1 AND workspace_id=$2 AND status='running' AND desired_status='running' AND expires_at>now() FOR UPDATE`, task.MachineID, task.WorkspaceID); err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.GetContext(ctx, &allowed, `SELECT EXISTS(SELECT 1 FROM dev_machine_workspace_policies WHERE workspace_id=$1 AND enabled) AND NOT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE machine_id=$2 AND status IN ('queued','starting','running','waiting_input'))`, task.WorkspaceID, task.MachineID); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAgentTaskConflict
	}
	err = tx.GetContext(ctx, step, `INSERT INTO dev_machine_agent_run_steps(agent_run_id,sequence,step_type,name,status,command_argv,summary,completed_at) VALUES($1,0,'validation','Independent validation',$2::dev_machine_agent_run_step_status,$3,$4,CASE WHEN $2::dev_machine_agent_run_step_status='skipped' THEN now() END) RETURNING *`, task.RunID, status, task.ValidationCommand, summary)
	if err != nil {
		return nil, err
	}
	if len(command) > 0 {
		op := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, AgentRunID: &task.RunID, Action: domain.DevMachineOpValidateAgent, Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: "task-validation:" + task.RunID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 3}
		if err = enqueueOperation(ctx, tx, op); err != nil {
			return nil, err
		}
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return nil, err
	}
	return step, tx.Commit()
}

func validationEvent(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, step *domain.DevMachineAgentRunStep) error {
	if task.ConversationID == nil {
		return ErrAgentTaskUnauthorized
	}
	_, err := conversationEvent(ctx, tx, *task.ConversationID, "task.tests_updated", map[string]any{"task_id": task.ID, "run_id": step.AgentRunID, "step": step})
	return err
}

// Validation has its own phase authority. Native output writers remain forbidden
// after native completion. Every write also fences the manager's operation lease.
func (r *DevMachineRepository) validationTransaction(ctx context.Context, op domain.DevMachineOperation) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
	if op.AgentRunID == nil || op.LeaseOwner == nil || op.Action != domain.DevMachineOpValidateAgent {
		return nil, nil, nil, ErrAgentTaskUnauthorized
	}
	var run domain.DevMachineAgentRun
	if err := r.db.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1`, op.AgentRunID); err != nil {
		return nil, nil, nil, err
	}
	if run.TaskID == nil || run.RequestedByUserID == nil {
		return nil, nil, nil, ErrAgentTaskUnauthorized
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: *run.RequestedByUserID})
	tx, task, err := NewAgentTaskRepository(r.db).transaction(ctx, *run.TaskID, true)
	if err != nil {
		return nil, nil, nil, err
	}
	fail := func(e error) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
		tx.Rollback()
		return nil, nil, nil, e
	}
	if task.State != "validating" || task.RunID != run.ID || task.MachineID == nil || *task.MachineID != run.MachineID || task.CreatorID != *run.RequestedByUserID || op.WorkspaceID != run.WorkspaceID || op.MachineID != run.MachineID {
		return fail(ErrAgentTaskUnauthorized)
	}
	var current domain.DevMachineOperation
	err = tx.GetContext(ctx, &current, `SELECT * FROM dev_machine_operations WHERE id=$1 AND action='validate_agent' AND agent_run_id=$2 AND workspace_id=$3 AND machine_id=$4 AND generation=$5 AND status='leased' AND lease_owner=$6 AND attempts=$7 AND lease_expires_at>clock_timestamp() FOR SHARE`, op.ID, run.ID, run.WorkspaceID, run.MachineID, op.Generation, *op.LeaseOwner, op.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrAgentTaskConflict)
	}
	if err != nil {
		return fail(err)
	}
	var machineID uuid.UUID
	err = tx.GetContext(ctx, &machineID, `SELECT m.id FROM dev_machines m JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id WHERE m.id=$1 AND m.workspace_id=$2 AND m.generation=$3 AND m.status='running' AND m.desired_status='running' AND m.expires_at>now() AND p.enabled FOR SHARE OF m,p`, run.MachineID, run.WorkspaceID, op.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrAgentTaskUnauthorized)
	}
	if err != nil {
		return fail(err)
	}
	var succeeded bool
	if err = tx.GetContext(ctx, &succeeded, `SELECT status='succeeded' FROM dev_machine_agent_runs WHERE id=$1 FOR SHARE`, run.ID); err != nil {
		return fail(err)
	}
	if !succeeded {
		return fail(ErrAgentTaskUnauthorized)
	}
	step := &domain.DevMachineAgentRunStep{}
	if err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=0 AND step_type='validation' FOR UPDATE`, run.ID); err != nil {
		return fail(err)
	}
	return tx, task, step, nil
}
func (r *DevMachineRepository) GetValidation(ctx context.Context, op domain.DevMachineOperation) (*domain.DevMachineAgentRunStep, error) {
	tx, _, step, err := r.validationTransaction(ctx, op)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return step, nil
}
func (r *DevMachineRepository) StartValidation(ctx context.Context, op domain.DevMachineOperation, containerID string) error {
	tx, task, step, err := r.validationTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusQueued && step.Status != domain.DevMachineAgentRunStepStatusRunning {
		return ErrAgentTaskConflict
	}
	changed := step.Status == domain.DevMachineAgentRunStepStatusQueued
	if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='running',started_at=COALESCE(started_at,now()),summary='Running independent validation' WHERE id=$1 RETURNING *`, step.ID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE dev_machine_services SET container_id=$2,status='running',health_status='healthy',started_at=COALESCE(started_at,now()) WHERE id=$1 AND agent_run_id=$3 AND machine_id=$4 AND workspace_id=$5`, step.ID, containerID, step.AgentRunID, op.MachineID, op.WorkspaceID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrAgentTaskConflict
	}
	if changed {
		if err = validationEvent(ctx, tx, *task, step); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *DevMachineRepository) CompleteValidation(ctx context.Context, op domain.DevMachineOperation, exit int) error {
	tx, task, step, err := r.validationTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusRunning || step.StartedAt == nil {
		return ErrAgentTaskConflict
	}
	status, summary := "failed", "Independent validation failed"
	if exit == 0 {
		status, summary = "succeeded", "Independent validation passed"
	}
	if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status=$2,exit_code=$3,summary=$4,completed_at=now() WHERE id=$1 RETURNING *`, step.ID, status, exit, summary); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_services SET status='stopped',health_status='stopped',stopped_at=now() WHERE id=$1`, step.ID); err != nil {
		return err
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *DevMachineRepository) CreateValidationOutput(ctx context.Context, op domain.DevMachineOperation, chunk *domain.DevMachineLogChunk) error {
	tx, _, step, err := r.validationTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusRunning || chunk.AgentRunID == nil || *chunk.AgentRunID != step.AgentRunID || chunk.ServiceID == nil || *chunk.ServiceID != step.ID || chunk.WorkspaceID != op.WorkspaceID || chunk.MachineID != op.MachineID {
		return ErrAgentTaskUnauthorized
	}
	return persistAgentOutput(ctx, tx, chunk, nil)
}
func (r *AgentTaskRepository) Validation(ctx context.Context, ws, id, run uuid.UUID) (*domain.DevMachineAgentRunStep, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var owned bool
	if err = tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3)`, run, id, ws); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrConversationNotFound
	}
	step := &domain.DevMachineAgentRunStep{}
	err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=0 AND step_type='validation'`, run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return step, err
}
