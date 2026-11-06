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
 SELECT $1,$2,$3,$4,NULLIF($5::uuid,'00000000-0000-0000-0000-000000000000'::uuid),$6,'pending',now()+make_interval(secs=>$7) FROM conversation_tool_calls WHERE id=$2 AND message_id=$8 AND status='running' RETURNING *`,
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
// The boolean reports whether this call changed the approval's status, so a
// repeated decision is not recorded twice.
func (r *ConversationRepository) ClaimApproval(ctx context.Context, ws, conversation, id uuid.UUID, approve bool) (*domain.ConversationApproval, bool, error) {
	return r.ClaimApprovalEdited(ctx, ws, conversation, id, approve, nil)
}

// ClaimApprovalEdited is ClaimApproval with the approver's edit of the
// payload: when a pending approval is approved, edit returns the payload to
// store with the claim, under the same lock, so the effect uses exactly what
// was approved. An error from edit leaves the approval pending. An approval
// already claimed keeps its payload, so a retry resumes what was approved.
func (r *ConversationRepository) ClaimApprovalEdited(ctx context.Context, ws, conversation, id uuid.UUID, approve bool, edit func(*domain.ConversationApproval) (json.RawMessage, error)) (*domain.ConversationApproval, bool, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, false, err
	}
	tx, _, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	approval, err := lockApproval(ctx, tx, conversation, id, actor)
	if err != nil {
		return nil, false, err
	}
	switch approval.Status {
	case "pending":
		var expired bool
		if err = tx.GetContext(ctx, &expired, `SELECT expires_at<=now() FROM conversation_approvals WHERE id=$1`, id); err != nil {
			return nil, false, err
		}
		target := "denied"
		if expired {
			target = "expired"
		} else if approve {
			target = "approving"
		}
		if target == "approving" && edit != nil {
			payload, err := edit(approval)
			if err != nil {
				return nil, false, err
			}
			if err = tx.GetContext(ctx, approval, `UPDATE conversation_approvals SET payload=$2,updated_at=now() WHERE id=$1 RETURNING *`, id, payload); err != nil {
				return nil, false, err
			}
		}
		if err = resolveApproval(ctx, tx, approval, target, nil, nil); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		if expired {
			return approval, true, ErrApprovalExpired
		}
		return approval, true, nil
	case "approving", "approved":
		if approve {
			return approval, false, nil
		}
	case "denied":
		if !approve {
			return approval, false, nil
		}
	case "expired":
		return approval, false, ErrApprovalExpired
	}
	return nil, false, ErrConversationConflict
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
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if flag, ok := ctx.Value(finishTransitionKey{}).(*bool); ok {
		*flag = true
	}
	return approval, nil
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

type finishTransitionKey struct{}

// WithFinishTransition returns a context whose FinishApproval reports through
// the returned flag whether it moved the approval out of `approving`, as opposed
// to finding it already finished. Callers use it to record an outcome once.
func WithFinishTransition(ctx context.Context) (context.Context, *bool) {
	flag := new(bool)
	return context.WithValue(ctx, finishTransitionKey{}, flag), flag
}

// RecordApprovalProgress stores the progress of a claimed approval's effect in
// its payload and tells the conversation's clients. Only an approval still
// `approving` and bound to the actor in ctx can be updated.
func (r *ConversationRepository) RecordApprovalProgress(ctx context.Context, ws, conversation, id uuid.UUID, payload json.RawMessage) (*domain.ConversationApproval, error) {
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
	if approval.Status != "approving" {
		return nil, ErrConversationConflict
	}
	if err = tx.GetContext(ctx, approval, `UPDATE conversation_approvals SET payload=$2,updated_at=now() WHERE id=$1 RETURNING *`, id, payload); err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, approval.ConversationID, "approval.updated", approval); err != nil {
		return nil, err
	}
	return approval, tx.Commit()
}

// ClaimedApproval names an approval whose effect was claimed but not finished.
type ClaimedApproval struct {
	ID             uuid.UUID `db:"id"`
	ConversationID uuid.UUID `db:"conversation_id"`
	WorkspaceID    uuid.UUID `db:"workspace_id"`
	ActorID        uuid.UUID `db:"actor_id"`
}

// ClaimedApprovals lists the approvals of an action still `approving`, so a
// restarted server can resume their effect as their actor. Resuming goes
// through the usual claim, which re-checks access.
func (r *ConversationRepository) ClaimedApprovals(ctx context.Context, action string) ([]ClaimedApproval, error) {
	claimed := []ClaimedApproval{}
	err := r.db.SelectContext(ctx, &claimed, `SELECT a.id,a.conversation_id,c.workspace_id,a.actor_id FROM conversation_approvals a JOIN conversations c ON c.id=a.conversation_id WHERE a.action=$1 AND a.status='approving' ORDER BY a.updated_at LIMIT 100`, action)
	return claimed, err
}
