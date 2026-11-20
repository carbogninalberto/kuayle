package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
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
	approval.MessageModel = a.Message.Model
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

// resolveApproval moves an approval to approving or a final status, which
// ends a merge's wait (next_check_at).
func resolveApproval(ctx context.Context, tx *sqlx.Tx, approval *domain.ConversationApproval, status string, taskID *uuid.UUID, errorText *string) error {
	resolved := status != "approving"
	if err := tx.GetContext(ctx, approval, `UPDATE conversation_approvals SET status=$2,task_id=COALESCE($3,task_id),error=$4,resolved_at=CASE WHEN $5 THEN now() END,next_check_at=NULL,updated_at=now() WHERE id=$1 RETURNING *`, approval.ID, status, taskID, errorText, resolved); err != nil {
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
	case "approving", domain.ApprovalWaitingChecks, "approved":
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
	// A merge waiting for the checks of its updated branch finishes too.
	if approval.Status != "approving" && approval.Status != domain.ApprovalWaitingChecks {
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

// The initial page also carries outstanding decisions from older history.
func toolApprovals(ctx context.Context, tx *sqlx.Tx, conversation uuid.UUID, calls []domain.ConversationToolCall, includePending bool) ([]domain.ConversationApproval, error) {
	ids := make([]uuid.UUID, len(calls))
	for i, call := range calls {
		ids[i] = call.ID
	}
	approvals := []domain.ConversationApproval{}
	err := tx.SelectContext(ctx, &approvals, `SELECT a.*,m.model AS message_model FROM conversation_approvals a
		JOIN conversation_tool_calls t ON t.id=a.tool_call_id
		JOIN conversation_messages m ON m.id=t.message_id
		WHERE a.conversation_id=$1 AND (a.tool_call_id=ANY($2) OR ($3 AND a.status='pending'))
		ORDER BY a.created_at,a.id`, conversation, pq.Array(ids), includePending)
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

// WaitApproval moves a claimed merge (`approving`) to waiting_checks with the
// payload holding its wait, to be read again at next. Like FinishApproval it is
// fenced by the actor in ctx and the conversation's authorization.
func (r *ConversationRepository) WaitApproval(ctx context.Context, ws, conversation, id uuid.UUID, payload json.RawMessage, next time.Time) (*domain.ConversationApproval, error) {
	return r.recordWait(ctx, ws, conversation, id, payload, next, "approving", true)
}

// UpdateApprovalWait stores a waiting merge's progress and when to read the
// pull request again. Its clients are told only when the payload changed.
func (r *ConversationRepository) UpdateApprovalWait(ctx context.Context, ws, conversation, id uuid.UUID, payload json.RawMessage, next time.Time, changed bool) (*domain.ConversationApproval, error) {
	return r.recordWait(ctx, ws, conversation, id, payload, next, domain.ApprovalWaitingChecks, changed)
}

func (r *ConversationRepository) recordWait(ctx context.Context, ws, conversation, id uuid.UUID, payload json.RawMessage, next time.Time, from string, announce bool) (*domain.ConversationApproval, error) {
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
	if approval.Status != from || approval.Action != "merge_pull_request" {
		return nil, ErrConversationConflict
	}
	if err = tx.GetContext(ctx, approval, `UPDATE conversation_approvals SET status=$2,payload=$3,next_check_at=$4,updated_at=now() WHERE id=$1 RETURNING *`, id, domain.ApprovalWaitingChecks, payload, next); err != nil {
		return nil, err
	}
	if announce {
		if _, err = conversationEvent(ctx, tx, approval.ConversationID, "approval.updated", approval); err != nil {
			return nil, err
		}
	}
	return approval, tx.Commit()
}

// ClaimMergeWaits claims up to limit waiting merges whose next check is due,
// oldest first, skipping rows another process holds, and pushes their next
// check by lease, so a process that stops mid-check leaves them to be claimed
// again then. Each is then continued as its actor, through the conversation's
// authorization.
func (r *ConversationRepository) ClaimMergeWaits(ctx context.Context, limit int, lease time.Duration) ([]ClaimedApproval, error) {
	claimed := []ClaimedApproval{}
	err := r.db.SelectContext(ctx, &claimed, `WITH due AS (
 SELECT id FROM conversation_approvals WHERE status='waiting_checks' AND next_check_at<=now() ORDER BY next_check_at LIMIT $1 FOR UPDATE SKIP LOCKED)
 UPDATE conversation_approvals a SET next_check_at=now()+make_interval(secs=>$2) FROM due, conversations c
 WHERE a.id=due.id AND c.id=a.conversation_id
 RETURNING a.id,a.conversation_id,c.workspace_id,a.actor_id,a.issue_id`, limit, lease.Seconds())
	return claimed, err
}

// ClaimedApproval names an approval whose effect was claimed but not finished.
type ClaimedApproval struct {
	ID             uuid.UUID `db:"id"`
	ConversationID uuid.UUID `db:"conversation_id"`
	WorkspaceID    uuid.UUID `db:"workspace_id"`
	ActorID        uuid.UUID `db:"actor_id"`
	// IssueID is set by ClaimMergeWaits.
	IssueID uuid.UUID `db:"issue_id"`
}

// ClaimedApprovals lists every approval of an action still `approving`, so a
// restarted server can resume their effect as their actor. Resuming goes
// through the usual claim, which re-checks access.
func (r *ConversationRepository) ClaimedApprovals(ctx context.Context, action string) ([]ClaimedApproval, error) {
	claimed := []ClaimedApproval{}
	err := r.db.SelectContext(ctx, &claimed, `SELECT a.id,a.conversation_id,c.workspace_id,a.actor_id FROM conversation_approvals a JOIN conversations c ON c.id=a.conversation_id WHERE a.action=$1 AND a.status='approving' ORDER BY a.updated_at`, action)
	return claimed, err
}

// LockApprovalEffect takes a PostgreSQL session lock on an approval's effect,
// so a single server process runs it. The lock lives on a connection of its
// own until unlock is called, or until the process dies and the database
// ends the session. held is false when another process holds the lock.
func (r *ConversationRepository) LockApprovalEffect(ctx context.Context, id uuid.UUID) (unlock func(), held bool, err error) {
	conn, err := r.db.Connx(ctx)
	if err != nil {
		return nil, false, err
	}
	// The connection is closed rather than returned to the pool, so a lock
	// whose unlock failed cannot stay with a pooled session.
	discard := func() {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}
	if err = conn.GetContext(ctx, &held, `SELECT pg_try_advisory_lock(hashtextextended('approval-effect:'||$1::text,0))`, id); err != nil || !held {
		discard()
		return nil, false, err
	}
	return func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended('approval-effect:'||$1::text,0))`, id)
		discard()
	}, true, nil
}

// ApprovalState reads an approval's status and payload as stored now.
func (r *ConversationRepository) ApprovalState(ctx context.Context, id uuid.UUID) (string, json.RawMessage, error) {
	var row struct {
		Status  string          `db:"status"`
		Payload json.RawMessage `db:"payload"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT status,payload FROM conversation_approvals WHERE id=$1`, id)
	return row.Status, row.Payload, err
}
