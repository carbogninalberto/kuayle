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

var ErrApprovalExpired = errors.New("approval has expired")

// CreateApproval records a pending decision for a running tool call under the
// generation lease. The actor is always the conversation creator.
func (r *ConversationRepository) CreateApproval(ctx context.Context, a domain.ConversationAttempt, toolCallID uuid.UUID, action string, issueID uuid.UUID, payload json.RawMessage, ttl time.Duration) (*domain.ConversationApproval, error) {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	approval := &domain.ConversationApproval{}
	if err = tx.GetContext(ctx, approval, `INSERT INTO conversation_approvals(conversation_id,tool_call_id,action,actor_id,issue_id,payload,status,expires_at)
 SELECT $1,$2,$3,$4,$5,$6,'pending',now()+make_interval(secs=>$7) FROM conversation_tool_calls WHERE id=$2 AND message_id=$8 AND status='running' RETURNING *`,
		a.Conversation.ID, toolCallID, action, a.Conversation.CreatorID, issueID, payload, ttl.Seconds(), a.Message.ID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationConflict
	} else if err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, a.Conversation.ID, "approval.requested", approval); err != nil {
		return nil, err
	}
	return approval, tx.Commit()
}

func lockApproval(ctx context.Context, tx *sqlx.Tx, conversation, id, actor uuid.UUID) (*domain.ConversationApproval, error) {
	approval := &domain.ConversationApproval{}
	err := tx.GetContext(ctx, approval, `SELECT * FROM conversation_approvals WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, id, conversation)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	// Approvals are bound to their actor even if conversation sharing is added.
	if approval.ActorID != actor {
		return nil, ErrConversationNotFound
	}
	return approval, nil
}

func resolveApproval(ctx context.Context, tx *sqlx.Tx, approval *domain.ConversationApproval, status string, taskID *uuid.UUID, errorText *string) error {
	resolved := status != "approving"
	if err := tx.GetContext(ctx, approval, `UPDATE conversation_approvals SET status=$2,task_id=COALESCE($3,task_id),error=$4,resolved_at=CASE WHEN $5 THEN now() END,updated_at=now() WHERE id=$1 RETURNING *`, approval.ID, status, taskID, errorText, resolved); err != nil {
		return err
	}
	event := "approval.updated"
	if resolved {
		event = "approval.resolved"
	}
	_, err := conversationEvent(ctx, tx, approval.ConversationID, event, approval)
	return err
}

// ClaimApproval applies the actor's decision. Denial is final. Approval moves to
// `approving`, which the caller completes after the effect; repeating an approval
// returns the same claim so a client retry resumes rather than duplicates it.
func (r *ConversationRepository) ClaimApproval(ctx context.Context, ws, conversation, id uuid.UUID, approve bool) (*domain.ConversationApproval, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, _, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	approval, err := lockApproval(ctx, tx, conversation, id, actor)
	if err != nil {
		return nil, err
	}
	switch approval.Status {
	case "pending":
		var expired bool
		if err = tx.GetContext(ctx, &expired, `SELECT expires_at<=now() FROM conversation_approvals WHERE id=$1`, id); err != nil {
			return nil, err
		}
		target := "denied"
		if expired {
			target = "expired"
		} else if approve {
			target = "approving"
		}
		if err = resolveApproval(ctx, tx, approval, target, nil, nil); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		if expired {
			return approval, ErrApprovalExpired
		}
		return approval, nil
	case "approving", "approved":
		if approve {
			return approval, nil
		}
	case "denied":
		if !approve {
			return approval, nil
		}
	case "expired":
		return approval, ErrApprovalExpired
	}
	return nil, ErrConversationConflict
}

// FinishApproval records the effect of a claimed approval exactly once.
func (r *ConversationRepository) FinishApproval(ctx context.Context, ws, conversation, id uuid.UUID, taskID *uuid.UUID, failure string) (*domain.ConversationApproval, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, _, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	approval, err := lockApproval(ctx, tx, conversation, id, actor)
	if err != nil {
		return nil, err
	}
	if approval.Status == "approved" && taskID != nil && approval.TaskID != nil && *approval.TaskID == *taskID {
		return approval, nil
	}
	if approval.Status != "approving" {
		return nil, ErrConversationConflict
	}
	status, errorText := "approved", (*string)(nil)
	if failure != "" {
		status, errorText = "failed", &failure
	}
	if err = resolveApproval(ctx, tx, approval, status, taskID, errorText); err != nil {
		return nil, err
	}
	return approval, tx.Commit()
}

func toolApprovals(ctx context.Context, tx *sqlx.Tx, calls []domain.ConversationToolCall) ([]domain.ConversationApproval, error) {
	approvals := []domain.ConversationApproval{}
	if len(calls) == 0 {
		return approvals, nil
	}
	ids := make([]uuid.UUID, len(calls))
	for i, call := range calls {
		ids[i] = call.ID
	}
	query, args, err := sqlx.In(`SELECT * FROM conversation_approvals WHERE tool_call_id IN (?) ORDER BY created_at`, ids)
	if err != nil {
		return nil, err
	}
	err = tx.SelectContext(ctx, &approvals, tx.Rebind(query), args...)
	return approvals, err
}
