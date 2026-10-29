package repository

import (
	"context"
	"errors"
	"sort"

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
	result, err := r.db.ExecContext(ctx, `UPDATE agent_live_messages m SET state='delivered',delivered_via='inbox',run_id=$2,delivered_at=now()
 FROM agent_tasks t WHERE t.id=m.task_id AND t.run_id=$2 AND m.id=ANY($1) AND m.state='queued'`, pq.Array(ids), run)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

// recordLiveOutbox records one outbox file addressed to the orchestrator in
// the task's live channel, within its hourly budget, inside RecordTaskOutbox's
// transaction. It reports whether a new record was inserted.
func recordLiveOutbox(ctx context.Context, tx *sqlx.Tx, sender messageSender, dedupe string, message *domain.AgentOutboxMessage, body, reason string) (bool, error) {
	row := map[string]any{"workspace_id": sender.WorkspaceID, "task_id": sender.ID, "run_id": sender.RunID, "dedupe_key": dedupe, "kind": domain.AgentLiveKindMessage, "body": body, "state": domain.AgentLiveReceived, "reject_reason": nil}
	if message != nil {
		row["kind"], row["body"] = message.Kind, message.Body
	}
	if reason != "" {
		row["state"], row["reject_reason"] = domain.AgentLiveRejected, reason
	}
	result, err := tx.NamedExecContext(ctx, `INSERT INTO agent_live_messages(workspace_id,task_id,direction,sender,kind,body,state,run_id,reject_reason,dedupe_key,created_at)
 VALUES(:workspace_id,:task_id,'from_agent','agent',:kind,:body,:state,:run_id,:reject_reason,:dedupe_key,clock_timestamp())
 ON CONFLICT (task_id,dedupe_key) DO NOTHING`, row)
	if err != nil {
		return false, err
	}
	inserted, _ := result.RowsAffected()
	return inserted > 0, nil
}
