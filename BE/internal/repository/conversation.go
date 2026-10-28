package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrConversationBusy     = errors.New("conversation already has an active reply")
	ErrConversationConflict = errors.New("conversation request conflicts with current state")
	ErrConversationInvalid  = errors.New("invalid conversation request")
)

type ConversationRepository struct{ db *sqlx.DB }

func NewConversationRepository(db *sqlx.DB) *ConversationRepository {
	return &ConversationRepository{db}
}

func conversationActor(ctx context.Context, ws uuid.UUID) (uuid.UUID, error) {
	a, ok := domain.AccessFromContext(ctx)
	if !ok || a.UserID == uuid.Nil || a.WorkspaceID != ws {
		return uuid.Nil, ErrConversationNotFound
	}
	return a.UserID, nil
}
func conversationMember(ctx context.Context, tx *sqlx.Tx, ws, user uuid.UUID) error {
	var role string
	err := tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, user)
	if errors.Is(err, sql.ErrNoRows) || role == "guest" {
		return ErrConversationNotFound
	}
	return err
}

// Hold the conversation lock while allocating event cursors. Sequence allocation
// alone does not ensure commit order; this lock makes replay gap-free per stream.
func (r *ConversationRepository) authorized(ctx context.Context, ws, id uuid.UUID) (*sqlx.Tx, *domain.Conversation, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, nil, err
	}
	c, err := lockConversation(ctx, tx, ws, id, user, false)
	if err == nil {
		err = conversationMember(ctx, tx, ws, user)
	}
	if err == nil {
		err = conversationScope(ctx, tx, *c)
	}
	if err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConversationNotFound
		}
		return nil, nil, err
	}
	return tx, c, nil
}
func (r *ConversationRepository) Create(ctx context.Context, ws uuid.UUID, title string) (*domain.Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return nil, ErrConversationInvalid
	}
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	var c domain.Conversation
	err = tx.GetContext(ctx, &c, `INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,$3) RETURNING *`, ws, user, title)
	if err != nil {
		return nil, err
	}
	return &c, tx.Commit()
}
func (r *ConversationRepository) List(ctx context.Context, ws uuid.UUID) ([]domain.Conversation, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	result := []domain.Conversation{}
	err = tx.SelectContext(ctx, &result, `SELECT c.* FROM conversations c WHERE workspace_id=$1 AND creator_id=$2 AND `+conversationScopePredicate("c")+` ORDER BY updated_at DESC,id LIMIT 100`, ws, user)
	return result, err
}
func (r *ConversationRepository) History(ctx context.Context, ws, id uuid.UUID, before int64) (*domain.ConversationHistory, error) {
	tx, c, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	h := &domain.ConversationHistory{Conversation: *c, Messages: []domain.ConversationMessage{}}
	h.Issue, err = conversationIssue(ctx, tx, *c)
	if err != nil {
		return nil, err
	}
	err = tx.SelectContext(ctx, &h.Messages, `SELECT * FROM conversation_messages WHERE conversation_id=$1 AND ($2::bigint=0 OR sequence<$2) ORDER BY sequence DESC LIMIT 51`, id, before)
	if err != nil {
		return nil, err
	}
	h.HasMore = len(h.Messages) > 50
	if h.HasMore {
		h.Messages = h.Messages[:50]
	}
	slices.Reverse(h.Messages)
	if h.Tools, err = messageToolCalls(ctx, tx, h.Messages); err != nil {
		return nil, err
	}
	if h.Approvals, err = toolApprovals(ctx, tx, h.Tools); err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, &h.Cursor, `SELECT COALESCE(MAX(cursor),0) FROM conversation_events WHERE conversation_id=$1`, id)
	return h, err
}
func (r *ConversationRepository) Events(ctx context.Context, ws, id uuid.UUID, after int64) ([]domain.ConversationEvent, error) {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	events := []domain.ConversationEvent{}
	err = tx.SelectContext(ctx, &events, `SELECT * FROM conversation_events WHERE conversation_id=$1 AND cursor>$2 ORDER BY cursor LIMIT 200`, id, after)
	return events, err
}
func conversationEvent(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, kind string, payload any) (*domain.ConversationEvent, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var event domain.ConversationEvent
	err = tx.GetContext(ctx, &event, `INSERT INTO conversation_events(conversation_id,type,payload) VALUES($1,$2,$3) RETURNING *`, id, kind, data)
	return &event, err
}
func activeConversation(ctx context.Context, tx *sqlx.Tx, id uuid.UUID) error {
	var active bool
	err := tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE conversation_id=$1 AND status IN ('queued','streaming'))`, id)
	if err != nil {
		return err
	}
	if active {
		return ErrConversationBusy
	}
	return nil
}
func addConversationMessage(ctx context.Context, tx *sqlx.Tx, id, request uuid.UUID, parent *uuid.UUID, role, content, status, provider, model string) (*domain.ConversationMessage, error) {
	var m domain.ConversationMessage
	err := tx.GetContext(ctx, &m, `INSERT INTO conversation_messages(conversation_id,request_id,parent_id,role,content,status,provider,model) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *`, id, request, parent, role, content, status, provider, model)
	if err != nil {
		return nil, err
	}
	_, err = conversationEvent(ctx, tx, id, "message.created", m)
	if err == nil && role == "assistant" {
		// Both Web and Telegram sends use this transaction. Deliver replies to
		// linked private chats only after completion and fresh authorization.
		_, err = tx.ExecContext(ctx, `INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,message_id)
            SELECT workspace_id,user_id,telegram_user_id,conversation_id,$2 FROM telegram_links WHERE conversation_id=$1`, id, m.ID)
	}
	return &m, err
}
func (r *ConversationRepository) Send(ctx context.Context, ws, id, request uuid.UUID, content, provider, model string) (*domain.ConversationMessage, error) {
	if request == uuid.Nil || strings.TrimSpace(content) == "" || len(content) > 16384 {
		return nil, ErrConversationInvalid
	}
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing domain.ConversationMessage
	err = tx.GetContext(ctx, &existing, `SELECT * FROM conversation_messages WHERE conversation_id=$1 AND request_id=$2 AND role='user'`, id, request)
	if err == nil {
		if existing.Content != content {
			return nil, ErrConversationConflict
		}
		err = tx.GetContext(ctx, &existing, `SELECT * FROM conversation_messages WHERE conversation_id=$1 AND request_id=$2 AND role='assistant'`, id, request)
		return &existing, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err = activeConversation(ctx, tx, id); err != nil {
		return nil, err
	}
	user, err := addConversationMessage(ctx, tx, id, request, nil, "user", content, "completed", "", "")
	if err != nil {
		return nil, err
	}
	reply, err := addConversationMessage(ctx, tx, id, request, &user.ID, "assistant", "", "queued", provider, model)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	return reply, tx.Commit()
}
func (r *ConversationRepository) Retry(ctx context.Context, ws, id, message, request uuid.UUID, provider, model string) (*domain.ConversationMessage, error) {
	if request == uuid.Nil {
		return nil, ErrConversationInvalid
	}
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var previous domain.ConversationMessage
	err = tx.GetContext(ctx, &previous, `SELECT * FROM conversation_messages WHERE conversation_id=$1 AND id=$2 AND role='assistant'`, id, message)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	var existing domain.ConversationMessage
	err = tx.GetContext(ctx, &existing, `SELECT * FROM conversation_messages WHERE conversation_id=$1 AND request_id=$2 AND role='assistant'`, id, request)
	if err == nil {
		if existing.ParentID == nil || previous.ParentID == nil || *existing.ParentID != *previous.ParentID {
			return nil, ErrConversationConflict
		}
		return &existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if previous.Status != "failed" && previous.Status != "interrupted" && previous.Status != "cancelled" {
		return nil, ErrConversationConflict
	}
	// Retrying a historical request after later turns would reorder context.
	var later bool
	err = tx.GetContext(ctx, &later, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE conversation_id=$1 AND sequence>$2)`, id, previous.Sequence)
	if err != nil {
		return nil, err
	}
	if later {
		return nil, ErrConversationConflict
	}
	if err = activeConversation(ctx, tx, id); err != nil {
		return nil, err
	}
	reply, err := addConversationMessage(ctx, tx, id, request, previous.ParentID, "assistant", "", "queued", provider, model)
	if err != nil {
		return nil, err
	}
	return reply, tx.Commit()
}
func transitionConversationMessage(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, status string) error {
	var m domain.ConversationMessage
	err := tx.GetContext(ctx, &m, `UPDATE conversation_messages SET status=$2,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 RETURNING *`, id, status)
	if err != nil {
		return err
	}
	if err = failRunningToolCalls(ctx, tx, m); err != nil {
		return err
	}
	_, err = conversationEvent(ctx, tx, m.ConversationID, "message.completed", map[string]any{"message_id": m.ID, "status": m.Status})
	return err
}
func (r *ConversationRepository) Cancel(ctx context.Context, ws, id uuid.UUID) error {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var message uuid.UUID
	err = tx.GetContext(ctx, &message, `SELECT id FROM conversation_messages WHERE conversation_id=$1 AND status IN ('queued','streaming')`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = transitionConversationMessage(ctx, tx, message, "cancelled"); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *ConversationRepository) Claim(ctx context.Context) (*domain.ConversationAttempt, error) {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var a domain.ConversationAttempt
	err = tx.GetContext(ctx, &a.Conversation, `SELECT c.* FROM conversations c WHERE EXISTS(SELECT 1 FROM conversation_messages m WHERE m.conversation_id=c.id AND m.status='queued') AND EXISTS(SELECT 1 FROM workspace_members wm WHERE wm.workspace_id=c.workspace_id AND wm.user_id=c.creator_id AND wm.role<>'guest') AND `+conversationScopePredicate("c")+` ORDER BY c.updated_at,c.id LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	locked, err := lockConversation(ctx, tx, a.Conversation.WorkspaceID, a.Conversation.ID, a.Conversation.CreatorID, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Conversation = *locked
	if err = conversationMember(ctx, tx, a.Conversation.WorkspaceID, a.Conversation.CreatorID); err != nil {
		return nil, err
	}
	if err = conversationScope(ctx, tx, a.Conversation); err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, &a.Message, `UPDATE conversation_messages SET status='streaming',lease_token=gen_random_uuid(),lease_expires_at=now()+make_interval(secs=>$2),updated_at=now() WHERE conversation_id=$1 AND status='queued' RETURNING *`, a.Conversation.ID, ConversationLease.Seconds())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = conversationEvent(ctx, tx, a.Conversation.ID, "message.updated", map[string]any{"message_id": a.Message.ID, "status": "streaming"})
	if err != nil {
		return nil, err
	}
	return &a, tx.Commit()
}
func fenceConversationAttempt(ctx context.Context, tx *sqlx.Tx, a domain.ConversationAttempt) error {
	var valid bool
	err := tx.GetContext(ctx, &valid, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE id=$1 AND conversation_id=$2 AND lease_token=$3 AND lease_expires_at>now() AND status='streaming')`, a.Message.ID, a.Conversation.ID, a.Message.LeaseToken)
	if err != nil {
		return err
	}
	if !valid {
		return ErrConversationConflict
	}
	return nil
}
func (r *ConversationRepository) Append(ctx context.Context, a domain.ConversationAttempt, delta string) (*domain.ConversationEvent, error) {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE conversation_messages SET content=content||$2,updated_at=now() WHERE id=$1`, a.Message.ID, delta)
	if err != nil {
		return nil, err
	}
	event, err := conversationEvent(ctx, tx, a.Conversation.ID, "message.delta", map[string]any{"message_id": a.Message.ID, "delta": delta})
	if err != nil {
		return nil, err
	}
	return event, tx.Commit()
}
func (r *ConversationRepository) Finish(ctx context.Context, a domain.ConversationAttempt, status string) error {
	if status != "completed" && status != "failed" && status != "interrupted" {
		return ErrConversationInvalid
	}
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return err
	}
	if err = transitionConversationMessage(ctx, tx, a.Message.ID, status); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *ConversationRepository) Recover(ctx context.Context) error {
	// Never retry an ambiguous provider call automatically. A successor requires
	// an explicit request and receives its own message and idempotency key.
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := []uuid.UUID{}
	err = tx.SelectContext(ctx, &ids, `SELECT c.id FROM conversations c WHERE EXISTS(SELECT 1 FROM conversation_messages m WHERE m.conversation_id=c.id AND m.status='streaming' AND m.lease_expires_at<=now()) ORDER BY c.id LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		var message uuid.UUID
		err = tx.GetContext(ctx, &message, `SELECT id FROM conversation_messages WHERE conversation_id=$1 AND status='streaming' AND lease_expires_at<=now()`, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err = transitionConversationMessage(ctx, tx, message, "interrupted"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ConversationLease is how long a claimed attempt stays valid without renewal.
// Replies may run far longer; the live worker renews the lease while it works,
// so recovery reclaims only attempts whose worker has stopped.
const ConversationLease = 2 * time.Minute

// RenewAttempt extends a live attempt's lease. It fails for a stale token, an
// already expired lease or lost conversation access, so it never revives an
// attempt that recovery may already have interrupted.
func (r *ConversationRepository) RenewAttempt(ctx context.Context, a domain.ConversationAttempt) error {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE conversation_messages SET lease_expires_at=now()+make_interval(secs=>$4) WHERE id=$1 AND conversation_id=$2 AND lease_token=$3 AND lease_expires_at>now() AND status='streaming'`, a.Message.ID, a.Conversation.ID, a.Message.LeaseToken, ConversationLease.Seconds())
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows != 1 {
		return ErrConversationConflict
	}
	return tx.Commit()
}

func (r *ConversationRepository) CheckAttempt(ctx context.Context, a domain.ConversationAttempt) error {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fenceConversationAttempt(ctx, tx, a)
}

// GenerationContext includes only completed replies and the current user turn.
// Failed attempts remain visible in history without contaminating model context.
func (r *ConversationRepository) GenerationContext(ctx context.Context, a domain.ConversationAttempt) ([]domain.ConversationMessage, error) {
	tx, _, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	messages := []domain.ConversationMessage{}
	err = tx.SelectContext(ctx, &messages, `SELECT * FROM conversation_messages WHERE conversation_id=$1 AND sequence<$2 AND status='completed' ORDER BY sequence DESC LIMIT 50`, a.Conversation.ID, a.Message.Sequence)
	if err != nil {
		return nil, err
	}
	total, end := 0, 0
	for i, m := range messages {
		if total+len(m.Content) > 65536 {
			break
		}
		total += len(m.Content)
		end = i + 1
	}
	messages = messages[:end]
	slices.Reverse(messages)
	// Do not start the bounded window with an orphan assistant turn.
	for len(messages) > 0 && messages[0].Role != "user" {
		messages = messages[1:]
	}
	return messages, nil
}
