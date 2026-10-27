package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AgentQueueItem is one of the actor's tasks for the cross-project queue.
type AgentQueueItem struct {
	ID              uuid.UUID `json:"id" db:"id"`
	ConversationID  uuid.UUID `json:"conversation_id" db:"conversation_id"`
	State           string    `json:"state" db:"state"`
	Reason          string    `json:"reason" db:"reason"`
	IssueIdentifier string    `json:"issue_identifier" db:"identifier_text"`
	IssueTitle      string    `json:"issue_title" db:"title"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// Queue lists the actor's most recent tasks across their issue conversations,
// re-checking membership and each issue's current team visibility, exactly as
// opening the conversation would.
func (r *AgentTaskRepository) Queue(ctx context.Context, ws uuid.UUID) ([]AgentQueueItem, error) {
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
	items := []AgentQueueItem{}
	err = tx.SelectContext(ctx, &items, `SELECT t.id,t.conversation_id,t.state,t.reason,t.updated_at,i.identifier_text,i.title
 FROM agent_tasks t JOIN conversations c ON c.id=t.conversation_id AND c.workspace_id=t.workspace_id JOIN issues i ON i.id=c.issue_id
 WHERE t.workspace_id=$1 AND t.creator_id=$2 AND c.creator_id=$2 AND `+conversationScopePredicate("c")+`
 ORDER BY t.updated_at DESC,t.id LIMIT 50`, ws, actor)
	return items, err
}
