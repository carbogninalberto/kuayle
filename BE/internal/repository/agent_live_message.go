package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

var (
	ErrAgentLiveMessageInvalid     = errors.New("invalid live message")
	ErrAgentLiveMessageClosed      = errors.New("the task no longer takes messages")
	ErrAgentLiveMessageRateLimited = errors.New("too many live messages for this task")
)

// liveMessagesPerDelivery bounds one inbox write or prompt.
const liveMessagesPerDelivery = 10

// QueueLiveMessage queues text from the task's creator, or from the
// orchestrator acting for them, for the task's agent: the running attempt
// receives it through its inbox, otherwise the next attempt's prompt does.
// Only the creator may message the agent, within the hourly budget, until the
// task is completed or cancelled (or being cancelled). The task row lock
// orders sequence numbers.
func (r *AgentTaskRepository) QueueLiveMessage(ctx context.Context, ws, id uuid.UUID, sender, body string) (*domain.AgentLiveMessage, error) {
	return r.queueLiveMessage(ctx, ws, id, sender, body, nil, "")
}

// SendLiveMessage queues the creator's message like QueueLiveMessage and, in
// the same transaction, acts on mode: domain.AgentLiveSendInterrupt stops the
// working attempt, which is then resumed on the same session with the
// message; domain.AgentLiveSendStart starts an attempt for it when none runs.
// A mode the task's state does not allow is ErrAgentTaskConflict and queues
// nothing.
func (r *AgentTaskRepository) SendLiveMessage(ctx context.Context, ws, id uuid.UUID, body, mode string) (*domain.AgentLiveMessage, error) {
	if mode != "" && mode != domain.AgentLiveSendInterrupt && mode != domain.AgentLiveSendStart {
		return nil, ErrAgentLiveMessageInvalid
	}
	return r.queueLiveMessage(ctx, ws, id, domain.AgentLiveSenderUser, body, nil, mode)
}

// QueueApprovedLiveMessage queues the message of an approved message_task
// request, from the orchestrator acting for the task's creator, under the
// same rules as QueueLiveMessage. The approval's request ID makes it
// idempotent: repeating or resuming the approval returns the message it
// already queued instead of sending the text again.
func (r *AgentTaskRepository) QueueApprovedLiveMessage(ctx context.Context, ws, id, request uuid.UUID, body string) (*domain.AgentLiveMessage, error) {
	if request == uuid.Nil {
		return nil, ErrAgentLiveMessageInvalid
	}
	return r.queueLiveMessage(ctx, ws, id, domain.AgentLiveSenderOrchestrator, body, &request, "")
}

func (r *AgentTaskRepository) queueLiveMessage(ctx context.Context, ws, id uuid.UUID, sender, body string, request *uuid.UUID, mode string) (*domain.AgentLiveMessage, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	if sender != domain.AgentLiveSenderUser && sender != domain.AgentLiveSenderOrchestrator {
		return nil, ErrAgentLiveMessageInvalid
	}
	body, err = domain.CleanAgentLiveMessage(body)
	if err != nil {
		return nil, ErrAgentLiveMessageInvalid
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	if task.Kind != domain.AgentTaskKindImplement {
		return nil, ErrAgentLiveMessageInvalid
	}
	var message domain.AgentLiveMessage
	if request != nil {
		// The task row lock serializes requests, so a replay finds the first one.
		err = tx.GetContext(ctx, &message, `SELECT * FROM agent_live_messages WHERE task_id=$1 AND request_id=$2`, id, *request)
		if err == nil {
			return &message, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	// A task that is being cancelled takes no message; one whose agent is
	// only being stopped by a pause or an interruption does, for when it
	// continues.
	stopping := task.State == "cancelling" && (task.WorkflowPausedAt != nil || task.LiveInterrupting())
	if task.State == "completed" || task.State == "cancelled" || (task.State == "cancelling" && !stopping) {
		return nil, ErrAgentLiveMessageClosed
	}
	var sent int
	if err = tx.GetContext(ctx, &sent, `SELECT count(*) FROM agent_live_messages WHERE task_id=$1 AND direction='to_agent' AND created_at>now()-interval '1 hour'`, id); err != nil {
		return nil, err
	}
	if sent >= domain.AgentLiveMessagesPerHour {
		return nil, ErrAgentLiveMessageRateLimited
	}
	err = tx.GetContext(ctx, &message, `INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,author_id,body,state,request_id)
 SELECT $1,$2,'to_agent',COALESCE(max(seq),0)+1,$3,$4,$5,'queued',$6 FROM agent_live_messages WHERE task_id=$2 RETURNING *`, ws, id, sender, actor, body, request)
	if err != nil {
		return nil, err
	}
	// What a person tells the agent is part of the task's notebook. A message
	// the assistant drafted and the person approved is marked as such.
	if err = noteNotebook(ctx, tx, id, &actor, domain.NotebookNote{Instruction: &domain.NotebookInstruction{
		Source: domain.NotebookInstructionLive, AuthorID: &actor, Assistant: sender == domain.AgentLiveSenderOrchestrator, Text: message.Body, At: message.CreatedAt}}); err != nil {
		return nil, err
	}
	if err = liveMessageEvents(ctx, tx, []domain.AgentLiveMessage{message}); err != nil {
		return nil, err
	}
	switch mode {
	case domain.AgentLiveSendInterrupt:
		// An agent that is already being stopped for a message is not
		// interrupted twice: this message joins the one it resumes with.
		if !task.LiveInterrupting() {
			if err = interruptForLiveMessage(ctx, tx, task, actor); err != nil {
				return nil, err
			}
		}
	case domain.AgentLiveSendStart:
		if err = startForLiveMessage(ctx, tx, task, actor); err != nil {
			return nil, err
		}
	default:
		// A harness Kuayle cannot hand a message to in the middle of a turn
		// receives it by being interrupted and resumed, when its attempt can
		// be interrupted; otherwise the message waits for the next attempt.
		if !domain.HarnessReadsLiveInbox(task.Provider) {
			if err = interruptForLiveMessage(ctx, tx, task, actor); err != nil && !errors.Is(err, ErrAgentTaskConflict) {
				return nil, err
			}
		}
	}
	return &message, tx.Commit()
}

const (
	liveInterruptingReason = "Interrupting: stopping the agent to deliver a message"
	liveInterruptedReason  = "Continuing with a message from the task's owner"
	liveStartedReason      = "Started to deliver a message from the task's owner"
	// What a task says when its agent was stopped for a message and could not
	// be started again. A workflow is then paused and resumes from its step;
	// any other task is blocked and continues with a retry.
	liveResumeFailedReason  = "Paused: the agent was stopped to deliver your message but could not be started again. Resume the task to continue; the message is delivered then."
	liveRestartFailedReason = "The agent was stopped to deliver your message but could not be started again. Send and start now, or retry the task, to continue; the message is delivered then."
)

// CanInterruptForLiveMessage reports whether the task has a working attempt
// that can be stopped and continued for a message: an implementing task,
// with or without a workflow, whose agent is preparing, running or waiting
// for input and whose run (its status is run) has not ended. A task that is
// paused, failing or being cancelled cannot. It is computed without I/O.
func CanInterruptForLiveMessage(task domain.AgentTask, run string) bool {
	if task.Kind != domain.AgentTaskKindImplement || task.WorkflowPausedAt != nil || task.FailurePending {
		return false
	}
	if task.RunsWorkflow() && (task.WorkflowStatus == nil || *task.WorkflowStatus != domain.WorkflowStatusRunning) {
		return false
	}
	switch task.State {
	case "preparing", "running", "waiting_input":
	default:
		return false
	}
	switch run {
	case "queued", "starting", "running", "waiting_input":
		return true
	}
	return false
}

// interruptForLiveMessage stops the task's working attempt so that its
// session continues with the queued message: the attempt goes through the
// usual runtime cleanup (cancelling), which keeps the machine, the checkout,
// the worktree and the session, marked (live_resume_generation) so that the
// cleanup continues the attempt by itself instead of cancelling the task
// (FinishCancellation). It is not a pause: a workflow's pending "pause after
// this step" stays as the person asked for it. Nothing changes, and it
// reports ErrAgentTaskConflict, when no attempt is working.
func interruptForLiveMessage(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, actor uuid.UUID) error {
	var run string
	err := tx.GetContext(ctx, &run, `SELECT status::text FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2`, task.RunID, task.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !CanInterruptForLiveMessage(*task, run) {
		return ErrAgentTaskConflict
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET live_resume_generation=generation+1,
 lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),updated_at=now() WHERE id=$1 RETURNING *`, task.ID); err != nil {
		return err
	}
	if err = transitionAgentTask(ctx, tx, task, "cancelling", liveInterruptingReason, &actor); err != nil {
		return err
	}
	return workflowEvent(ctx, tx, *task)
}

// continueInterruptedForLiveMessage continues the attempt that was stopped
// to deliver a message, once it is gone: a new attempt on the same step,
// visit and iteration resumes its session. The caller holds the task, still
// cancelling. When that cannot happen the task is left saying so: a workflow
// paused on its step (Resume continues it), any other task blocked (a retry
// or "send and start now" continues it).
func continueInterruptedForLiveMessage(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask) error {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT live_resume`); err != nil {
		return err
	}
	stopped := *task
	err := restartInterruptedStep(ctx, tx, task, &task.CreatorID, liveInterruptedReason)
	if err == nil || ctx.Err() != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT live_resume`); err != nil {
		return err
	}
	*task = stopped
	// The interruption is over: whatever starts the task again later (a
	// resume, a retry) is that, and a cancellation cancels.
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET live_resume_generation=NULL WHERE id=$1 RETURNING *`, task.ID); err != nil {
		return err
	}
	reason := liveRestartFailedReason
	if task.RunsWorkflow() {
		reason = liveResumeFailedReason
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_pause_request=NULL,workflow_paused_at=now() WHERE id=$1 RETURNING *`, task.ID); err != nil {
			return err
		}
		if err = workflowPauseAudit(ctx, tx, *task, nil, domain.WorkflowPauseActionPaused, domain.WorkflowPauseNow); err != nil {
			return err
		}
	}
	return transitionAgentTask(ctx, tx, task, "blocked", reason, nil)
}

// startForLiveMessage starts an attempt for the queued message when none is
// running. A task whose agent a pause stopped resumes that step; a failed,
// blocked or waiting task starts a new iteration on its last agent step, as a
// retry does, but keeps its prompt and continues the previous session (when
// the same profile ran it). A task that is working, or paused by a usage
// limit, is ErrAgentTaskConflict.
func startForLiveMessage(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, actor uuid.UUID) error {
	if task.WorkflowPausedAt != nil && task.State == "blocked" {
		resume, err := ComputeWorkflowResume(*task, time.Now())
		if err != nil {
			return err
		}
		if err = endWorkflowPause(ctx, tx, task, resume, &actor); err != nil {
			return err
		}
		if err = restartInterruptedStep(ctx, tx, task, &actor, liveStartedReason); err != nil {
			return err
		}
		return tx.GetContext(ctx, task, `UPDATE agent_tasks SET live_resume_generation=generation WHERE id=$1 RETURNING *`, task.ID)
	}
	if (task.State != "failed" && task.State != "blocked" && task.State != "awaiting_review") || (task.State == "blocked" && task.UsageLimitMessage != nil) {
		return ErrAgentTaskConflict
	}
	var pending bool
	err := tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased') AND o.action NOT IN ('start_preview','stop_preview'))
 OR EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.id=$3 AND r.status<>'succeeded' AND NOT EXISTS(SELECT 1 FROM dev_machine_operations o WHERE o.agent_run_id=r.id AND o.idempotency_key=$4 AND o.status='completed'))`, task.ID, task.CheckoutID, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String())
	if err != nil {
		return err
	}
	if pending {
		return ErrAgentTaskConflict
	}
	err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET generation=generation+1,run_id=gen_random_uuid(),live_resume_generation=generation+1,failure_count=0,failure_pending=false,failure_class=NULL,sign_in_required=false,
 usage_limit_message=NULL,usage_limit_resets_at=NULL,usage_limit_resume_at=NULL,usage_limit_resumes=0,resume_native_session=false,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),
 issue_status_id=i.status_id,issue_updated_at=i.updated_at FROM issues i WHERE t.id=$1 AND i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id RETURNING t.*`, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentTaskConflict
	}
	if err != nil {
		return err
	}
	if err = forgetWorkflowPause(ctx, tx, task, &actor); err != nil {
		return err
	}
	if err = retryWorkflow(ctx, tx, task, actor); err != nil {
		return err
	}
	return transitionAgentTask(ctx, tx, task, "queued", liveStartedReason, &actor)
}

// LiveMessages lists the newest messages of a task's live channel, both
// directions, oldest first, for the task's creator.
func (r *AgentTaskRepository) LiveMessages(ctx context.Context, ws, id uuid.UUID, limit int) ([]domain.AgentLiveMessage, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	messages := []domain.AgentLiveMessage{}
	err = tx.SelectContext(ctx, &messages, `SELECT * FROM (SELECT * FROM agent_live_messages WHERE task_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2) m ORDER BY created_at,id`, id, limit)
	return messages, err
}

// ClaimLiveMessages places the task's queued messages, oldest first, in its
// current attempt's prompt within room bytes. Recreating the same attempt
// places the same messages; messages claimed for an attempt that was never
// created are claimed again. A message the task's previous attempt received
// on the same workflow step and never replied to is placed once more, until
// domain.AgentLiveReplyAttempts attempts that ran to an end were given it
// (an attempt that was interrupted, paused or cancelled does not count: it
// was not allowed to finish); it is never carried to another step.
func (r *AgentTaskRepository) ClaimLiveMessages(ctx context.Context, task domain.AgentTask, room int) ([]domain.AgentLiveMessage, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	messages := []domain.AgentLiveMessage{}
	if err = tx.SelectContext(ctx, &messages, `SELECT * FROM agent_live_messages WHERE task_id=$1 AND workspace_id=$2 AND state='delivered' AND delivered_via='prompt' AND run_id=$3 ORDER BY seq`, task.ID, task.WorkspaceID, task.RunID); err != nil || len(messages) > 0 {
		return messages, err
	}
	var candidates []struct {
		domain.AgentLiveMessage
		// Orphan: the attempt that held the message never ran to an end (it
		// was never created, or it was interrupted, paused or cancelled), so
		// this claim does not count as one more attempt that was given it.
		Orphan bool `db:"orphan"`
	}
	// The task's previous attempt, whose unanswered messages this one takes.
	const previous = `(SELECT rn.id FROM dev_machine_agent_runs rn WHERE rn.task_id=m.task_id AND rn.id<>$4 ORDER BY rn.created_at DESC,rn.id LIMIT 1)`
	const unfinished = `(NOT EXISTS (SELECT 1 FROM dev_machine_agent_runs rn WHERE rn.id=m.run_id) OR EXISTS (SELECT 1 FROM dev_machine_agent_runs rn WHERE rn.id=m.run_id AND rn.status='cancelled'))`
	if err = tx.SelectContext(ctx, &candidates, `SELECT m.*,(m.state='delivered' AND `+unfinished+`) AS orphan
 FROM agent_live_messages m WHERE m.task_id=$1 AND m.workspace_id=$2 AND m.direction='to_agent' AND (m.state='queued'
  OR (m.state='delivered' AND m.delivered_via='prompt' AND NOT EXISTS (SELECT 1 FROM dev_machine_agent_runs rn WHERE rn.id=m.run_id) AND NOT EXISTS (SELECT 1 FROM agent_tasks tk WHERE tk.run_id=m.run_id))
  OR (m.state='delivered' AND m.deliveries>=1 AND m.workflow_node IS NOT DISTINCT FROM $6::text AND m.run_id=`+previous+`
   AND (m.deliveries<=$5::int OR `+unfinished+`)
   AND NOT EXISTS (SELECT 1 FROM agent_live_messages a WHERE a.task_id=m.task_id AND a.direction='from_agent' AND a.state='received' AND a.reply_to=m.seq)))
 ORDER BY m.seq LIMIT $3 FOR UPDATE OF m SKIP LOCKED`, task.ID, task.WorkspaceID, liveMessagesPerDelivery, task.RunID, domain.AgentLiveReplyAttempts-1, task.WorkflowNode); err != nil {
		return nil, err
	}
	fresh, orphans := []uuid.UUID{}, []uuid.UUID{}
	for _, candidate := range candidates {
		cost := len(candidate.Body) + 240
		if cost > room {
			break
		}
		room -= cost
		if candidate.Orphan {
			orphans = append(orphans, candidate.ID)
		} else {
			fresh = append(fresh, candidate.ID)
		}
	}
	if len(fresh)+len(orphans) == 0 {
		return messages, nil
	}
	if err = tx.SelectContext(ctx, &messages, `UPDATE agent_live_messages SET state='delivered',delivered_via='prompt',run_id=$3,delivered_at=now(),workflow_node=$4,
 deliveries=CASE WHEN id=ANY($2) THEN GREATEST(deliveries,1) ELSE deliveries+1 END WHERE id=ANY($1) OR id=ANY($2) RETURNING *`, pq.Array(fresh), pq.Array(orphans), task.RunID, task.WorkflowNode); err != nil {
		return nil, err
	}
	if err = liveMessageEvents(ctx, tx, messages); err != nil {
		return nil, err
	}
	sort.Slice(messages, func(i, j int) bool { return *messages[i].Seq < *messages[j].Seq })
	return messages, tx.Commit()
}

// PendingLiveMessages returns, oldest first, the queued messages for the task
// whose current attempt is run, for the manager to write to its inbox.
func (r *DevMachineRepository) PendingLiveMessages(ctx context.Context, run uuid.UUID) ([]domain.AgentLiveMessage, error) {
	messages := []domain.AgentLiveMessage{}
	err := r.db.SelectContext(ctx, &messages, `SELECT m.* FROM agent_live_messages m JOIN agent_tasks t ON t.id=m.task_id AND t.workspace_id=m.workspace_id
 WHERE t.run_id=$1 AND t.state NOT IN ('completed','cancelled','cancelling') AND m.state='queued' ORDER BY m.seq LIMIT $2`, run, liveMessagesPerDelivery)
	return messages, err
}

// MarkLiveMessagesDelivered records that run's inbox received the messages.
// Only queued messages of the task whose current attempt is run change.
func (r *DevMachineRepository) MarkLiveMessagesDelivered(ctx context.Context, run uuid.UUID, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	messages := []domain.AgentLiveMessage{}
	err = tx.SelectContext(ctx, &messages, `UPDATE agent_live_messages m SET state='delivered',delivered_via='inbox',run_id=$2,delivered_at=now(),deliveries=m.deliveries+1,workflow_node=t.workflow_node
 FROM agent_tasks t WHERE t.id=m.task_id AND t.run_id=$2 AND m.id=ANY($1) AND m.state='queued' RETURNING m.*`, pq.Array(ids), run)
	if err != nil {
		return 0, err
	}
	if err = liveMessageEvents(ctx, tx, messages); err != nil {
		return 0, err
	}
	return len(messages), tx.Commit()
}

const unansweredLiveMessages = `m.run_id=$1 AND m.direction='to_agent' AND m.state='delivered' AND m.deliveries>0
 AND NOT EXISTS (SELECT 1 FROM agent_live_messages a WHERE a.task_id=m.task_id AND a.direction='from_agent' AND a.state='received' AND a.reply_to=m.seq)`

// UnansweredLiveMessages returns, in order, the sequence numbers of the
// messages run received (through its inbox or its prompt) that its agent has
// not replied to.
func (r *DevMachineRepository) UnansweredLiveMessages(ctx context.Context, run uuid.UUID) ([]int64, error) {
	seqs := []int64{}
	err := r.db.SelectContext(ctx, &seqs, `SELECT m.seq FROM agent_live_messages m WHERE `+unansweredLiveMessages+` ORDER BY m.seq`, run)
	return seqs, err
}

// SettleLiveMessages closes run's side of the live channel when it ends. A
// message written to its inbox after unread (shortly before the harness
// exited) may never have been read, so it is queued again for the next
// attempt's prompt instead of counting as delivered. It returns the sequence
// numbers of the messages the run did receive and never replied to.
func (r *DevMachineRepository) SettleLiveMessages(ctx context.Context, run uuid.UUID, unread time.Time) ([]int64, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	requeued := []domain.AgentLiveMessage{}
	if err = tx.SelectContext(ctx, &requeued, `UPDATE agent_live_messages m SET state='queued',delivered_via=NULL,run_id=NULL,delivered_at=NULL,workflow_node=NULL,deliveries=m.deliveries-1
 WHERE `+unansweredLiveMessages+` AND m.delivered_via='inbox' AND m.delivered_at>$2 RETURNING m.*`, run, unread.UTC()); err != nil {
		return nil, err
	}
	if err = liveMessageEvents(ctx, tx, requeued); err != nil {
		return nil, err
	}
	seqs := []int64{}
	if err = tx.SelectContext(ctx, &seqs, `SELECT m.seq FROM agent_live_messages m WHERE `+unansweredLiveMessages+` ORDER BY m.seq`, run); err != nil {
		return nil, err
	}
	return seqs, tx.Commit()
}

// recordLiveOutbox records one outbox file addressed to the orchestrator in
// the task's live channel, within its hourly budget, inside RecordTaskOutbox's
// transaction, and tells the task's conversation; an accepted question also
// notifies the creator on Telegram. It reports whether a new record was
// inserted.
func recordLiveOutbox(ctx context.Context, tx *sqlx.Tx, sender messageSender, dedupe string, message *domain.AgentOutboxMessage, body, reason string) (bool, error) {
	kind, state := domain.AgentLiveKindMessage, domain.AgentLiveReceived
	var rejected *string
	var replyTo *int64
	if message != nil {
		kind, body, replyTo = message.Kind, message.Body, message.ReplyTo
	}
	if replyTo != nil && reason == "" {
		// A reply names a message this task's agent was given.
		var known bool
		if err := tx.GetContext(ctx, &known, `SELECT EXISTS(SELECT 1 FROM agent_live_messages WHERE task_id=$1 AND direction='to_agent' AND seq=$2 AND state='delivered')`, sender.ID, *replyTo); err != nil {
			return false, err
		}
		if !known {
			reason = "reply_to does not name a message this task received"
		}
	}
	if reason != "" {
		state, rejected = domain.AgentLiveRejected, &reason
	}
	var recorded domain.AgentLiveMessage
	err := tx.GetContext(ctx, &recorded, `INSERT INTO agent_live_messages(workspace_id,task_id,direction,sender,kind,body,state,run_id,reject_reason,dedupe_key,reply_to,created_at)
 VALUES($1,$2,'from_agent','agent',$3,$4,$5,$6,$7,$8,$9,clock_timestamp())
 ON CONFLICT (task_id,dedupe_key) DO NOTHING RETURNING *`, sender.WorkspaceID, sender.ID, kind, body, state, sender.RunID, rejected, dedupe, replyTo)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = liveMessageEvents(ctx, tx, []domain.AgentLiveMessage{recorded}); err != nil {
		return false, err
	}
	if recorded.Kind == domain.AgentLiveKindQuestion && recorded.State == domain.AgentLiveReceived {
		if err = notifyLiveQuestion(ctx, tx, recorded); err != nil {
			return false, err
		}
	}
	return true, nil
}

// liveMessageOwner is the conversation that owns a task, with its issue.
type liveMessageOwner struct {
	WorkspaceID    uuid.UUID `db:"workspace_id"`
	CreatorID      uuid.UUID `db:"creator_id"`
	ConversationID uuid.UUID `db:"conversation_id"`
	Identifier     string    `db:"identifier_text"`
	Title          string    `db:"title"`
}

// ownerOfLiveTask returns the conversation of the task's creator that owns
// the task, or nil for a task without one.
func ownerOfLiveTask(ctx context.Context, tx *sqlx.Tx, task uuid.UUID) (*liveMessageOwner, error) {
	var owner liveMessageOwner
	err := tx.GetContext(ctx, &owner, `SELECT t.workspace_id,t.creator_id,t.conversation_id,i.identifier_text,i.title FROM agent_tasks t
 JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
 JOIN conversations c ON c.id=t.conversation_id AND c.workspace_id=t.workspace_id AND c.creator_id=t.creator_id WHERE t.id=$1`, task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &owner, err
}

// liveMessageEvents tells the conversation that owns each message's task
// about it (task.live_message), labelled with the issue, in the transaction
// that changed it, so open chats and task cards update live.
func liveMessageEvents(ctx context.Context, tx *sqlx.Tx, messages []domain.AgentLiveMessage) error {
	owners := map[uuid.UUID]*liveMessageOwner{}
	for _, message := range messages {
		owner, known := owners[message.TaskID]
		if !known {
			var err error
			if owner, err = ownerOfLiveTask(ctx, tx, message.TaskID); err != nil {
				return err
			}
			owners[message.TaskID] = owner
		}
		if owner == nil {
			continue
		}
		if _, err := conversationEvent(ctx, tx, owner.ConversationID, "task.live_message", domain.AgentLiveChatMessage{AgentLiveMessage: message, Identifier: owner.Identifier}); err != nil {
			return err
		}
	}
	return nil
}

// liveQuestionRunes bounds the agent's question in a Telegram notice.
const liveQuestionRunes = 1500

// notifyLiveQuestion queues a Telegram notice of an agent's question for the
// task's creator when they linked Telegram on an enabled bot, like task
// notices; the delivery worker re-checks access before sending. The question
// is untrusted agent text: it is fenced so Telegram shows it verbatim, never
// as links or formatting.
func notifyLiveQuestion(ctx context.Context, tx *sqlx.Tx, message domain.AgentLiveMessage) error {
	owner, err := ownerOfLiveTask(ctx, tx, message.TaskID)
	if err != nil || owner == nil {
		return err
	}
	var linked bool
	if err = tx.GetContext(ctx, &linked, `SELECT EXISTS(SELECT 1 FROM telegram_links l JOIN telegram_settings s ON s.workspace_id=l.workspace_id WHERE l.workspace_id=$1 AND l.user_id=$2 AND s.enabled)`, owner.WorkspaceID, owner.CreatorID); err != nil || !linked {
		return err
	}
	body := message.Body
	if utf8.RuneCountInString(body) > liveQuestionRunes {
		body = string([]rune(body)[:liveQuestionRunes]) + "…"
	}
	body = strings.ReplaceAll(body, "```", "` ` `")
	text := fmt.Sprintf("%s · %s: the coding agent asks:\n```\n%s\n```\nThis is the agent's own text. Answer it in Kuayle.", owner.Identifier, owner.Title, body)
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,text)
 SELECT l.workspace_id,l.user_id,l.telegram_user_id,$3,$4 FROM telegram_links l WHERE l.workspace_id=$1 AND l.user_id=$2`, owner.WorkspaceID, owner.CreatorID, owner.ConversationID, text)
	return err
}

// conversationAgentMessages returns what the conversation's coding agents
// sent it and Kuayle accepted, created in [from, to) (either bound optional),
// newest 200, oldest first, labelled with their issues.
func conversationAgentMessages(ctx context.Context, tx *sqlx.Tx, c domain.Conversation, from, to *time.Time) ([]domain.AgentLiveChatMessage, error) {
	messages := []domain.AgentLiveChatMessage{}
	err := tx.SelectContext(ctx, &messages, `SELECT * FROM (SELECT m.*,i.identifier_text FROM agent_live_messages m
 JOIN agent_tasks t ON t.id=m.task_id AND t.workspace_id=m.workspace_id
 JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
 WHERE t.conversation_id=$1 AND t.workspace_id=$2 AND t.creator_id=$3 AND m.direction='from_agent' AND m.state='received'
  AND ($4::timestamptz IS NULL OR m.created_at>=$4) AND ($5::timestamptz IS NULL OR m.created_at<$5)
 ORDER BY m.created_at DESC,m.id DESC LIMIT 200) a ORDER BY created_at,id`, c.ID, c.WorkspaceID, c.CreatorID, from, to)
	return messages, err
}
