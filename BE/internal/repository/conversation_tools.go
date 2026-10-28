package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// StartToolCall records a tool invocation before it executes, under the
// generation lease, so a stale or revoked attempt cannot act.
func (r *ConversationRepository) StartToolCall(ctx context.Context, a domain.ConversationAttempt, sequence int, callID, name string, input json.RawMessage) (*domain.ConversationToolCall, error) {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	call := &domain.ConversationToolCall{}
	if err = tx.GetContext(ctx, call, `INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,$3,$4,$5,$6,'running') RETURNING *`, a.Conversation.ID, a.Message.ID, sequence, callID, name, input); err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, a.Conversation.ID, "tool.started", call); err != nil {
		return nil, err
	}
	return call, tx.Commit()
}

// FinishToolCall records the outcome of a running call exactly once.
func (r *ConversationRepository) FinishToolCall(ctx context.Context, a domain.ConversationAttempt, id uuid.UUID, output json.RawMessage, errorText string, issueID *uuid.UUID) (*domain.ConversationToolCall, error) {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	status := "completed"
	var outputValue, errorValue any
	if errorText != "" {
		status, errorValue = "failed", errorText
	} else {
		outputValue = output
	}
	call := &domain.ConversationToolCall{}
	if err = tx.GetContext(ctx, call, `UPDATE conversation_tool_calls SET status=$3,output=$4,error=$5,issue_id=$6,completed_at=now() WHERE id=$1 AND message_id=$2 AND status='running' RETURNING *`, id, a.Message.ID, status, outputValue, errorValue, issueID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationConflict
	} else if err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, a.Conversation.ID, "tool.completed", call); err != nil {
		return nil, err
	}
	return call, tx.Commit()
}

// A call still running when its message ends never learned its outcome. It is
// reported as failed with an explicit unknown outcome and never re-executed.
func failRunningToolCalls(ctx context.Context, tx *sqlx.Tx, m domain.ConversationMessage) error {
	calls := []domain.ConversationToolCall{}
	if err := tx.SelectContext(ctx, &calls, `UPDATE conversation_tool_calls SET status='failed',error='Interrupted before the outcome was recorded; check before repeating',completed_at=now() WHERE message_id=$1 AND status='running' RETURNING *`, m.ID); err != nil {
		return err
	}
	for i := range calls {
		if _, err := conversationEvent(ctx, tx, m.ConversationID, "tool.completed", calls[i]); err != nil {
			return err
		}
	}
	return nil
}

func messageToolCalls(ctx context.Context, tx *sqlx.Tx, messages []domain.ConversationMessage) ([]domain.ConversationToolCall, error) {
	calls := []domain.ConversationToolCall{}
	if len(messages) == 0 {
		return calls, nil
	}
	ids := make([]uuid.UUID, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}
	query, args, err := sqlx.In(`SELECT * FROM conversation_tool_calls WHERE message_id IN (?) ORDER BY message_id,sequence`, ids)
	if err != nil {
		return nil, err
	}
	err = tx.SelectContext(ctx, &calls, tx.Rebind(query), args...)
	return calls, err
}
