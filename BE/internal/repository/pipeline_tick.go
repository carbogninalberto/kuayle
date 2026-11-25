package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The tick of a pipeline task (KUA-252, service/team_tick.go) keeps its
// position in the task's workflow columns, like the graph runner: the phase
// in workflow_node, waiting in awaiting_review with workflow_wake_at, a
// person's decision fenced by workflow_sequence, the turns it charged in
// workflow_iterations and the time budget in the same clock. What else it
// remembers between passes is pipeline_state, which only the tick reads.

// startPipeline pins a pipeline version on a new task, whose first turn is
// the writer's: the iteration delegation creates. The writer is a member
// from the start; the other roles become members when first addressed.
func startPipeline(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, version domain.WorkflowVersion) error {
	pipeline, err := domain.ParsePipeline(version.Graph)
	if err != nil {
		return err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_node=$3,workflow_visits='{}',workflow_iterations=1,workflow_status='running',workflow_sequence=1,pipeline=true WHERE id=$1 RETURNING *`,
		task.ID, version.ID, pipeline.Phases[0].ID); err != nil {
		return err
	}
	return ensureWriterMember(ctx, tx, *task, *pipeline)
}

// PipelineApply is what one pass of a pipeline task's tick, or a person's
// decision on it, does to the task.
type PipelineApply struct {
	// Sequence is the workflow_sequence the pass or decision read; a person's
	// decision moves it on, so a pass that read an older one is refused.
	Sequence int
	// Phase is the current phase, and State the tick's memory.
	Phase string
	State json.RawMessage
	// Entry is what happens to the task: a wait (its tick waits for an
	// event or Wake, and NeedsPerson holds the time budget while a person is
	// awaited), the writer's turn (an iteration, reading the room up to
	// ReadSeq), a publication, a pause, a failure or completion.
	Entry   WorkflowEntry
	ReadSeq int64
	// Turns are the turns the pass started, charged to the budget.
	Turns int
	// ClaimedWake is the wake-up the worker claimed the task for: an event
	// that set another one while the pass ran (a webhook, a member's turn
	// ending) keeps the earlier of the two, so it is never lost.
	ClaimedWake *time.Time
	// ExtendBudget grants another budget of the same size (a person
	// continued past it).
	ExtendBudget bool
	Notes        []domain.NotebookNote
	// FailureClass is why the task fails, when it does.
	FailureClass domain.FailureClass
	// Actor is the person deciding; nil for the tick.
	Actor *uuid.UUID
}

// ApplyPipeline applies a pass of the tick to a claimed task, fenced by the
// worker lease, the task's advisory lock and its sequence.
func (r *AgentTaskRepository) ApplyPipeline(ctx context.Context, claimed domain.AgentTask, apply PipelineApply) (*domain.AgentTask, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return nil, err
	}
	if err = applyPipelineTx(ctx, tx, task, apply); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// DecidePipeline applies a person's decision on a pipeline task (an
// approval, or continuing or stopping where it paused) for its creator or an
// owner or admin who can see the issue, as DecideWorkflow does. decide reads
// the task as it is locked for the decision and says what the decision does.
func (r *AgentTaskRepository) DecidePipeline(ctx context.Context, ws, id uuid.UUID, decide func(domain.AgentTask) (*PipelineApply, error)) (*domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, task, err := r.decisionTransaction(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	apply, err := decide(*task)
	if err != nil {
		return nil, err
	}
	apply.Actor, apply.ClaimedWake = &actor, task.WorkflowWakeAt
	if err = applyPipelineTx(ctx, tx, task, *apply); err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "task_workflow", id, "decision", map[string]any{"sequence": apply.Sequence, "phase": apply.Phase}); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

func applyPipelineTx(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, apply PipelineApply) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "workflow-runner:"+task.ID.String()); err != nil {
		return err
	}
	if !task.Pipeline || task.WorkflowSequence != apply.Sequence || task.WorkflowPausedAt != nil || WorkflowEntryState(apply.Entry.Kind) == "" {
		return ErrAgentTaskConflict
	}
	switch task.State {
	case "running", "publishing", "awaiting_review", "blocked":
	default:
		return ErrAgentTaskConflict
	}
	now := time.Now()
	result := ComputeWorkflowApply(*task, WorkflowApply{Hold: true, Entry: apply.Entry, ExtendBudget: apply.ExtendBudget}, now)
	if apply.Entry.Kind == WorkflowEnterIteration || apply.Entry.Kind == WorkflowEnterPublish {
		result.Status = domain.WorkflowStatusRunning
	}
	if err := noteNotebook(ctx, tx, task.ID, apply.Actor, apply.Notes...); err != nil {
		return err
	}
	state := string(apply.State)
	if state == "" {
		state = "{}"
	}
	from, reason, held := task.State, task.Reason, task.WorkflowHeldSince != nil
	// A pass moves the sequence on only when what a person decides on
	// changes (the phase or the status), or a person decided.
	if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_sequence=workflow_sequence+CASE WHEN $2 IS DISTINCT FROM workflow_node OR $3 IS DISTINCT FROM workflow_status OR $12::boolean THEN 1 ELSE 0 END,
 workflow_node=$2,workflow_status=$3,pipeline_state=$4::jsonb,workflow_iterations=workflow_iterations+$5,workflow_budget_extensions=$6,workflow_budget_from=$7,workflow_held_since=$8,
 workflow_wake_at=CASE WHEN workflow_wake_at IS DISTINCT FROM $10::timestamptz THEN LEAST(workflow_wake_at,$9::timestamptz) ELSE $9::timestamptz END,updated_at=now() WHERE id=$1 AND workflow_sequence=$11 RETURNING *`,
		task.ID, apply.Phase, result.Status, state, apply.Turns, result.Cursor.Extensions, nullTime(result.Cursor.BudgetFrom), nullTime(result.Cursor.HeldSince),
		apply.Entry.WakeAt, apply.ClaimedWake, apply.Sequence, apply.Actor != nil); err != nil {
		return err
	}
	var err error
	switch kind := apply.Entry.Kind; {
	case kind == WorkflowEnterIteration:
		if err = workflowIteration(ctx, tx, task, apply.Entry, apply.Actor); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE task_members SET last_read_seq=GREATEST(last_read_seq,$2),turns=turns+1,updated_at=now() WHERE task_id=$1 AND member_task_id IS NULL`, task.ID, apply.ReadSeq)
	case kind == WorkflowEnterFail:
		err = beginAgentTaskFailure(ctx, tx, task, apply.FailureClass, apply.Entry.Reason)
	case task.State != WorkflowEntryState(kind) || reason != apply.Entry.Reason:
		err = transitionWorkflowTask(ctx, tx, task, WorkflowEntryState(kind), apply.Entry.Reason, apply.Actor)
	}
	if err != nil {
		return err
	}
	// A pass that changed nothing a person sees is not announced again; a
	// person newly awaited is.
	moved := task.WorkflowSequence != apply.Sequence
	if task.State == "awaiting_review" && (moved || (apply.Entry.NeedsPerson && !held)) {
		if err = markWorkflowReviewNotice(ctx, tx, *task, apply.Entry.NeedsPerson, apply.Actor); err != nil {
			return err
		}
	}
	if !moved && task.State == from {
		return nil
	}
	return workflowEvent(ctx, tx, *task)
}
