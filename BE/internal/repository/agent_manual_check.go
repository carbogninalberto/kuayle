package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// A manual check runs the requested command in a credential-free container, without an agent
// attempt and without changing the task's state. Its result is a manual_check
// step on the task's latest attempt.
const manualCheckStepType = "manual_check"

// manualCheckKeyPrefix starts a run_check operation's idempotency key; the
// rest is the ID of the step the operation fills.
const manualCheckKeyPrefix = "task-check:"

// manualCheckStates are the task states in which nothing else uses the
// checkout. Active states are refused as busy; the others have no checkout.
var manualCheckStates = []string{"awaiting_review", "blocked", "failed", "completed"}

var activeTaskStates = []string{"preparing", "running", "waiting_input", "validating", "publishing", "cancelling"}

var (
	// ErrMachineBusy means an agent attempt, publication or another
	// manual check is using the machine.
	ErrMachineBusy = errors.New("the agent is working on this machine; try again when it pauses")
	// ErrCheckUnavailable means the task has no attempt with a ready checkout
	// on a running machine to check.
	ErrCheckUnavailable = errors.New("the check needs the task's latest iteration and its checkout on a running machine")
	// ErrNoCheckCommand means no command was given.
	ErrNoCheckCommand = errors.New("provide a check command")
	// ErrCheckForbidden means the caller may not run a command of their own.
	ErrCheckForbidden = errors.New("only members who can edit development settings can run a custom check command")
)

// activeManualCheckSQL reports a queued or running manual check or custom
// tool run or assistant command on machine $1; each holds the machine like an
// agent attempt.
const activeManualCheckSQL = `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE machine_id=$1 AND action IN ('run_check','run_tool','run_command') AND status IN ('pending','leased'))`

// RunCheck queues a manual check on the task's latest attempt for its
// creator. The supplied command requires development-management permission.
func (r *AgentTaskRepository) RunCheck(ctx context.Context, ws, id uuid.UUID, command []string) (*domain.DevMachineAgentRunStep, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	if command != nil && (len(command) == 0 || domain.ValidateTestCommand(command) != nil) {
		return nil, ErrConversationInvalid
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	if command != nil {
		var role string
		if err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, actor); err != nil {
			return nil, err
		}
		if !domain.HasPermission(role, domain.PermDevMachineManage) {
			return nil, ErrCheckForbidden
		}
	}
	if command == nil {
		// The check is refused for what it would run on before it is refused
		// for the missing command.
		if _, err = queueManualCheck(ctx, tx, task, actor, nil, ""); err != nil {
			return nil, err
		}
		return nil, ErrNoCheckCommand
	}
	step, err := queueManualCheck(ctx, tx, task, actor, command, "Manual check")
	if err != nil {
		return nil, err
	}
	return step, tx.Commit()
}

// RunPipelineCheck runs a script check of the pipeline a task is pinned to
// in the task's worktree, through the manual check runner, for its creator
// (the tick's). The command comes from the pinned version, which only those
// who manage workflows write, so the creator needs no permission to run a
// command of their own. It returns the check's step.
func (r *AgentTaskRepository) RunPipelineCheck(ctx context.Context, root domain.AgentTask, name string, command []string) (*domain.DevMachineAgentRunStep, error) {
	if len(command) == 0 || domain.ValidateTestCommand(command) != nil {
		return nil, ErrConversationInvalid
	}
	tx, task, err := r.transaction(ctx, root.ID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if !task.Pipeline || task.WorkspaceID != root.WorkspaceID {
		return nil, ErrTaskRoomUnavailable
	}
	step, err := queueManualCheck(ctx, tx, task, task.CreatorID, command, name)
	if err != nil {
		return nil, err
	}
	return step, tx.Commit()
}

// PipelineCheckResult is a check run of a task's attempts: its status, its
// exit code once it ended, and the end of its output (at most limit bytes).
func (r *AgentTaskRepository) PipelineCheckResult(ctx context.Context, task domain.AgentTask, step uuid.UUID, limit int) (domain.DevMachineAgentRunStepStatus, *int, string, error) {
	var row struct {
		Status domain.DevMachineAgentRunStepStatus `db:"status"`
		Exit   *int                                `db:"exit_code"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT s.status,s.exit_code FROM dev_machine_agent_run_steps s JOIN dev_machine_agent_runs r ON r.id=s.agent_run_id
 WHERE s.id=$1 AND s.step_type=$2 AND r.task_id=$3 AND r.workspace_id=$4`, step, manualCheckStepType, task.ID, task.WorkspaceID)
	if err != nil {
		return "", nil, "", err
	}
	var chunks []string
	if err = r.db.SelectContext(ctx, &chunks, `SELECT content FROM dev_machine_log_chunks WHERE service_id=$1 AND workspace_id=$2 ORDER BY sequence DESC,id DESC LIMIT 40`, step, task.WorkspaceID); err != nil {
		return "", nil, "", err
	}
	slices.Reverse(chunks)
	output := strings.Join(chunks, "")
	if len(output) > limit {
		output = strings.ToValidUTF8(output[len(output)-limit:], "")
	}
	return row.Status, row.Exit, output, nil
}

// queueManualCheck queues a check of command (nil only checks that one can
// run) on the locked task's latest attempt, in its worktree, as actor.
func queueManualCheck(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, actor uuid.UUID, command []string, name string) (*domain.DevMachineAgentRunStep, error) {
	ws := task.WorkspaceID
	if slices.Contains(activeTaskStates, task.State) {
		return nil, ErrMachineBusy
	}
	if !slices.Contains(manualCheckStates, task.State) || task.Kind == domain.AgentTaskKindReview || task.MachineID == nil {
		return nil, ErrCheckUnavailable
	}
	// The redaction snapshot hides the attempt's credentials from the output.
	var ready bool
	if err := tx.GetContext(ctx, &ready, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND machine_id=$3 AND workspace_id=$4 AND encrypted_output_secrets IS NOT NULL)
 AND EXISTS(SELECT 1 FROM dev_machine_checkouts WHERE id=$5 AND machine_id=$3 AND workspace_id=$4 AND status='ready')`, task.RunID, task.ID, task.MachineID, ws, task.CheckoutID); err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrCheckUnavailable
	}
	generation, err := lockMachineForCheck(ctx, tx, task.WorkspaceID, *task.MachineID)
	if err != nil {
		return nil, err
	}
	if command == nil {
		return nil, nil
	}
	argv, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	compose := task.ComposeFile
	// Steps whose operation ended without a result (for example a machine
	// teardown) are failed, so none stays queued forever.
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_run_steps s SET status='failed',completed_at=now(),summary='The check could not finish'
 WHERE s.agent_run_id=$1 AND s.step_type=$2 AND s.status IN ('queued','running')
 AND NOT EXISTS(SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=$3 AND o.idempotency_key=$4||s.id::text AND o.status IN ('pending','leased'))`, task.RunID, manualCheckStepType, task.MachineID, manualCheckKeyPrefix); err != nil {
		return nil, err
	}
	step := &domain.DevMachineAgentRunStep{}
	if err = tx.GetContext(ctx, step, `INSERT INTO dev_machine_agent_run_steps(agent_run_id,sequence,step_type,name,status,command_argv,summary,compose_file)
 SELECT $1,GREATEST(99,COALESCE(MAX(sequence),0))+1,$2,$5,'queued',$3,'Waiting for the check to start',$4 FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 RETURNING *`, task.RunID, manualCheckStepType, argv, compose, name); err != nil {
		return nil, err
	}
	op := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: ws, MachineID: *task.MachineID, AgentRunID: &task.RunID, CheckoutID: &task.CheckoutID, AgentTaskID: &task.ID, Action: domain.DevMachineOpRunCheck, Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: manualCheckKeyPrefix + step.ID.String(), RequestedByUserID: &actor, MaxAttempts: 3}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return nil, err
	}
	return step, nil
}

// lockMachineForCheck takes native-run creation's locks, then refuses a
// machine that is not running, a disabled policy and any other agent work,
// check or custom tool run. Manual checks and custom tools share it.
func lockMachineForCheck(ctx context.Context, tx *sqlx.Tx, ws, machine uuid.UUID) (int64, error) {
	for _, id := range []uuid.UUID{ws, machine} {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, id); err != nil {
			return 0, err
		}
	}
	var generation int64
	err := tx.GetContext(ctx, &generation, `SELECT m.generation FROM dev_machines m JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id AND p.enabled
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.status='running' AND m.desired_status='running' FOR UPDATE OF m`, machine, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCheckUnavailable
	}
	if err != nil {
		return 0, err
	}
	var busy bool
	if err = tx.GetContext(ctx, &busy, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.machine_id=$1 AND (r.status IN ('queued','starting','running','waiting_input')
 OR EXISTS(SELECT 1 FROM agent_tasks t WHERE t.run_id=r.id AND t.state IN ('preparing','running','waiting_input','validating','publishing','cancelling'))))
 OR EXISTS(SELECT 1 FROM dev_machine_operations WHERE machine_id=$1 AND action IN ('validate_agent','publish_agent','run_check','run_tool','run_command') AND status IN ('pending','leased'))`, machine); err != nil {
		return 0, err
	}
	if busy {
		return 0, ErrMachineBusy
	}
	return generation, nil
}

func manualCheckStepID(op domain.DevMachineOperation) (uuid.UUID, bool) {
	raw, ok := strings.CutPrefix(op.IdempotencyKey, manualCheckKeyPrefix)
	if !ok || op.Action != domain.DevMachineOpRunCheck {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}

// manualCheckTransaction is a manual check's write authority: the requester
// still created the task and may use it, the task is still idle on the same
// attempt and machine, and the manager still holds the operation's lease.
func (r *DevMachineRepository) manualCheckTransaction(ctx context.Context, op domain.DevMachineOperation) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
	stepID, ok := manualCheckStepID(op)
	if !ok || op.AgentRunID == nil || op.LeaseOwner == nil || op.RequestedByUserID == nil {
		return nil, nil, nil, ErrAgentTaskUnauthorized
	}
	var run domain.DevMachineAgentRun
	if err := r.db.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1`, op.AgentRunID); err != nil {
		return nil, nil, nil, err
	}
	if run.TaskID == nil {
		return nil, nil, nil, ErrAgentTaskUnauthorized
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: *op.RequestedByUserID})
	tx, task, err := NewAgentTaskRepository(r.db).transaction(ctx, *run.TaskID, true)
	if err != nil {
		return nil, nil, nil, err
	}
	fail := func(e error) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
		tx.Rollback()
		return nil, nil, nil, e
	}
	if !slices.Contains(manualCheckStates, task.State) || task.RunID != run.ID || task.MachineID == nil || *task.MachineID != run.MachineID || op.WorkspaceID != run.WorkspaceID || op.MachineID != run.MachineID {
		return fail(ErrAgentTaskUnauthorized)
	}
	if err = fenceTaskStepOperation(ctx, tx, op, run); err != nil {
		return fail(err)
	}
	step := &domain.DevMachineAgentRunStep{}
	if err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE id=$1 AND agent_run_id=$2 AND step_type=$3 FOR UPDATE`, stepID, run.ID, manualCheckStepType); err != nil {
		return fail(err)
	}
	return tx, task, step, nil
}

// FailManualCheck records a manual check whose operation failed for good, so
// its step never stays queued or running.
func (r *DevMachineRepository) FailManualCheck(ctx context.Context, op domain.DevMachineOperation, summary string) error {
	stepID, ok := manualCheckStepID(op)
	if !ok || op.AgentTaskID == nil {
		return ErrAgentTaskUnauthorized
	}
	tx, task, err := NewAgentTaskRepository(r.db).transaction(ctx, *op.AgentTaskID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var failed bool
	if err = tx.GetContext(ctx, &failed, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE id=$1 AND machine_id=$2 AND action='run_check' AND agent_task_id=$3 AND status='failed')`, op.ID, op.MachineID, task.ID); err != nil {
		return err
	}
	if !failed {
		return ErrAgentTaskConflict
	}
	step := &domain.DevMachineAgentRunStep{}
	err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps s SET status='failed',completed_at=now(),summary=$4 FROM dev_machine_agent_runs r
 WHERE s.id=$1 AND s.step_type=$2 AND s.status IN ('queued','running') AND r.id=s.agent_run_id AND r.task_id=$3 RETURNING s.*`, stepID, manualCheckStepType, task.ID, summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = wakePipelineTick(ctx, tx, *task); err != nil {
		return err
	}
	if task.ConversationID != nil {
		if err = validationEvent(ctx, tx, *task, step); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ManualChecks lists the manual checks of one of the task's attempts, oldest
// first, under current task permissions.
func (r *AgentTaskRepository) ManualChecks(ctx context.Context, ws, id, run uuid.UUID) ([]domain.DevMachineAgentRunStep, error) {
	if err := r.authorizeRunRead(ctx, ws, id, run); err != nil {
		return nil, err
	}
	steps := []domain.DevMachineAgentRunStep{}
	err := r.db.SelectContext(ctx, &steps, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND step_type=$2 ORDER BY sequence`, run, manualCheckStepType)
	return steps, err
}
