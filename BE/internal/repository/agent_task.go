package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrAgentTaskUnauthorized = errors.New("agent task execution is no longer authorized")

var ErrAgentTaskConflict = errors.New("agent task changed or execution lease expired")

type AgentTaskRepository struct{ db *sqlx.DB }

func NewAgentTaskRepository(db *sqlx.DB) *AgentTaskRepository { return &AgentTaskRepository{db} }

func (r *AgentTaskRepository) Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.RequestID == uuid.Nil || len(input.Prompt) == 0 || len(input.Prompt) > 32768 || !utf8.ValidString(input.Prompt) || len(input.Provider) > 64 || input.Provider == "" || len(input.Model) > 128 || len(input.Thinking) > 32 {
		return nil, ErrConversationInvalid
	}
	tx, c, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if c.IssueID == nil || c.TeamID == nil {
		return nil, ErrConversationInvalid
	}
	// Serialize a caller's request ID across distinct conversations too.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-task-request:"+ws.String()+":"+c.CreatorID.String()+":"+input.RequestID.String()); err != nil {
		return nil, err
	}
	if existing, err := replayAgentTaskCreate(ctx, tx, c, input); err != nil || existing != nil {
		return existing, err
	}
	requested, command, err := resolveAgentTaskCommand(ctx, tx, ws, *c.IssueID, input.TestCommand)
	if err != nil {
		return nil, err
	}
	var task domain.AgentTask
	instructions, err := resolveScopeDefault(ctx, tx, ws, *c.IssueID, "agent_instructions")
	if err != nil {
		return nil, err
	}
	compose, err := resolveScopeDefault(ctx, tx, ws, *c.IssueID, "compose_file")
	if err != nil {
		return nil, err
	}
	preview, previewPort, err := resolvePreview(ctx, tx, ws, *c.IssueID)
	if err != nil {
		return nil, err
	}
	var agentProfile *uuid.UUID
	if input.AgentProfile != nil {
		if input.AgentProfile.WorkspaceID != ws || input.AgentProfile.ArchivedAt != nil || input.Workflow != nil {
			return nil, ErrConversationInvalid
		}
		agentProfile = &input.AgentProfile.ID
	}
	if (input.Workflow != nil || input.AgentProfile != nil) && input.ModelServerID == nil {
		// The first agent step runs on the default model server like every
		// later step (defaultModelServer).
		if input.ModelServerID, err = defaultModelServer(ctx, tx, ws, input.Provider); err != nil {
			return nil, err
		}
	}
	err = tx.GetContext(ctx, &task, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,model,thinking,reason,issue_status_id,issue_updated_at,test_command,validation_command,agent_instructions,compose_file,preview_command,preview_port,model_server_id,agent_profile_id) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'Waiting for execution',i.status_id,i.updated_at,$11,$12,$13,$14,$15,$16,$17,$18 FROM issues i WHERE i.id=$4 AND i.workspace_id=$1 AND i.team_id=$5 RETURNING *`, ws, conversation, c.CreatorID, c.IssueID, c.TeamID, input.RequestID, input.Prompt, input.Provider, input.Model, input.Thinking, requested, command, instructions, compose, preview, previewPort, input.ModelServerID, agentProfile)
	if err != nil {
		return nil, err
	}
	// The profile's instructions (of the first workflow step, or of the agent
	// identity the task runs as) join the inherited instructions.
	if (input.Workflow != nil || input.AgentProfile != nil) && input.WorkflowInstructions != "" {
		if err = tx.GetContext(ctx, &task, `UPDATE agent_tasks SET agent_instructions=$2 WHERE id=$1 RETURNING *`, task.ID, joinInstructions(instructions, input.WorkflowInstructions)); err != nil {
			return nil, err
		}
	}
	if input.Workflow != nil {
		// The version is pinned before the queued snapshot is recorded, so a
		// replay returns it.
		if input.Workflow.WorkspaceID != ws {
			return nil, ErrConversationInvalid
		}
		if err = startWorkflow(ctx, tx, &task, *input.Workflow); err != nil {
			return nil, err
		}
	}
	// Name what the new task waits for; the worker starts it once unblocked.
	if reason, err := waitingReason(ctx, tx, task.IssueID); err != nil {
		return nil, err
	} else if reason != "" {
		if err = tx.GetContext(ctx, &task, `UPDATE agent_tasks SET reason=$2 WHERE id=$1 RETURNING *`, task.ID, reason); err != nil {
			return nil, err
		}
	}
	if err = recordAgentTaskTransition(ctx, tx, task, "", &c.CreatorID); err != nil {
		return nil, err
	}
	return &task, tx.Commit()
}

// Always acquire the privacy graph and conversation locks before the task row.
// Claim only locks a task row, and never waits for a conversation while holding it.
func lockAgentTask(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, authorize bool) (*domain.AgentTask, error) {
	var task domain.AgentTask
	if err := tx.GetContext(ctx, &task, `SELECT * FROM agent_tasks WHERE id=$1`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	if authorize {
		a, ok := domain.AccessFromContext(ctx)
		if !ok || a.UserID != task.CreatorID || a.WorkspaceID != task.WorkspaceID {
			return nil, ErrConversationNotFound
		}
	}
	if task.ConversationID != nil {
		var c *domain.Conversation
		var err error
		if authorize {
			c, err = lockConversation(ctx, tx, task.WorkspaceID, *task.ConversationID, task.CreatorID, false)
		} else {
			// Cleanup must remain possible after an issue moves to another team.
			// Pin the live issue without authorizing its content, then its chat,
			// preserving the same issue -> conversation -> task lock order.
			var pinned []uuid.UUID
			err = tx.SelectContext(ctx, &pinned, `SELECT id FROM issues WHERE id=$1 AND workspace_id=$2 FOR SHARE`, task.IssueID, task.WorkspaceID)
			if err == nil {
				c = &domain.Conversation{}
				err = tx.GetContext(ctx, c, `SELECT * FROM conversations WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, task.ConversationID, task.WorkspaceID)
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		if err != nil {
			return nil, err
		}
		if authorize {
			if err = conversationMember(ctx, tx, task.WorkspaceID, task.CreatorID); err != nil {
				return nil, err
			}
			if err = conversationScope(ctx, tx, *c); err != nil {
				return nil, err
			}
			if c.IssueID == nil || *c.IssueID != task.IssueID || c.TeamID == nil || *c.TeamID != task.TeamID {
				return nil, ErrConversationNotFound
			}
		}
	} else if authorize {
		return nil, ErrConversationNotFound
	}
	if err := tx.GetContext(ctx, &task, `SELECT * FROM agent_tasks WHERE id=$1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	return &task, nil
}
func (r *AgentTaskRepository) transaction(ctx context.Context, id uuid.UUID, authorize bool) (*sqlx.Tx, *domain.AgentTask, error) {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, nil, err
	}
	task, err := lockAgentTask(ctx, tx, id, authorize)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return tx, task, nil
}
func (r *AgentTaskRepository) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return task, nil
}
func (r *AgentTaskRepository) List(ctx context.Context, ws, conversation uuid.UUID) ([]domain.AgentTask, error) {
	tx, _, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tasks := []domain.AgentTask{}
	err = tx.SelectContext(ctx, &tasks, `SELECT * FROM agent_tasks WHERE conversation_id=$1 AND kind='implement' ORDER BY created_at DESC,id LIMIT 100`, conversation)
	return tasks, err
}
func (r *AgentTaskRepository) Transitions(ctx context.Context, ws, id uuid.UUID) ([]domain.AgentTaskTransition, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	entries := []domain.AgentTaskTransition{}
	err = tx.SelectContext(ctx, &entries, `SELECT sequence,task_id,from_state,to_state,actor_id,reason,created_at FROM agent_task_transitions WHERE task_id=$1 ORDER BY sequence`, id)
	return entries, err
}
func (r *AgentTaskRepository) Claim(ctx context.Context) (*domain.AgentTask, error) {
	var task domain.AgentTask
	// A queued task whose issue has unfinished blockers waits for them.
	err := r.db.GetContext(ctx, &task, `WITH candidate AS (
 SELECT id FROM agent_tasks WHERE ((state NOT IN ('completed','cancelled','failed','blocked','awaiting_review') AND next_poll_at<=now())
  -- A workflow step waiting for a review, CI, a person or a timeout is
  -- evaluated when an event or its timeout wakes it.
  OR (state='awaiting_review' AND workflow_wake_at<=now() AND next_poll_at<=now()))
 AND (lease_expires_at IS NULL OR lease_expires_at<=now())
 -- A workflow a person paused is left alone until it is resumed; only the
 -- cleanup of an attempt the pause stopped (or a cancellation) goes on.
 AND (workflow_paused_at IS NULL OR state='cancelling')
 AND NOT (state='queued' AND kind='implement' AND `+openBlockersExist("agent_tasks.issue_id")+`)
 -- The creator's agent subscription is at its usage limit until a reset.
 AND NOT `+usageLimitHeld+`
 ORDER BY LEAST(next_poll_at,COALESCE(workflow_wake_at,next_poll_at)),created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE agent_tasks t SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds'
 FROM candidate c WHERE t.id=c.id RETURNING t.*`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &task, err
}
func fenceAgentTask(ctx context.Context, tx *sqlx.Tx, current, claimed domain.AgentTask) error {
	if current.Generation != claimed.Generation || current.LeaseToken == nil || claimed.LeaseToken == nil || *current.LeaseToken != *claimed.LeaseToken {
		return ErrAgentTaskConflict
	}
	var valid bool
	if err := tx.GetContext(ctx, &valid, `SELECT lease_expires_at>clock_timestamp() FROM agent_tasks WHERE id=$1`, current.ID); err != nil {
		return err
	}
	if !valid {
		return ErrAgentTaskConflict
	}
	return nil
}
func recordAgentTaskTransition(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, from string, actor *uuid.UUID) error {
	// The creator is notified of a transition marked pending (ClaimTaskNotices).
	// A task that waits for its own agent review needs nobody yet. A workflow
	// entering awaiting_review overrides the mark: markWorkflowReviewNotice.
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_task_transitions(task_id,from_state,to_state,actor_id,reason,notification_pending)
 VALUES($1,$2,$3,$4,$5,$6::boolean AND NOT ($7::boolean AND EXISTS(SELECT 1 FROM agent_tasks r WHERE r.review_of_task_id=$1 AND r.state NOT IN ('completed','failed','cancelled'))))`,
		task.ID, from, task.State, actor, task.Reason, taskTransitionNotifies(task, actor), task.State == "awaiting_review")
	if err != nil {
		return err
	}
	// Before the state event, so its payload carries the preview state.
	if err = taskPreviewTransition(ctx, tx, &task); err != nil {
		return err
	}
	if err = syncAgentSession(ctx, tx, task); err != nil {
		return err
	}
	if err = reviewUpdated(ctx, tx, task); err != nil {
		return err
	}
	// Workflows: a finished review wakes the step waiting for it, and a task
	// that finished outside the runner closes its workflow.
	if err = wakeWorkflowParent(ctx, tx, task); err != nil {
		return err
	}
	if err = closeWorkflowOnFinish(ctx, tx, task); err != nil {
		return err
	}
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.state_changed", task); err != nil {
			return err
		}
		if err = notifyTaskTelegram(ctx, tx, task); err != nil {
			return err
		}
	}
	return purgeTaskSession(ctx, tx, task)
}

// taskPreviewTransition stops a running preview when its task finishes. It
// never starts one: the preview command runs files the agent just wrote, in
// the developer container with its developer secrets, agent sign-ins and
// optional sudo, so a start needs the creator's own request (RequestPreview).
func taskPreviewTransition(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask) error {
	switch task.State {
	case "completed", "cancelled", "failed":
	default:
		return nil
	}
	if task.PreviewState == nil || (*task.PreviewState != domain.AgentTaskPreviewStarting && *task.PreviewState != domain.AgentTaskPreviewRunning) {
		return nil
	}
	if err := queueStopPreview(ctx, tx, task.ID, nil); err != nil {
		return err
	}
	return tx.GetContext(ctx, task, `SELECT * FROM agent_tasks WHERE id=$1`, task.ID)
}

// purgeTaskSession queues removal of a finished task's native harness
// transcripts. Failed tasks keep them, since an explicit retry may resume.
func purgeTaskSession(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if (task.State != "completed" && task.State != "cancelled") || task.MachineID == nil {
		return nil
	}
	// Sessions exist only after an attempt ran in the task's checkout; a task
	// cancelled before its checkout was created has nothing to purge.
	var generation int64
	err := tx.GetContext(ctx, &generation, `SELECT m.generation FROM dev_machines m JOIN dev_machine_checkouts c ON c.machine_id=m.id AND c.id=$3
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.status<>'destroyed'`, task.MachineID, task.WorkspaceID, task.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return enqueueOperation(ctx, tx, &domain.DevMachineOperation{
		WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, CheckoutID: &task.CheckoutID,
		Action: domain.DevMachineOpPurgeTaskSession, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: "task-session-purge:" + task.ID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 5,
	})
}
func transitionAgentTask(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, to, reason string, actor *uuid.UUID) error {
	if !domain.ValidAgentTaskKindTransition(task.Kind, task.State, to) || len(reason) > 2048 || strings.TrimSpace(reason) == "" {
		return ErrAgentTaskConflict
	}
	from := task.State
	if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET state=$2,reason=$3,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, to, reason); err != nil {
		return err
	}
	return recordAgentTaskTransition(ctx, tx, *task, from, actor)
}
func (r *AgentTaskRepository) Transition(ctx context.Context, claimed domain.AgentTask, to, reason string) error {
	if to == "cancelled" {
		return r.FinishCancellation(ctx, claimed, reason)
	}
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if to == "awaiting_review" {
		var published bool
		if err = tx.GetContext(ctx, &published, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_run_steps s JOIN dev_machine_operations o ON o.agent_run_id=s.agent_run_id AND o.action='publish_agent'
         WHERE s.agent_run_id=$1 AND s.sequence=1 AND s.step_type='publication' AND s.status='succeeded' AND s.completed_at IS NOT NULL AND o.status='completed')`, task.RunID); err != nil {
			return err
		}
		if !published {
			return ErrAgentTaskConflict
		}
	}
	if to == "publishing" {
		var passed bool
		if err = tx.GetContext(ctx, &passed, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_run_steps s JOIN dev_machine_operations o ON o.agent_run_id=s.agent_run_id AND o.action='validate_agent'
         WHERE s.agent_run_id=$1 AND s.sequence=0 AND s.step_type='validation' AND s.status='succeeded' AND s.exit_code=0 AND s.started_at IS NOT NULL AND s.completed_at IS NOT NULL AND o.status='completed')
         -- With no check configured the step is skipped, never passed; the work
         -- is still published, as a draft whose publication records no check.
         OR EXISTS(SELECT 1 FROM dev_machine_agent_run_steps s WHERE s.agent_run_id=$1 AND s.sequence=0 AND s.step_type='validation' AND s.status='skipped' AND s.command_argv='[]'::jsonb AND s.completed_at IS NOT NULL)`, task.RunID); err != nil {
			return err
		}
		if !passed {
			return ErrAgentTaskConflict
		}
	}
	if err = transitionAgentTask(ctx, tx, task, to, reason, nil); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) RequestCancel(ctx context.Context, ws, id uuid.UUID) error {
	if _, err := conversationActor(ctx, ws); err != nil {
		return err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if task.State == "cancelled" {
		return nil
	}
	// A person's cancel always wins: an attempt being stopped to deliver a
	// message is not continued behind their back, and nothing that happened
	// to an earlier interruption makes the cleanup start the task again.
	if task.LiveResumeGeneration != nil {
		interrupting := task.LiveInterrupting()
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET live_resume_generation=NULL,
 reason=CASE WHEN $2 THEN 'Cancellation requested' ELSE reason END,next_poll_at=CASE WHEN $2 THEN now() ELSE next_poll_at END,updated_at=now() WHERE id=$1 RETURNING *`, id, interrupting); err != nil {
			return err
		}
		if interrupting && task.ConversationID != nil {
			if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
				return err
			}
		}
	}
	// Cancelling a paused workflow ends the pause: the task is cancelled, not
	// left blocked by the cleanup of the attempt the pause stopped.
	if paused := task.WorkflowPausedAt != nil; paused || task.WorkflowPauseRequest != nil {
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_paused_at=NULL,workflow_pause_request=NULL WHERE id=$1 RETURNING *`, id); err != nil {
			return err
		}
		if paused && task.State == "cancelling" {
			// The cleanup the pause started goes on, now as a cancellation; a
			// cleanup that failed is started again below.
			if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET reason='Cancellation requested',next_poll_at=now(),updated_at=now() WHERE id=$1 RETURNING *`, id); err != nil {
				return err
			}
			if task.ConversationID != nil {
				if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
					return err
				}
			}
		}
	}
	if task.State == "cancelling" {
		result, err := tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='pending',attempts=0,available_at=now(),error_code=NULL,error_message=NULL WHERE workspace_id=$1 AND status='failed' AND ((agent_run_id=$2 AND idempotency_key=$3) OR (machine_id=$4 AND idempotency_key=$5))`, task.WorkspaceID, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String(), task.ReservedMachineID, fmt.Sprintf("task-idle:%s:%d", task.ID, task.Generation))
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET failure_count=0,reason='Runtime cleanup retry requested',next_poll_at=now() WHERE id=$1 RETURNING *`, id); err != nil {
				return err
			}
			if task.ConversationID != nil {
				if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
					return err
				}
			}
		}
		return tx.Commit()
	}
	if err = transitionAgentTask(ctx, tx, task, "cancelling", "Cancellation requested", &task.CreatorID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) Revoke(ctx context.Context, claimed domain.AgentTask) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	// Cleanup authority is not execution authority. Refuse to revoke a live grant.
	var authorized bool
	err = tx.GetContext(ctx, &authorized, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id AND wm.user_id=c.creator_id AND wm.role<>'guest' WHERE c.id=$1 AND `+conversationScopePredicate("c")+`)`, task.ConversationID)
	if err != nil {
		return err
	}
	if authorized {
		return ErrAgentTaskConflict
	}
	if task.State != "cancelling" {
		if err = transitionAgentTask(ctx, tx, task, "cancelling", "Issue access was revoked", nil); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, task.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) FinishCancellation(ctx context.Context, claimed domain.AgentTask, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	var active bool
	err = tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.id=$1 AND
 (r.status IN ('queued','starting','running','waiting_input') OR NOT EXISTS(
  SELECT 1 FROM dev_machine_operations o WHERE o.agent_run_id=r.id AND o.idempotency_key=$2 AND o.status='completed')))
 OR EXISTS(SELECT 1 FROM dev_machine_operations WHERE (agent_run_id=$1 OR checkout_id=$3 OR (machine_id=$4 AND $4=$5)) AND status IN ('pending','leased'))`, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String(), task.CheckoutID, task.MachineID, task.ReservedMachineID)
	if err != nil {
		return err
	}
	if active {
		return ErrAgentTaskConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_run_steps SET status='cancelled',completed_at=now(),summary='Runtime cancellation confirmed' WHERE agent_run_id=$1 AND step_type IN ('validation','publication','manual_check') AND status IN ('queued','running')`, task.RunID); err != nil {
		return err
	}
	target := "cancelled"
	if task.WorkflowPausedAt != nil {
		// A person paused the workflow while its agent worked: the attempt is
		// stopped, and the task waits to be resumed.
		target, reason = "blocked", workflowPausedReason
	} else if task.LiveInterrupting() && !task.FailurePending {
		// A person interrupted the agent to deliver a message: one action,
		// stopped and continued on the same session. A continuation that
		// cannot happen leaves the task saying so, never silently stopped.
		if err = continueInterruptedForLiveMessage(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	} else if task.FailurePending && task.UsageLimitMessage != nil {
		// A usage limit pauses the task; the reason says until when.
		target, reason = "blocked", task.Reason
	} else if task.FailurePending {
		target = "failed"
		// A classified failure keeps the real cause recorded by the run: it
		// tells the user what to change or retry. So does a resource limit, a
		// workflow's own reason such as a step's visit limit, and an agent
		// that ended without a final message.
		if task.FailureClass != nil && strings.TrimSpace(task.Reason) != "" {
			reason = task.Reason
		} else if domain.IsResourceLimitReason(task.Reason) || domain.IsWorkflowReason(task.Reason) || domain.IsNoFinalMessageReason(task.Reason) {
			reason = task.Reason
		} else {
			reason = "Execution failed; runtime cleanup confirmed"
		}
	}
	// An interruption that ends any other way (the attempt failed while it
	// was being stopped) is over: a later retry is not its continuation.
	if task.LiveResumeGeneration != nil && *task.LiveResumeGeneration > task.Generation {
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET live_resume_generation=NULL WHERE id=$1 RETURNING *`, task.ID); err != nil {
			return err
		}
	}
	if err = transitionAgentTask(ctx, tx, task, target, reason, nil); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) Release(ctx context.Context, claimed domain.AgentTask) error {
	result, err := r.db.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now()+make_interval(secs=>power(2,failure_count)::int) WHERE id=$1 AND lease_token=$2 AND generation=$3`, claimed.ID, claimed.LeaseToken, claimed.Generation)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrAgentTaskConflict
	}
	return nil
}

// Runtime writes carry task authority through existing services. Fencing occurs
// inside their transaction, so cancellation/revocation cannot race a queued effect.
func beginTaskRuntimeTransaction(ctx context.Context, db *sqlx.DB, kind string, ws, machine, object uuid.UUID) (*sqlx.Tx, error) {
	authority, ok := domain.TaskExecutionFromContext(ctx)
	if !ok {
		return db.BeginTxx(ctx, nil)
	}
	tx, err := beginConversationTransaction(ctx, db)
	if err != nil {
		return nil, err
	}
	task, err := lockAgentTask(ctx, tx, authority.TaskID, true)
	if err == nil {
		claimed := *task
		claimed.LeaseToken = &authority.LeaseToken
		claimed.Generation = authority.Generation
		err = fenceAgentTask(ctx, tx, *task, claimed)
	}
	if err == nil {
		valid := task.WorkspaceID == ws && task.MachineID != nil && *task.MachineID == machine
		switch kind {
		case "machine":
			valid = valid && task.State == "preparing" && task.ReservedMachineID == machine
		case "start":
			valid = valid && task.State == "preparing" && task.ReservedMachineID == machine && object == machine
		case "checkout":
			valid = valid && task.State == "preparing" && task.CheckoutID == object
		case "run":
			valid = valid && (task.State == "preparing" || task.State == "running") && task.RunID == object
		default:
			valid = false
		}
		if !valid {
			err = ErrAgentTaskConflict
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// MachineReservation is the machine a task runs on after a reservation and the
// task's current reservation ID. A uuid.Nil MachineID means the task has no
// machine yet: it starts a task machine under ReservedID unless it waits for a
// task slot. Abandoned, when set, is a machine created for this task that never
// started and can be deleted.
type MachineReservation struct {
	MachineID  uuid.UUID
	ReservedID uuid.UUID
	Abandoned  *uuid.UUID
}

// ErrMachineSlotWait reports a task waiting for a free task slot on a suitable
// machine of its creator that already runs its allowed number of tasks.
var ErrMachineSlotWait = errors.New("no free task slot on the machine")

// MachineSlotWaitError names the full machine a task waits on.
type MachineSlotWaitError struct {
	Name  string
	Tasks int
	Limit int
}

func (e *MachineSlotWaitError) Error() string {
	return fmt.Sprintf("%s already runs %d of %d task slots", e.Name, e.Tasks, e.Limit)
}

func (e *MachineSlotWaitError) Unwrap() error { return ErrMachineSlotWait }

// taskSlotLimit bounds how many active tasks share a machine: its own
// max_tasks, or the workspace policy's task_machine_max_tasks when it has none.
// The reusable-machine query and the full-machine lookup both use it.
const taskSlotLimit = "COALESCE(m.max_tasks, policy.task_machine_max_tasks)"

// machineSelectionSQL is a machine that suits a task of its creator, without
// the free task slot condition: both the machine a task shares and the full
// machine it waits on are chosen from it.
func machineSelectionSQL() string {
	return `FROM dev_machines m
 JOIN dev_machine_workspace_policies policy ON policy.workspace_id=m.workspace_id AND policy.enabled
 CROSS JOIN LATERAL (SELECT count(*) AS tasks FROM agent_tasks t WHERE t.machine_id=m.id AND t.id<>$6
  AND t.state NOT IN ('completed','cancelled','failed','blocked','awaiting_review') AND ` + taskHoldsMachine("t") + `) load
 CROSS JOIN LATERAL (SELECT load.tasks=0
  AND NOT EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.machine_id=m.id AND r.status IN ('queued','starting','running','waiting_input'))
  AND NOT EXISTS(SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id AND o.status IN ('pending','leased') AND o.action IN ('validate_agent','publish_agent')) AS idle) state
 WHERE m.workspace_id=$1 AND m.created_by_user_id=$2
 AND m.status='running' AND m.desired_status='running'
 AND m.delete_requested_at IS NULL AND NOT m.environment_builder
 AND m.profile IN ('standard','headless')
 AND m.environment_id IS NOT DISTINCT FROM $4::uuid
 AND (policy.allow_sudo OR NOT m.sudo_enabled)
 AND (policy.allow_docker OR NOT EXISTS(SELECT 1 FROM dev_machine_services s WHERE s.machine_id=m.id AND s.service_type='docker'))
 AND NOT EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.machine_id=m.id AND r.task_id IS NULL AND r.status IN ('queued','starting','running','waiting_input'))
 AND NOT EXISTS(SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id AND o.status IN ('pending','leased')
  AND o.action IN ('stop','pause','teardown','snapshot_environment','run_check','run_tool'))`
}

// machineSelectionOrder ranks an idle machine first, then the least loaded one,
// with the task's issue, recent activity, cached repository and agent breaking
// ties.
const machineSelectionOrder = `
 ORDER BY state.idle DESC, load.tasks,
  EXISTS(SELECT 1 FROM agent_tasks t WHERE t.machine_id=m.id AND t.issue_id=$7) DESC,
  m.last_activity_at DESC NULLS LAST,
  EXISTS(SELECT 1 FROM dev_machine_checkouts c WHERE c.machine_id=m.id AND c.github_repo_id=$3) DESC,
  EXISTS(SELECT 1 FROM dev_machine_agent_providers p WHERE p.machine_id=m.id AND p.provider_id=$5 AND p.enabled) DESC,
  m.id`

// reusableMachine returns the machine with a free task slot, if any.
func reusableMachine(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, repositoryID uuid.UUID, environmentID *uuid.UUID) (uuid.UUID, error) {
	var machineID uuid.UUID
	err := tx.GetContext(ctx, &machineID, `SELECT m.id `+machineSelectionSQL()+`
 AND load.tasks < `+taskSlotLimit+machineSelectionOrder+`
 FOR UPDATE OF m SKIP LOCKED LIMIT 1`,
		task.WorkspaceID, task.CreatorID, repositoryID, environmentID, task.Provider, task.ID, task.IssueID)
	return machineID, err
}

// machineLoad is a suitable machine of the task's creator and how much of its
// task slot limit is in use.
type machineLoad struct {
	Name  string
	Tasks int
	Limit int
}

// fullMachine returns the suitable machine a task would use, whether or not it
// has a free task slot. reusableMachine already took every unlocked machine
// with a free slot, so a machine below its limit here is held by another
// transaction's row lock or freed its last slot between the two reads: the
// reservation retries instead of creating a machine. A machine at its limit is
// the one the task waits on.
func fullMachine(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, repositoryID uuid.UUID, environmentID *uuid.UUID) (*machineLoad, error) {
	var load machineLoad
	err := tx.QueryRowxContext(ctx, `SELECT m.name, load.tasks, `+taskSlotLimit+` `+machineSelectionSQL()+machineSelectionOrder+`
 LIMIT 1`,
		task.WorkspaceID, task.CreatorID, repositoryID, environmentID, task.Provider, task.ID, task.IssueID).Scan(&load.Name, &load.Tasks, &load.Limit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &load, nil
}

// saveTaskMachine stores the machine a task waits on and, when it left a
// machine it cannot keep, a fresh reservation for the machine it starts next.
// uuid.Nil leaves the task without a machine.
func saveTaskMachine(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, machineID uuid.UUID, freshReservation bool) error {
	query := `UPDATE agent_tasks SET machine_id=$2,updated_at=now() WHERE id=$1 RETURNING *`
	if freshReservation {
		query = `UPDATE agent_tasks SET machine_id=$2,reserved_machine_id=gen_random_uuid(),updated_at=now() WHERE id=$1 RETURNING *`
	}
	var machine any
	if machineID != uuid.Nil {
		machine = machineID
	}
	if err := tx.GetContext(ctx, task, query, task.ID, machine); err != nil {
		return err
	}
	_, err := conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task)
	return err
}

// ReserveMachine serializes compatible-machine selection with other task
// reservations. A task waits for a free task slot on a suitable machine of its
// creator instead of creating one the host cannot start. A task whose own task
// machine never started moves to such a slot when one frees, reporting its
// unstarted machine for deletion; otherwise it keeps waiting for host capacity.
func (r *AgentTaskRepository) ReserveMachine(ctx context.Context, claimed domain.AgentTask, repositoryID uuid.UUID, environmentID *uuid.UUID) (MachineReservation, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return MachineReservation{}, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return MachineReservation{}, err
	}
	if task.State != "preparing" {
		return MachineReservation{}, ErrAgentTaskConflict
	}
	reservation := MachineReservation{ReservedID: task.ReservedMachineID}
	if task.MachineID != nil {
		reservation.MachineID = *task.MachineID
	}
	var gone bool
	var abandoned *uuid.UUID
	if task.MachineID != nil {
		// A reservation for a machine that was never created (e.g. blocked by
		// the machine limit) is revisited, so a free task slot can be used. A
		// destroyed or deleting machine is kept only as history: the task gets
		// a new reservation, since that machine ID cannot be reused. A machine
		// created for this task that never started can make way for a slot.
		var pending, unstarted bool
		// This read is unlocked, so the manager may move the machine from
		// queued to spawning before the delete below. That is safe:
		// requestPermanentDelete locks the row and queues a teardown with a
		// generation bump, and the task never ran on that machine.
		if err = tx.QueryRowContext(ctx, `SELECT $1::uuid=$2::uuid AND NOT EXISTS(SELECT 1 FROM dev_machines WHERE id=$1),
 EXISTS(SELECT 1 FROM dev_machines WHERE id=$1 AND (status='destroyed' OR delete_requested_at IS NOT NULL)),
 $1::uuid=$2::uuid AND EXISTS(SELECT 1 FROM dev_machines WHERE id=$1 AND status='queued' AND started_at IS NULL AND delete_requested_at IS NULL)`,
			*task.MachineID, task.ReservedMachineID).Scan(&pending, &gone, &unstarted); err != nil {
			return MachineReservation{}, err
		}
		switch {
		case gone:
			if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET machine_id=NULL,reserved_machine_id=gen_random_uuid(),updated_at=now() WHERE id=$1 RETURNING *`, task.ID); err != nil {
				return MachineReservation{}, err
			}
			reservation = MachineReservation{ReservedID: task.ReservedMachineID}
		case unstarted:
			own := *task.MachineID
			abandoned = &own
		case !pending:
			return reservation, nil
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, task.WorkspaceID); err != nil {
		return MachineReservation{}, err
	}
	machineID, err := reusableMachine(ctx, tx, task, repositoryID, environmentID)
	if err == nil {
		reservation.MachineID = machineID
		reservation.Abandoned = abandoned
		if err = saveTaskMachine(ctx, tx, task, machineID, abandoned != nil); err != nil {
			return MachineReservation{}, err
		}
		reservation.ReservedID = task.ReservedMachineID
		return reservation, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MachineReservation{}, err
	}
	// No machine has a free task slot. A machine below its limit is here
	// because another transaction locks its row (or a slot freed between the
	// two reads): the next poll takes that slot rather than starting a machine
	// the host cannot admit. A machine at its limit is one the task waits on.
	load, err := fullMachine(ctx, tx, task, repositoryID, environmentID)
	if err != nil {
		return MachineReservation{}, err
	}
	if load != nil {
		if load.Tasks < load.Limit {
			return MachineReservation{}, ErrAgentTaskConflict
		}
		if gone || abandoned != nil {
			reservation.MachineID = uuid.Nil
			reservation.Abandoned = abandoned
			if err = saveTaskMachine(ctx, tx, task, uuid.Nil, abandoned != nil); err != nil {
				return MachineReservation{}, err
			}
			reservation.ReservedID = task.ReservedMachineID
			if err = tx.Commit(); err != nil {
				return MachineReservation{}, err
			}
		}
		return reservation, &MachineSlotWaitError{Name: load.Name, Tasks: load.Tasks, Limit: load.Limit}
	}
	if abandoned != nil {
		// No other machine: the task keeps waiting for host capacity on its own
		// unstarted machine, as it did before a slot could free.
		return reservation, nil
	}
	reservation.MachineID = task.ReservedMachineID
	if err = saveTaskMachine(ctx, tx, task, reservation.MachineID, false); err != nil {
		return MachineReservation{}, err
	}
	reservation.ReservedID = task.ReservedMachineID
	return reservation, tx.Commit()
}

// stopFailure says who stopped an attempt this repository is cancelling, and
// why, from what the task already records. A cleanup that follows a failure
// keeps the run's own class and cause, so it returns nothing.
func stopFailure(task domain.AgentTask) (domain.FailureClass, string) {
	switch {
	case task.FailurePending:
		return "", ""
	case task.WorkflowPausedAt != nil:
		return domain.FailurePerson, "A person paused the work; the run was stopped to wait for them"
	case task.LiveInterrupting():
		return domain.FailurePerson, "A person stopped the run to deliver their message"
	case task.Reason == "Issue access was revoked":
		return domain.FailurePolicy, "The run was stopped because access to the issue or the workspace policy no longer allows it"
	default:
		return domain.FailurePerson, "The run was cancelled by a person"
	}
}

// CancelAttempt always queues physical cleanup, including for native attempts
// already marked failed/cancelled. Their status alone says nothing about a live
// container. The operation key makes crash recovery and repeated calls harmless.
func (r *AgentTaskRepository) CancelAttempt(ctx context.Context, claimed domain.AgentTask) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if task.State != "cancelling" {
		return ErrAgentTaskConflict
	}
	var run domain.DevMachineAgentRun
	err = tx.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1 AND workspace_id=$2`, task.RunID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		// No attempt yet: the task may still wait for its machine to start.
		if err = withdrawTaskMachineStart(ctx, tx, *task); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if task.MachineID == nil || run.MachineID != *task.MachineID {
		return ErrAgentTaskConflict
	}
	var generation int64
	if err = tx.GetContext(ctx, &generation, `SELECT generation FROM dev_machines WHERE id=$1 AND workspace_id=$2 FOR SHARE`, run.MachineID, task.WorkspaceID); err != nil {
		return err
	}
	// The reason the attempt is stopped is known here, so the class and cause
	// are written with the cancellation: a person's cancel, pause or message,
	// a policy refusal, or the failure that cleanup follows. A run that
	// already has a class keeps it.
	class, cause := stopFailure(*task)
	_, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET status='cancelled',cancelled_at=now(),completed_at=now(),
 failure_class=COALESCE(failure_class,NULLIF($2,'')),failure_cause=COALESCE(failure_cause,NULLIF($3,''))
 WHERE id=$1 AND status IN ('queued','starting','running','waiting_input')`, run.ID, string(class), cause)
	if err != nil {
		return err
	}
	operation := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: run.MachineID, AgentRunID: &run.ID, Action: domain.DevMachineOpCancelAgent, Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: "task-cancel:" + task.ID.String() + ":" + run.ID.String(), MaxAttempts: 5}
	if err = enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	if operation.Status == domain.DevMachineOpStatusFailed {
		// Nobody asked to give this task up: the cleanup of an attempt a pause
		// stopped, or a message interrupted, is started again until it
		// succeeds; the task then waits paused, or continues with the
		// message. Any other cleanup waits for its creator's explicit retry.
		if task.WorkflowPausedAt == nil && !task.LiveInterrupting() {
			return ErrAgentTaskCleanupFailed
		}
		if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='pending',attempts=0,available_at=now(),error_code=NULL,error_message=NULL WHERE workspace_id=$1 AND agent_run_id=$2 AND idempotency_key=$3 AND status='failed'`,
			task.WorkspaceID, run.ID, operation.IdempotencyKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Native attempts keep their task identity even after a retry changes run_id.
// Missing, moved, cancelled and superseded tasks must never fall back to manual
// run authorization when a queued infrastructure operation is recovered.
func beginAuthorizedAgentRun(ctx context.Context, db *sqlx.DB, runID uuid.UUID) (*sqlx.Tx, error) {
	var run domain.DevMachineAgentRun
	if err := db.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1`, runID); err != nil {
		return nil, err
	}
	if run.TaskID == nil {
		return db.BeginTxx(ctx, nil)
	}
	var creator uuid.UUID
	if err := db.GetContext(ctx, &creator, `SELECT creator_id FROM agent_tasks WHERE id=$1 AND workspace_id=$2`, run.TaskID, run.WorkspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentTaskUnauthorized
		}
		return nil, err
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: creator})
	tx, err := beginConversationTransaction(ctx, db)
	if err != nil {
		return nil, err
	}
	task, err := lockAgentTask(ctx, tx, *run.TaskID, true)
	if errors.Is(err, ErrConversationNotFound) {
		err = ErrAgentTaskUnauthorized
	}
	if err == nil {
		allowed := task.RunID == run.ID && task.MachineID != nil && *task.MachineID == run.MachineID && run.RequestedByUserID != nil && *run.RequestedByUserID == task.CreatorID
		allowed = allowed && (task.State == "preparing" || task.State == "running" || task.State == "waiting_input")
		if !allowed {
			err = ErrAgentTaskUnauthorized
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
func (r *DevMachineRepository) AuthorizeAgentRun(ctx context.Context, runID uuid.UUID) error {
	tx, err := beginAuthorizedAgentRun(ctx, r.db, runID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.GetContext(ctx, &active, `SELECT status IN ('queued','starting','running','waiting_input') FROM dev_machine_agent_runs WHERE id=$1 FOR SHARE`, runID); err != nil {
		return err
	}
	if !active {
		return sql.ErrNoRows
	}
	return nil
}

// RecordStepResult counts unexpected worker failures, not ordinary capacity or
// cleanup waits. No provider/SQL error text is persisted into the conversation.
func (r *AgentTaskRepository) RecordStepResult(ctx context.Context, claimed domain.AgentTask, failed bool) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	count := 0
	if failed {
		count = min(task.FailureCount+1, 5)
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET failure_count=$2 WHERE id=$1 RETURNING *`, task.ID, count); err != nil {
		return err
	}
	if count == 5 && task.State != "cancelling" {
		if err = beginAgentTaskFailure(ctx, tx, task, "", "Worker recovery exhausted; stopping execution"); err != nil {
			return err
		}
	}
	if count == 5 && task.State == "cancelling" && task.Reason != "Cleanup has not completed; retry cancellation after restoring the machine manager" {
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET reason='Cleanup has not completed; retry cancellation after restoring the machine manager' WHERE id=$1 RETURNING *`, task.ID); err != nil {
			return err
		}
		if task.ConversationID != nil {
			if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func beginAgentTaskFailure(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, class domain.FailureClass, reason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET failure_pending=true, failure_class=NULLIF($2,'') WHERE id=$1`, task.ID, string(class)); err != nil {
		return err
	}
	task.FailurePending = true
	return transitionAgentTask(ctx, tx, task, "cancelling", reason, nil)
}
func (r *AgentTaskRepository) Fail(ctx context.Context, claimed domain.AgentTask, reason string) error {
	return r.FailWithClass(ctx, claimed, "", reason)
}

// FailWithClass fails the attempt and stores who or what caused it (KUA-137),
// so a task never ends with only "Execution failed".
func (r *AgentTaskRepository) FailWithClass(ctx context.Context, claimed domain.AgentTask, class domain.FailureClass, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if err = beginAgentTaskFailure(ctx, tx, task, class, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// FailForSignIn fails the attempt like Fail and marks that the agent needs the
// user's own sign-in, which the task panel then offers.
func (r *AgentTaskRepository) FailForSignIn(ctx context.Context, claimed domain.AgentTask, reason string) error {
	return r.FailForSignInWithClass(ctx, claimed, domain.FailureProvider, reason)
}

func (r *AgentTaskRepository) FailForSignInWithClass(ctx context.Context, claimed domain.AgentTask, class domain.FailureClass, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET sign_in_required=true WHERE id=$1`, task.ID); err != nil {
		return err
	}
	if err = beginAgentTaskFailure(ctx, tx, task, class, reason); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrAgentTaskCleanupFailed = errors.New("task runtime cleanup needs an explicit retry")

func (r *AgentTaskRepository) Retry(ctx context.Context, ws, id uuid.UUID, req domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	req.Prompt, req.Model = strings.TrimSpace(req.Prompt), strings.TrimSpace(req.Model)
	if req.RequestID == uuid.Nil || req.Generation < 1 || len(req.Prompt) == 0 || len(req.Prompt) > 32768 || !utf8.ValidString(req.Prompt) || utf8.RuneCountInString(req.Model) > 200 || !utf8.ValidString(req.Model) {
		return nil, ErrConversationInvalid
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if previous, err := replayAgentTaskRetry(ctx, tx, task, req); err != nil || previous != nil {
		return previous, err
	}
	// A review is requested again rather than retried, which pins it to the
	// commit published at that time.
	if task.Kind == domain.AgentTaskKindReview || task.Generation != req.Generation || (task.State != "failed" && task.State != "blocked" && task.State != "awaiting_review") {
		return nil, ErrAgentTaskConflict
	}
	var pending bool
	err = tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased'))
 OR EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.id=$3 AND r.status<>'succeeded' AND NOT EXISTS(SELECT 1 FROM dev_machine_operations o WHERE o.agent_run_id=r.id AND o.idempotency_key=$4 AND o.status='completed'))`, task.ID, task.CheckoutID, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String())
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, ErrAgentTaskConflict
	}
	requested, command, err := resolveAgentTaskCommand(ctx, tx, ws, task.IssueID, req.TestCommand)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_retries(task_id,request_id,expected_generation,prompt,model,model_server_id) VALUES($1,$2,$3,$4,$5,$6)`, id, req.RequestID, req.Generation, req.Prompt, req.Model, requestedRetryServer(req)); err != nil {
		return nil, err
	}
	instructions, err := resolveScopeDefault(ctx, tx, ws, task.IssueID, "agent_instructions")
	if err != nil {
		return nil, err
	}
	compose, err := resolveScopeDefault(ctx, tx, ws, task.IssueID, "compose_file")
	if err != nil {
		return nil, err
	}
	preview, previewPort, err := resolvePreview(ctx, tx, ws, task.IssueID)
	if err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET model=COALESCE(NULLIF($9::text,''),t.model),thinking=CASE WHEN $9::text<>'' THEN $10::text ELSE t.thinking END,model_server_id=CASE WHEN $9::text<>'' THEN $11::uuid ELSE t.model_server_id END,prompt=$2,test_command=$3,validation_command=$4,agent_instructions=CASE WHEN t.workflow_version_id IS NOT NULL AND t.kind<>'review' THEN t.agent_instructions ELSE $5 END,compose_file=$6,preview_command=$7,preview_port=$8,generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,failure_class=NULL,sign_in_required=false,usage_limit_message=NULL,usage_limit_resets_at=NULL,usage_limit_resume_at=NULL,usage_limit_resumes=0,resume_native_session=false,live_resume_generation=NULL,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),issue_status_id=i.status_id,issue_updated_at=i.updated_at FROM issues i WHERE t.id=$1 AND i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id RETURNING t.*`, id, req.Prompt, requested, command, instructions, compose, preview, previewPort, req.Model, req.Thinking, req.ModelServerID); err != nil {
		return nil, err
	}
	// A workflow task continues on its implementing agent step, keeping that
	// step's instructions (project defaults joined with its profile's and the
	// step's own, set when the step was entered).
	if err = forgetWorkflowPause(ctx, tx, task, &task.CreatorID); err != nil {
		return nil, err
	}
	if err = retryWorkflow(ctx, tx, task, task.CreatorID); err != nil {
		return nil, err
	}
	if req.Note != nil {
		// A retry's own instructions are the creator's: only they may retry.
		if note := req.Note.Instruction; note != nil && note.AuthorID == nil {
			author := task.CreatorID
			note.AuthorID = &author
		}
		if err = noteNotebook(ctx, tx, task.ID, &task.CreatorID, *req.Note); err != nil {
			return nil, err
		}
	}
	if err = transitionAgentTask(ctx, tx, task, "queued", "New execution iteration requested", &task.CreatorID); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// RelatedWork is the coordination context placed in a task's native prompt.
func (r *AgentTaskRepository) RelatedWork(ctx context.Context, task domain.AgentTask) ([]domain.RelatedWork, error) {
	return NewIssueDependencyRepository(r.db).RelatedWork(ctx, task.IssueID)
}

// IssueBrief is the issue text placed in a task's native prompt.
func (r *AgentTaskRepository) IssueBrief(ctx context.Context, task domain.AgentTask) (*domain.IssueBrief, error) {
	brief := &domain.IssueBrief{}
	err := r.db.GetContext(ctx, brief, `SELECT identifier_text, title, description FROM issues WHERE id=$1 AND workspace_id=$2`, task.IssueID, task.WorkspaceID)
	return brief, err
}

// NoteWaiting records why a preparing task cannot proceed yet, so the user
// sees it instead of a stale reason. It never changes the task's state.
func (r *AgentTaskRepository) NoteWaiting(ctx context.Context, claimed domain.AgentTask, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if task.State != "preparing" || task.Reason == reason {
		return nil
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET reason=$2,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, reason); err != nil {
		return err
	}
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
			return err
		}
	}
	return tx.Commit()
}
