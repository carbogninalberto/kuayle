package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Pausing a workflow. A paused task (workflow_paused_at) is left alone by the
// worker (Claim) and refuses every move (applyWorkflowTx), so nothing
// advances and no step starts until its creator resumes it. What a pause
// does depends on what the task is doing:
//
//   - a waiting step (CI, an approval, a merge, a reviewer) or a task that has
//     not started its attempt yet pauses at once, where it is;
//   - "pause now" while the agent works stops the attempt the way a usage
//     limit does: the runtime is cleaned up, which keeps the checkout, the
//     worktree and the session, and the task blocks until it is resumed;
//   - otherwise (Kuayle is running a check or a publication, or the pause is
//     "after this step") the request is recorded and takes effect when the
//     step has an outcome, before the workflow acts on it (ParkWorkflow).
//
// Resuming continues from the same step, as the same visit and iteration. An
// interrupted attempt continues as a new attempt on the native session of the
// one that was stopped. The time spent paused is added to the start of the
// time budget and to the step's own timeout, so neither counts it.

// What a pause request does (ComputeWorkflowPause).
const (
	WorkflowPauseTakesEffect = "paused"
	WorkflowPauseInterrupts  = "interrupt"
	WorkflowPauseRequested   = "requested"
)

const (
	workflowPausingReason = "Pausing: stopping the agent"
	workflowPausedReason  = "Paused"
	workflowResumedReason = "Resumed"
)

// ComputeWorkflowPause decides what a pause request does to the task. run is
// the status of the task's current attempt, empty when it has none yet. It is
// computed without I/O, so the runner's tests pause exactly as the database
// does.
func ComputeWorkflowPause(task domain.AgentTask, mode, run string) (string, error) {
	if mode != domain.WorkflowPauseNow && mode != domain.WorkflowPauseAfterStep {
		return "", ErrConversationInvalid
	}
	if !task.RunsWorkflow() || task.WorkflowPausedAt != nil || task.WorkflowStatus == nil {
		return "", ErrAgentTaskConflict
	}
	// A request is only ever turned into a pause now.
	if task.WorkflowPauseRequest != nil && (mode != domain.WorkflowPauseNow || *task.WorkflowPauseRequest == domain.WorkflowPauseNow) {
		return "", ErrAgentTaskConflict
	}
	if *task.WorkflowStatus != domain.WorkflowStatusRunning && *task.WorkflowStatus != domain.WorkflowStatusWaiting {
		return "", ErrAgentTaskConflict
	}
	switch task.State {
	case "queued", "awaiting_review":
		return WorkflowPauseTakesEffect, nil
	case "preparing", "running", "waiting_input":
		switch run {
		case "":
			if task.State == "preparing" {
				return WorkflowPauseTakesEffect, nil
			}
		case "queued", "starting", "running", "waiting_input":
			if mode == domain.WorkflowPauseNow {
				return WorkflowPauseInterrupts, nil
			}
		}
		// The attempt already ended, or the pause waits for it.
		return WorkflowPauseRequested, nil
	case "validating", "publishing":
		return WorkflowPauseRequested, nil
	}
	return "", ErrAgentTaskConflict
}

// WorkflowResume is what resuming does to a task.
type WorkflowResume struct {
	// Withdrawn: the pause had not taken effect yet; only its request ends.
	Withdrawn bool
	// Restart: the pause stopped the agent, so the step continues as a new
	// attempt. Otherwise the worker simply picks the task up again.
	Restart bool
	// Paused is how long the workflow was paused, and BudgetFrom where its
	// time budget starts now that this time is left out.
	Paused     time.Duration
	BudgetFrom *time.Time
}

// ComputeWorkflowResume decides what resuming the task does, without I/O.
func ComputeWorkflowResume(task domain.AgentTask, now time.Time) (WorkflowResume, error) {
	if !task.RunsWorkflow() {
		return WorkflowResume{}, ErrAgentTaskConflict
	}
	if task.WorkflowPausedAt == nil {
		if task.WorkflowPauseRequest == nil {
			return WorkflowResume{}, ErrAgentTaskConflict
		}
		return WorkflowResume{Withdrawn: true}, nil
	}
	switch task.State {
	case "cancelling", "completed", "failed", "cancelled":
		// The interrupted attempt is still being cleaned up, or the task ended.
		return WorkflowResume{}, ErrAgentTaskConflict
	}
	resume := WorkflowResume{Restart: task.State == "blocked", Paused: max(now.Sub(*task.WorkflowPausedAt), 0)}
	from := task.WorkflowBudgetFrom
	if from == nil {
		from = task.WorkflowStartedAt
	}
	if from != nil {
		at := from.Add(resume.Paused)
		resume.BudgetFrom = &at
	}
	return resume, nil
}

func workflowPauseAudit(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, actor *uuid.UUID, action, mode string) error {
	detail := map[string]any{"sequence": task.WorkflowSequence}
	if mode != "" {
		detail["mode"] = mode
	}
	return workflowAudit(ctx, tx, task.WorkspaceID, actor, "task_workflow", task.ID, action, detail)
}

// workflowPauseChanged tells open views that the task's pause changed.
func workflowPauseChanged(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.ConversationID != nil {
		if _, err := conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
			return err
		}
	}
	return workflowEvent(ctx, tx, task)
}

// PauseWorkflow pauses the task's workflow for its creator, the only person
// who may cancel it too. The worker's lease is dropped, so a step it is
// applying right now is refused and sees the pause when it claims again.
func (r *AgentTaskRepository) PauseWorkflow(ctx context.Context, ws, id uuid.UUID, mode string) (*domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var run string
	err = tx.GetContext(ctx, &run, `SELECT status::text FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2`, task.RunID, task.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	effect, err := ComputeWorkflowPause(*task, mode, run)
	if err != nil {
		return nil, err
	}
	const release = `lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),updated_at=now()`
	if effect == WorkflowPauseRequested {
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_pause_request=$2,`+release+` WHERE id=$1 RETURNING *`, id, mode); err != nil {
			return nil, err
		}
		if err = workflowPauseAudit(ctx, tx, *task, &actor, domain.WorkflowPauseActionRequested, mode); err != nil {
			return nil, err
		}
		if err = workflowPauseChanged(ctx, tx, *task); err != nil {
			return nil, err
		}
		return task, tx.Commit()
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_pause_request=NULL,workflow_paused_at=now(),`+release+` WHERE id=$1 RETURNING *`, id); err != nil {
		return nil, err
	}
	if err = workflowPauseAudit(ctx, tx, *task, &actor, domain.WorkflowPauseActionPaused, mode); err != nil {
		return nil, err
	}
	if effect == WorkflowPauseInterrupts {
		// Cleanup stops the attempt and then blocks the task (FinishCancellation).
		if err = transitionAgentTask(ctx, tx, task, "cancelling", workflowPausingReason, &actor); err != nil {
			return nil, err
		}
		if err = workflowEvent(ctx, tx, *task); err != nil {
			return nil, err
		}
	} else if err = workflowPauseChanged(ctx, tx, *task); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// ParkWorkflow makes a requested pause take effect: the worker calls it when
// the task's current step has an outcome, instead of acting on that outcome.
// The step stays open; after a resume the worker reports the outcome again.
func (r *AgentTaskRepository) ParkWorkflow(ctx context.Context, claimed domain.AgentTask) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if task.WorkflowPauseRequest == nil || task.WorkflowPausedAt != nil {
		return ErrAgentTaskConflict
	}
	mode := *task.WorkflowPauseRequest
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_pause_request=NULL,workflow_paused_at=now(),updated_at=now() WHERE id=$1 RETURNING *`, task.ID); err != nil {
		return err
	}
	if err = workflowPauseAudit(ctx, tx, *task, nil, domain.WorkflowPauseActionPaused, mode); err != nil {
		return err
	}
	if err = workflowPauseChanged(ctx, tx, *task); err != nil {
		return err
	}
	return tx.Commit()
}

// ResumeWorkflow resumes a workflow its creator paused, or withdraws a pause
// that has not taken effect yet.
func (r *AgentTaskRepository) ResumeWorkflow(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	resume, err := ComputeWorkflowResume(*task, time.Now())
	if err != nil {
		return nil, err
	}
	if resume.Withdrawn {
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_pause_request=NULL,updated_at=now() WHERE id=$1 RETURNING *`, id); err != nil {
			return nil, err
		}
		if err = workflowPauseAudit(ctx, tx, *task, &actor, domain.WorkflowPauseActionWithdrawn, ""); err != nil {
			return nil, err
		}
		if err = workflowPauseChanged(ctx, tx, *task); err != nil {
			return nil, err
		}
		return task, tx.Commit()
	}
	if err = endWorkflowPause(ctx, tx, task, resume, &actor); err != nil {
		return nil, err
	}
	if !resume.Restart {
		// The worker picks the task up where it was; a waiting step is
		// evaluated at once, since what it waits for may have happened.
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_wake_at=CASE WHEN state='awaiting_review' AND workflow_status='waiting' THEN now() ELSE workflow_wake_at END,
 failure_count=0,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),updated_at=now() WHERE id=$1 RETURNING *`, id); err != nil {
			return nil, err
		}
		if err = workflowPauseChanged(ctx, tx, *task); err != nil {
			return nil, err
		}
		return task, tx.Commit()
	}
	// The interrupted attempt must be gone before its successor starts.
	var pending bool
	if err = tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased') AND o.action NOT IN ('start_preview','stop_preview'))`, task.ID, task.CheckoutID); err != nil {
		return nil, err
	}
	if pending {
		return nil, ErrAgentTaskConflict
	}
	// Only an attempt that started has a session to continue; one stopped
	// before that starts the step's session.
	var started bool
	err = tx.GetContext(ctx, &started, `SELECT started_at IS NOT NULL FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2`, task.RunID, task.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,sign_in_required=false,
 lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),issue_status_id=i.status_id,issue_updated_at=i.updated_at,resume_native_session=$2
 FROM issues i WHERE t.id=$1 AND i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id RETURNING t.*`, id, started); err != nil {
		return nil, err
	}
	// The same step, visit and iteration: only its attempt is new.
	if _, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET generation=$3 WHERE task_id=$1 AND sequence=$2`, task.ID, task.WorkflowSequence, task.Generation); err != nil {
		return nil, err
	}
	if err = transitionAgentTask(ctx, tx, task, "queued", workflowResumedReason, &actor); err != nil {
		return nil, err
	}
	if err = workflowEvent(ctx, tx, *task); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// endWorkflowPause ends the task's pause in the caller's transaction: the
// paused time is left out of the time budget and of the open step's timeout,
// and the resume is audited.
func endWorkflowPause(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, resume WorkflowResume, actor *uuid.UUID) error {
	if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_paused_at=NULL,workflow_pause_request=NULL,workflow_budget_from=$2 WHERE id=$1 RETURNING *`, task.ID, resume.BudgetFrom); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET paused_seconds=paused_seconds+$3 WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL`, task.ID, task.WorkflowSequence, int(resume.Paused/time.Second)); err != nil {
		return err
	}
	return workflowPauseAudit(ctx, tx, *task, actor, domain.WorkflowPauseActionResumed, "")
}

// forgetWorkflowPause ends a pause, or drops a request for one, when the
// task's creator retries it instead of resuming it.
func forgetWorkflowPause(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, actor *uuid.UUID) error {
	if task.WorkflowPausedAt == nil && task.WorkflowPauseRequest == nil {
		return nil
	}
	if task.WorkflowPausedAt == nil {
		return tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_pause_request=NULL WHERE id=$1 RETURNING *`, task.ID)
	}
	resume, err := ComputeWorkflowResume(*task, time.Now())
	if err != nil {
		// Its interrupted attempt is still being cleaned up: nothing resumes.
		return tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_paused_at=NULL WHERE id=$1 RETURNING *`, task.ID)
	}
	return endWorkflowPause(ctx, tx, task, resume, actor)
}

// WorkflowPauses lists the pauses and resumes of the task's workflow, oldest
// first, under the task's authorization.
func (r *AgentTaskRepository) WorkflowPauses(ctx context.Context, ws, id uuid.UUID) ([]domain.WorkflowPauseEvent, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var rows []struct {
		Action  string          `db:"action"`
		ActorID *uuid.UUID      `db:"actor_id"`
		Detail  json.RawMessage `db:"detail"`
		At      time.Time       `db:"created_at"`
	}
	if err = tx.SelectContext(ctx, &rows, `SELECT action,actor_id,detail,created_at FROM workflow_audit_events
 WHERE workspace_id=$1 AND entity='task_workflow' AND entity_id=$2 AND action IN ($3,$4,$5,$6) ORDER BY id LIMIT 500`,
		ws, id, domain.WorkflowPauseActionRequested, domain.WorkflowPauseActionPaused, domain.WorkflowPauseActionResumed, domain.WorkflowPauseActionWithdrawn); err != nil {
		return nil, err
	}
	events := make([]domain.WorkflowPauseEvent, 0, len(rows))
	for _, row := range rows {
		var detail struct {
			Sequence int    `json:"sequence"`
			Mode     string `json:"mode"`
		}
		_ = json.Unmarshal(row.Detail, &detail)
		events = append(events, domain.WorkflowPauseEvent{Action: row.Action, Sequence: detail.Sequence, Mode: detail.Mode, ActorID: row.ActorID, At: row.At})
	}
	return events, nil
}
