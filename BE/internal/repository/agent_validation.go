package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

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
	if task.State != "validating" || task.MachineID == nil || task.Kind == domain.AgentTaskKindReview {
		return nil, ErrAgentTaskConflict
	}
	var run domain.DevMachineAgentRun
	if err = tx.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND machine_id=$3 AND status='succeeded'`, task.RunID, task.ID, task.MachineID); err != nil {
		return nil, err
	}
	step := &domain.DevMachineAgentRunStep{}
	err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=0 AND step_type='validation'`, task.RunID)
	if err == nil {
		if err = failExhaustedStep(ctx, tx, *task, step, validationPhase, "Independent validation could not finish"); err != nil {
			return nil, err
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
	generation, err := lockTaskMachine(ctx, tx, *task)
	if err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, step, `INSERT INTO dev_machine_agent_run_steps(agent_run_id,sequence,step_type,name,status,command_argv,summary,completed_at,compose_file) VALUES($1,0,'validation','Independent validation',$2::dev_machine_agent_run_step_status,$3,$4,CASE WHEN $2::dev_machine_agent_run_step_status='skipped' THEN now() END,$5) RETURNING *`, task.RunID, status, task.ValidationCommand, summary, task.ComposeFile)
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

// lockTaskMachine matches native-run creation's lock order to exclude competing
// writers, then returns the generation a new task-step operation must fence.
func lockTaskMachine(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) (int64, error) {
	for _, id := range []uuid.UUID{task.WorkspaceID, *task.MachineID} {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, id); err != nil {
			return 0, err
		}
	}
	var generation int64
	if err := tx.GetContext(ctx, &generation, `SELECT generation FROM dev_machines WHERE id=$1 AND workspace_id=$2 AND status='running' AND desired_status='running' AND expires_at>now() FOR UPDATE`, task.MachineID, task.WorkspaceID); err != nil {
		return 0, err
	}
	var allowed bool
	if err := tx.GetContext(ctx, &allowed, `SELECT EXISTS(SELECT 1 FROM dev_machine_workspace_policies WHERE workspace_id=$1 AND enabled) AND NOT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE machine_id=$2 AND status IN ('queued','starting','running','waiting_input'))
 AND NOT EXISTS(SELECT 1 FROM dev_machine_operations WHERE machine_id=$2 AND action IN ('run_check','run_tool') AND status IN ('pending','leased'))`, task.WorkspaceID, task.MachineID); err != nil {
		return 0, err
	}
	if !allowed {
		return 0, ErrAgentTaskConflict
	}
	return generation, nil
}

// failExhaustedStep marks a step failed once its infrastructure operation has
// exhausted retries, so it is never reported as passed or indefinitely queued.
func failExhaustedStep(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, step *domain.DevMachineAgentRunStep, phase taskStepPhase, summary string) error {
	var failed bool
	var reason sql.NullString
	// A rejected Compose definition is the project's own configuration, so its
	// reason is safe to show; other infrastructure errors keep the generic text.
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE agent_run_id=$1 AND action=$2 AND status='failed'),
 (SELECT left(error_message,500) FROM dev_machine_operations WHERE agent_run_id=$1 AND action=$2 AND status='failed' AND error_code='compose_rejected' LIMIT 1)`, task.RunID, phase.action).Scan(&failed, &reason); err != nil {
		return err
	}
	if reason.Valid {
		summary = strings.TrimPrefix(reason.String, "compose_rejected: ")
	}
	if !failed || step.Status == domain.DevMachineAgentRunStepStatusFailed || step.Status == domain.DevMachineAgentRunStepStatusCancelled {
		return nil
	}
	if err := tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='failed',completed_at=now(),summary=$2 WHERE id=$1 RETURNING *`, step.ID, summary); err != nil {
		return err
	}
	return validationEvent(ctx, tx, task, step)
}

func validationEvent(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, step *domain.DevMachineAgentRunStep) error {
	if task.ConversationID == nil {
		return ErrAgentTaskUnauthorized
	}
	event := "task.tests_updated"
	if step.StepType == publicationPhase.stepType {
		event = "task.publication_updated"
	}
	_, err := conversationEvent(ctx, tx, *task.ConversationID, event, map[string]any{"task_id": task.ID, "run_id": step.AgentRunID, "step": step})
	return err
}

// taskStepPhase binds a post-native task state to the single run step and
// machine operation allowed to act for it.
type taskStepPhase struct {
	action   domain.DevMachineOperationAction
	state    string
	stepType string
	sequence int
}

var (
	validationPhase  = taskStepPhase{domain.DevMachineOpValidateAgent, "validating", "validation", 0}
	publicationPhase = taskStepPhase{domain.DevMachineOpPublishAgent, "publishing", "publication", 1}
)

// Validation has its own phase authority. Native output writers remain forbidden
// after native completion. Every write also fences the manager's operation lease.
// A manual check uses the same check writers under its own authority.
func (r *DevMachineRepository) validationTransaction(ctx context.Context, op domain.DevMachineOperation) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
	if op.Action == domain.DevMachineOpRunCheck {
		return r.manualCheckTransaction(ctx, op)
	}
	return r.taskStepTransaction(ctx, op, validationPhase)
}

func (r *DevMachineRepository) taskStepTransaction(ctx context.Context, op domain.DevMachineOperation, phase taskStepPhase) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
	if op.AgentRunID == nil || op.LeaseOwner == nil || op.Action != phase.action {
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
	if task.State != phase.state || task.RunID != run.ID || task.MachineID == nil || *task.MachineID != run.MachineID || task.CreatorID != *run.RequestedByUserID || op.WorkspaceID != run.WorkspaceID || op.MachineID != run.MachineID {
		return fail(ErrAgentTaskUnauthorized)
	}
	if err = fenceTaskStepOperation(ctx, tx, op, run); err != nil {
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
	if err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=$2 AND step_type=$3 FOR UPDATE`, run.ID, phase.sequence, phase.stepType); err != nil {
		return fail(err)
	}
	return tx, task, step, nil
}

// fenceTaskStepOperation requires the manager's live lease on the operation
// and the machine generation, running state and enabled policy it was queued for.
func fenceTaskStepOperation(ctx context.Context, tx *sqlx.Tx, op domain.DevMachineOperation, run domain.DevMachineAgentRun) error {
	var current domain.DevMachineOperation
	err := tx.GetContext(ctx, &current, `SELECT * FROM dev_machine_operations WHERE id=$1 AND action=$8 AND agent_run_id=$2 AND workspace_id=$3 AND machine_id=$4 AND generation=$5 AND status='leased' AND lease_owner=$6 AND attempts=$7 AND lease_expires_at>clock_timestamp() FOR SHARE`, op.ID, run.ID, run.WorkspaceID, run.MachineID, op.Generation, *op.LeaseOwner, op.Attempts, op.Action)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentTaskConflict
	}
	if err != nil {
		return err
	}
	var machineID uuid.UUID
	err = tx.GetContext(ctx, &machineID, `SELECT m.id FROM dev_machines m JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id WHERE m.id=$1 AND m.workspace_id=$2 AND m.generation=$3 AND m.status='running' AND m.desired_status='running' AND m.expires_at>now() AND p.enabled FOR SHARE OF m,p`, run.MachineID, run.WorkspaceID, op.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentTaskUnauthorized
	}
	return err
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
	summary := "Running independent validation"
	if step.StepType == manualCheckStepType {
		summary = "Running the check"
	}
	if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='running',started_at=COALESCE(started_at,now()),summary=$2 WHERE id=$1 RETURNING *`, step.ID, summary); err != nil {
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
	if step.StepType == manualCheckStepType {
		summary = map[bool]string{true: "The check passed", false: "The check failed"}[exit == 0]
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

// SaveValidationArtifacts stores report files while the check is still
// running under the manager's lease; replays keep the first copy.
func (r *DevMachineRepository) SaveValidationArtifacts(ctx context.Context, op domain.DevMachineOperation, artifacts []domain.ValidationArtifact) error {
	tx, _, step, err := r.validationTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusRunning {
		return ErrAgentTaskConflict
	}
	for _, artifact := range artifacts {
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_validation_artifacts(step_id,path,content_type,size,data) VALUES($1,$2,$3,$4,$5) ON CONFLICT (step_id,path) DO NOTHING`, step.ID, artifact.Path, artifact.ContentType, len(artifact.Data), artifact.Data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ValidationArtifacts lists an attempt's report files under current task
// permissions, without their content.
func (r *AgentTaskRepository) ValidationArtifacts(ctx context.Context, ws, id, run uuid.UUID) ([]domain.ValidationArtifact, error) {
	if _, err := r.Validation(ctx, ws, id, run); err != nil {
		return nil, err
	}
	artifacts := []domain.ValidationArtifact{}
	err := r.db.SelectContext(ctx, &artifacts, `SELECT a.id,a.step_id,a.path,a.content_type,a.size,a.created_at FROM agent_validation_artifacts a
 JOIN dev_machine_agent_run_steps s ON s.id=a.step_id WHERE s.agent_run_id=$1 ORDER BY a.path`, run)
	return artifacts, err
}

// ValidationArtifact returns one report file of an attempt the caller may read.
func (r *AgentTaskRepository) ValidationArtifact(ctx context.Context, ws, id, run, artifact uuid.UUID) (*domain.ValidationArtifact, error) {
	if _, err := r.Validation(ctx, ws, id, run); err != nil {
		return nil, err
	}
	result := &domain.ValidationArtifact{}
	err := r.db.GetContext(ctx, result, `SELECT a.* FROM agent_validation_artifacts a JOIN dev_machine_agent_run_steps s ON s.id=a.step_id WHERE a.id=$1 AND s.agent_run_id=$2`, artifact, run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	return result, err
}
