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
// task is completed or cancelled. The task row lock orders sequence numbers.
func (r *AgentTaskRepository) QueueLiveMessage(ctx context.Context, ws, id uuid.UUID, sender, body string) (*domain.AgentLiveMessage, error) {
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
	if task.State == "completed" || task.State == "cancelled" || task.State == "cancelling" {
		return nil, ErrAgentLiveMessageClosed
	}
	var sent int
	if err = tx.GetContext(ctx, &sent, `SELECT count(*) FROM agent_live_messages WHERE task_id=$1 AND direction='to_agent' AND created_at>now()-interval '1 hour'`, id); err != nil {
		return nil, err
	}
	if sent >= domain.AgentLiveMessagesPerHour {
		return nil, ErrAgentLiveMessageRateLimited
	}
	var message domain.AgentLiveMessage
	err = tx.GetContext(ctx, &message, `INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,author_id,body,state)
 SELECT $1,$2,'to_agent',COALESCE(max(seq),0)+1,$3,$4,$5,'queued' FROM agent_live_messages WHERE task_id=$2 RETURNING *`, ws, id, sender, actor, body)
	if err != nil {
		return nil, err
	}
	if err = liveMessageEvents(ctx, tx, []domain.AgentLiveMessage{message}); err != nil {
		return nil, err
	}
	return &message, tx.Commit()
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
// created are claimed again.
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
	candidates := []domain.AgentLiveMessage{}
	if err = tx.SelectContext(ctx, &candidates, `SELECT * FROM agent_live_messages m WHERE m.task_id=$1 AND m.workspace_id=$2 AND (m.state='queued'
  OR (m.state='delivered' AND m.delivered_via='prompt' AND NOT EXISTS (SELECT 1 FROM dev_machine_agent_runs rn WHERE rn.id=m.run_id) AND NOT EXISTS (SELECT 1 FROM agent_tasks tk WHERE tk.run_id=m.run_id)))
 ORDER BY m.seq LIMIT $3 FOR UPDATE OF m SKIP LOCKED`, task.ID, task.WorkspaceID, liveMessagesPerDelivery); err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for _, candidate := range candidates {
		cost := len(candidate.Body) + 120
		if cost > room {
			break
		}
		room -= cost
		ids = append(ids, candidate.ID)
	}
	if len(ids) == 0 {
		return messages, nil
	}
	if err = tx.SelectContext(ctx, &messages, `UPDATE agent_live_messages SET state='delivered',delivered_via='prompt',run_id=$2,delivered_at=now() WHERE id=ANY($1) RETURNING *`, pq.Array(ids), task.RunID); err != nil {
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
	err = tx.SelectContext(ctx, &messages, `UPDATE agent_live_messages m SET state='delivered',delivered_via='inbox',run_id=$2,delivered_at=now()
 FROM agent_tasks t WHERE t.id=m.task_id AND t.run_id=$2 AND m.id=ANY($1) AND m.state='queued' RETURNING m.*`, pq.Array(ids), run)
	if err != nil {
		return 0, err
	}
	if err = liveMessageEvents(ctx, tx, messages); err != nil {
		return 0, err
	}
	return len(messages), tx.Commit()
}

// recordLiveOutbox records one outbox file addressed to the orchestrator in
// the task's live channel, within its hourly budget, inside RecordTaskOutbox's
// transaction, and tells the task's conversation; an accepted question also
// notifies the creator on Telegram. It reports whether a new record was
// inserted.
func recordLiveOutbox(ctx context.Context, tx *sqlx.Tx, sender messageSender, dedupe string, message *domain.AgentOutboxMessage, body, reason string) (bool, error) {
	kind, state := domain.AgentLiveKindMessage, domain.AgentLiveReceived
	var rejected *string
	if message != nil {
		kind, body = message.Kind, message.Body
	}
	if reason != "" {
		state, rejected = domain.AgentLiveRejected, &reason
	}
	var recorded domain.AgentLiveMessage
	err := tx.GetContext(ctx, &recorded, `INSERT INTO agent_live_messages(workspace_id,task_id,direction,sender,kind,body,state,run_id,reject_reason,dedupe_key,created_at)
 VALUES($1,$2,'from_agent','agent',$3,$4,$5,$6,$7,$8,clock_timestamp())
 ON CONFLICT (task_id,dedupe_key) DO NOTHING RETURNING *`, sender.WorkspaceID, sender.ID, kind, body, state, sender.RunID, rejected, dedupe)
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
