package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// lockTaskMachine matches native-run creation's lock order to exclude competing
// writers, then returns the generation a new task-step operation must fence.
func lockTaskMachine(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) (int64, error) {
	for _, id := range []uuid.UUID{task.WorkspaceID, *task.MachineID} {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, id); err != nil {
			return 0, err
		}
	}
	var generation int64
	if err := tx.GetContext(ctx, &generation, `SELECT generation FROM dev_machines WHERE id=$1 AND workspace_id=$2 AND status='running' AND desired_status='running' FOR UPDATE`, task.MachineID, task.WorkspaceID); err != nil {
		return 0, err
	}
	var allowed bool
	// Other tasks' attempts may run beside it on a shared machine, each in its
	// own worktree; the user's own agent session, a check or a tool run, or a
	// still active attempt of this task may not.
	if err := tx.GetContext(ctx, &allowed, `SELECT EXISTS(SELECT 1 FROM dev_machine_workspace_policies WHERE workspace_id=$1 AND enabled) AND NOT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE machine_id=$2 AND (task_id IS NULL OR task_id=$3) AND status IN ('queued','starting','running','waiting_input'))
 AND NOT EXISTS(SELECT 1 FROM dev_machine_operations WHERE machine_id=$2 AND action IN ('run_check','run_tool','run_command') AND status IN ('pending','leased'))`, task.WorkspaceID, task.MachineID, task.ID); err != nil {
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
	// Only known, user-facing failures may supply a step summary.
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE agent_run_id=$1 AND action=$2 AND status='failed'),
 (SELECT CASE WHEN error_code='compose_rejected' THEN left(error_message,500) ELSE error_message END
  FROM dev_machine_operations WHERE agent_run_id=$1 AND action=$2 AND status='failed'
  AND (error_code='compose_rejected' OR (action='publish_agent' AND error_code IN ($3,$4))) LIMIT 1)`, task.RunID, phase.action, domain.PublicationCheckoutBlockedCode, domain.PublicationWorkflowsPermissionCode).Scan(&failed, &reason); err != nil {
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
	publicationPhase = taskStepPhase{domain.DevMachineOpPublishAgent, "publishing", "publication", 1}
)

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
	err = tx.GetContext(ctx, &machineID, `SELECT m.id FROM dev_machines m JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id WHERE m.id=$1 AND m.workspace_id=$2 AND m.generation=$3 AND m.status='running' AND m.desired_status='running' AND p.enabled FOR SHARE OF m,p`, run.MachineID, run.WorkspaceID, op.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentTaskUnauthorized
	}
	return err
}

func (r *DevMachineRepository) GetManualCheck(ctx context.Context, op domain.DevMachineOperation) (*domain.DevMachineAgentRunStep, error) {
	tx, _, step, err := r.manualCheckTransaction(ctx, op)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return step, nil
}
func (r *DevMachineRepository) StartManualCheck(ctx context.Context, op domain.DevMachineOperation, containerID string) error {
	tx, task, step, err := r.manualCheckTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusQueued && step.Status != domain.DevMachineAgentRunStepStatusRunning {
		return ErrAgentTaskConflict
	}
	changed := step.Status == domain.DevMachineAgentRunStepStatusQueued
	summary := "Running the check"
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

func (r *DevMachineRepository) CompleteManualCheck(ctx context.Context, op domain.DevMachineOperation, exit int) error {
	tx, task, step, err := r.manualCheckTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusRunning || step.StartedAt == nil {
		return ErrAgentTaskConflict
	}
	status, summary := "failed", "The check failed"
	if exit == 0 {
		status, summary = "succeeded", "The check passed"
	}
	if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status=$2,exit_code=$3,summary=$4,completed_at=now() WHERE id=$1 RETURNING *`, step.ID, status, exit, summary); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_services SET status='stopped',health_status='stopped',stopped_at=now() WHERE id=$1`, step.ID); err != nil {
		return err
	}
	if err = wakePipelineTick(ctx, tx, *task); err != nil {
		return err
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return err
	}
	return tx.Commit()
}

// wakePipelineTick asks the tick of a pipeline task waiting for a check of
// its own to read the result.
func wakePipelineTick(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	_, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND pipeline AND state='awaiting_review'`, task.ID)
	return err
}
func (r *DevMachineRepository) CreateManualCheckOutput(ctx context.Context, op domain.DevMachineOperation, chunk *domain.DevMachineLogChunk) error {
	tx, _, step, err := r.manualCheckTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusRunning || chunk.AgentRunID == nil || *chunk.AgentRunID != step.AgentRunID || chunk.ServiceID == nil || *chunk.ServiceID != step.ID || chunk.WorkspaceID != op.WorkspaceID || chunk.MachineID != op.MachineID {
		return ErrAgentTaskUnauthorized
	}
	return persistAgentOutput(ctx, tx, chunk, nil)
}
func (r *AgentTaskRepository) authorizeRunRead(ctx context.Context, ws, id, run uuid.UUID) error {
	if _, err := conversationActor(ctx, ws); err != nil {
		return err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owned bool
	if err = tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3)`, run, id, ws); err != nil {
		return err
	}
	if !owned {
		return ErrConversationNotFound
	}
	return nil
}

// SaveValidationArtifacts stores report files while the check is still
// running under the manager's lease; replays keep the first copy.
func (r *DevMachineRepository) SaveValidationArtifacts(ctx context.Context, op domain.DevMachineOperation, artifacts []domain.ValidationArtifact) error {
	tx, _, step, err := r.manualCheckTransaction(ctx, op)
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
	if err := r.authorizeRunRead(ctx, ws, id, run); err != nil {
		return nil, err
	}
	artifacts := []domain.ValidationArtifact{}
	err := r.db.SelectContext(ctx, &artifacts, `SELECT a.id,a.step_id,a.path,a.content_type,a.size,a.created_at FROM agent_validation_artifacts a
 JOIN dev_machine_agent_run_steps s ON s.id=a.step_id WHERE s.agent_run_id=$1 ORDER BY a.path`, run)
	return artifacts, err
}

// ValidationArtifact returns one report file of an attempt the caller may read.
func (r *AgentTaskRepository) ValidationArtifact(ctx context.Context, ws, id, run, artifact uuid.UUID) (*domain.ValidationArtifact, error) {
	if err := r.authorizeRunRead(ctx, ws, id, run); err != nil {
		return nil, err
	}
	result := &domain.ValidationArtifact{}
	err := r.db.GetContext(ctx, result, `SELECT a.* FROM agent_validation_artifacts a JOIN dev_machine_agent_run_steps s ON s.id=a.step_id WHERE a.id=$1 AND s.agent_run_id=$2`, artifact, run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	return result, err
}
