package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const taskDecisionList = `(SELECT COALESCE(jsonb_agg(to_jsonb(d)-'task_id' ORDER BY d.seq,d.kind),'[]') FROM task_owner_decisions d WHERE d.task_id=t.id)`
const taskDecisionCount = `(SELECT count(*) FROM task_owner_decisions d WHERE d.task_id=t.id)`

func decisionEvent(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	var decisions domain.OwnerDecisions
	if err := tx.GetContext(ctx, &decisions, `SELECT `+taskDecisionList+` FROM agent_tasks t WHERE t.id=$1`, task.ID); err != nil {
		return err
	}
	_, err := conversationEvent(ctx, tx, *task.ConversationID, "task.decisions_updated", map[string]any{"task_id": task.ID, "pending_decisions": decisions})
	return err
}

// AgentQueueItem is one of the actor's tasks for the cross-project queue.
type AgentQueueItem struct {
	TaskTitle            string    `json:"task_title,omitempty" db:"task_title"`
	PendingDecisionCount int       `json:"pending_decision_count" db:"pending_decision_count"`
	ID                   uuid.UUID `json:"id" db:"id"`
	ConversationID       uuid.UUID `json:"conversation_id" db:"conversation_id"`
	State                string    `json:"state" db:"state"`
	Reason               string    `json:"reason" db:"reason"`
	IssueIdentifier      string    `json:"issue_identifier" db:"identifier_text"`
	IssueTitle           string    `json:"issue_title" db:"title"`
	Provider             string    `json:"provider" db:"provider"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
	// The task's machine and worktree, for opening a terminal on its work.
	MachineID   *uuid.UUID `json:"machine_id,omitempty" db:"machine_id"`
	MachineName *string    `json:"machine_name,omitempty" db:"machine_name"`
	CheckoutID  *uuid.UUID `json:"checkout_id,omitempty" db:"checkout_id"`
}

// ownTaskRows and ownTaskVisible are the tasks Queue shows the actor ($2) in
// workspace $1: their own implementing tasks, from their own issue
// conversations whose team they can still see. Board reuses them, so the
// two never differ.
const ownTaskRows = `agent_tasks t JOIN conversations c ON c.id=t.conversation_id AND c.workspace_id=t.workspace_id JOIN issues i ON i.id=c.issue_id`

func ownTaskVisible() string {
	return `t.workspace_id=$1 AND t.kind='implement' AND t.creator_id=$2 AND c.creator_id=$2 AND ` + conversationScopePredicate("c")
}

// Queue lists the actor's most recent tasks across their issue conversations,
// re-checking membership and each issue's current team visibility, exactly as
// opening the conversation would.
func (r *AgentTaskRepository) Queue(ctx context.Context, ws uuid.UUID) ([]AgentQueueItem, error) {
	return r.queue(ctx, ws, false, "")
}
func (r *AgentTaskRepository) NeedsMe(ctx context.Context, ws uuid.UUID, issueIdentifier string) ([]AgentQueueItem, error) {
	return r.queue(ctx, ws, true, issueIdentifier)
}
func (r *AgentTaskRepository) queue(ctx context.Context, ws uuid.UUID, needsMe bool, issueIdentifier string) ([]AgentQueueItem, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	tail := " ORDER BY t.updated_at DESC,t.id LIMIT 50"
	if needsMe {
		tail = " AND counts.pending_decision_count>0 ORDER BY t.updated_at DESC,t.id"
	}
	items := []AgentQueueItem{}
	err = tx.SelectContext(ctx, &items, `SELECT left(split_part(t.prompt,E'\n',1),160) AS task_title,counts.pending_decision_count,t.id,t.conversation_id,t.state,t.reason,t.provider,t.updated_at,i.identifier_text,i.title,t.machine_id,m.name AS machine_name,
 CASE WHEN t.machine_id IS NOT NULL THEN t.checkout_id END AS checkout_id
 FROM `+ownTaskRows+`
 LEFT JOIN dev_machines m ON m.id=t.machine_id AND m.workspace_id=t.workspace_id AND m.status<>'destroyed'
 CROSS JOIN LATERAL (SELECT `+taskDecisionCount+` AS pending_decision_count) counts
 WHERE `+ownTaskVisible()+` AND ($3='' OR i.identifier_text=$3)`+tail, ws, actor, issueIdentifier)
	return items, err
}

// MachineTasks lists the tasks that used one of the actor's machines, active
// ones first and then the most recently updated, with the same membership,
// creator and issue visibility checks as Queue. Only the machine's owner may
// list them; any other caller gets ErrAgentTaskUnauthorized.
func (r *AgentTaskRepository) MachineTasks(ctx context.Context, ws, machine uuid.UUID) ([]AgentQueueItem, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	var owned bool
	if err = tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM dev_machines WHERE workspace_id=$1 AND id=$2 AND created_by_user_id=$3)`, ws, machine, actor); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrAgentTaskUnauthorized
	}
	items := []AgentQueueItem{}
	err = tx.SelectContext(ctx, &items, `SELECT t.id,t.conversation_id,t.state,t.reason,t.provider,t.updated_at,i.identifier_text,i.title,t.machine_id,m.name AS machine_name,t.checkout_id
 FROM agent_tasks t JOIN conversations c ON c.id=t.conversation_id AND c.workspace_id=t.workspace_id JOIN issues i ON i.id=c.issue_id
 JOIN dev_machines m ON m.id=t.machine_id AND m.workspace_id=t.workspace_id
 WHERE t.workspace_id=$1 AND t.machine_id=$2 AND t.creator_id=$3 AND c.creator_id=$3 AND `+conversationScopePredicate("c")+`
 ORDER BY (t.state NOT IN ('completed','failed','cancelled','blocked','awaiting_review')) DESC,t.updated_at DESC,t.id LIMIT 30`, ws, machine, actor)
	return items, err
}
