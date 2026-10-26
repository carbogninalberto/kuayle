package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The queued transition already persists the exact accepted task in the same
// transaction as its request receipt. Reuse that immutable snapshot rather than
// returning a later attempt. Missing audit data fails closed, never reruns work.
func acceptedAgentTask(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, generation int64) (*domain.AgentTask, error) {
	var payload []byte
	err := tx.GetContext(ctx, &payload, `SELECT payload FROM conversation_events WHERE conversation_id=$1 AND type='task.state_changed' AND payload->>'id'=$2 AND payload->>'generation'=$3 AND payload->>'state'='queued' ORDER BY cursor LIMIT 1`, task.ConversationID, task.ID.String(), strconv.FormatInt(generation, 10))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationConflict
	}
	if err != nil {
		return nil, err
	}
	var accepted domain.AgentTask
	if json.Unmarshal(payload, &accepted) != nil || accepted.WorkspaceID != task.WorkspaceID || accepted.CreatorID != task.CreatorID || accepted.ID != task.ID || accepted.Generation != generation {
		return nil, ErrConversationConflict
	}
	return &accepted, nil
}

func replayAgentTaskCreate(ctx context.Context, tx *sqlx.Tx, c *domain.Conversation, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	var existing domain.AgentTask
	err := tx.GetContext(ctx, &existing, `SELECT * FROM agent_tasks WHERE workspace_id=$1 AND creator_id=$2 AND request_id=$3`, c.WorkspaceID, c.CreatorID, input.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existing.ConversationID == nil || *existing.ConversationID != c.ID {
		return nil, ErrConversationConflict
	}
	if c.IssueID == nil || c.TeamID == nil || *c.IssueID != existing.IssueID || *c.TeamID != existing.TeamID {
		return nil, ErrConversationNotFound
	}
	accepted, err := acceptedAgentTask(ctx, tx, &existing, 1)
	if err != nil {
		return nil, err
	}
	if accepted.Prompt != strings.TrimSpace(input.Prompt) || accepted.Provider != input.Provider || accepted.Model != input.Model || accepted.Thinking != input.Thinking {
		return nil, ErrConversationConflict
	}
	return accepted, nil
}

func replayAgentTaskRetry(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	var previous struct {
		Generation int64  `db:"expected_generation"`
		Prompt     string `db:"prompt"`
	}
	err := tx.GetContext(ctx, &previous, `SELECT expected_generation,prompt FROM agent_task_retries WHERE task_id=$1 AND request_id=$2`, task.ID, input.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if previous.Generation != input.Generation || previous.Prompt != strings.TrimSpace(input.Prompt) {
		return nil, ErrConversationConflict
	}
	return acceptedAgentTask(ctx, tx, task, previous.Generation+1)
}

// Replay checks current conversation authorization, but does not require policy
// permitting new work: this only acknowledges an already committed operation.
func (r *AgentTaskRepository) ReplayCreate(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	tx, c, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return replayAgentTaskCreate(ctx, tx, c, input)
}
func (r *AgentTaskRepository) ReplayRetry(ctx context.Context, ws, id uuid.UUID, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return replayAgentTaskRetry(ctx, tx, task, input)
}
