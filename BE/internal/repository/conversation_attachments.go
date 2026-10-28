package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrAttachmentLimit means the uploader already has the maximum number of
// pending (not yet sent) images in the conversation.
var ErrAttachmentLimit = errors.New("too many pending attachments")

const attachmentColumns = `id,workspace_id,conversation_id,message_id,uploaded_by,storage_key,filename,content_type,size,width,height,position,created_at`

func validAttachmentIDs(ids []uuid.UUID) bool {
	if len(ids) > domain.ConversationAttachmentsPerMessage {
		return false
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func validNewAttachment(a domain.NewConversationAttachment) bool {
	_, known := domain.ConversationAttachmentTypes[a.ContentType]
	return known && a.StorageKey != "" && a.Filename != "" && len(a.Filename) <= 255 &&
		a.Size > 0 && a.Size <= domain.ConversationAttachmentMaxBytes &&
		a.Width > 0 && a.Height > 0 && a.Width <= domain.ConversationAttachmentMaxDimension && a.Height <= domain.ConversationAttachmentMaxDimension
}

// CreateAttachment records an already stored upload as pending in an
// authorized conversation. The caller deletes the stored object on error.
func (r *ConversationRepository) CreateAttachment(ctx context.Context, ws, id uuid.UUID, a domain.NewConversationAttachment) (*domain.ConversationAttachment, error) {
	if !validNewAttachment(a) {
		return nil, ErrConversationInvalid
	}
	tx, c, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var pending int
	if err = tx.GetContext(ctx, &pending, `SELECT count(*) FROM conversation_attachments WHERE conversation_id=$1 AND uploaded_by=$2 AND message_id IS NULL`, id, c.CreatorID); err != nil {
		return nil, err
	}
	if pending >= domain.ConversationAttachmentPendingLimit {
		return nil, ErrAttachmentLimit
	}
	var created domain.ConversationAttachment
	err = tx.GetContext(ctx, &created, `INSERT INTO conversation_attachments(workspace_id,conversation_id,uploaded_by,storage_key,filename,content_type,size,width,height)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+attachmentColumns, ws, id, c.CreatorID, a.StorageKey, a.Filename, a.ContentType, a.Size, a.Width, a.Height)
	if err != nil {
		return nil, err
	}
	return &created, tx.Commit()
}

// Attachment returns one image of an authorized conversation. Pending images
// are visible only to their uploader.
func (r *ConversationRepository) Attachment(ctx context.Context, ws, id, attachment uuid.UUID) (*domain.ConversationAttachment, error) {
	tx, c, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var a domain.ConversationAttachment
	err = tx.GetContext(ctx, &a, `SELECT `+attachmentColumns+` FROM conversation_attachments WHERE id=$1 AND conversation_id=$2 AND workspace_id=$3 AND (message_id IS NOT NULL OR uploaded_by=$4)`, attachment, id, ws, c.CreatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	return &a, err
}

// RemovePendingAttachment discards an image removed from the composer before
// sending. Sent images stay with their message.
func (r *ConversationRepository) RemovePendingAttachment(ctx context.Context, ws, id, attachment uuid.UUID) error {
	tx, c, err := r.authorized(ctx, ws, id)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM conversation_attachments WHERE id=$1 AND conversation_id=$2 AND uploaded_by=$3 AND message_id IS NULL`, attachment, id, c.CreatorID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrConversationNotFound
	}
	return tx.Commit()
}

// bindAttachments attaches the uploader's pending images, in request order, to
// a new user message. Every ID must be pending in this conversation.
func bindAttachments(ctx context.Context, tx *sqlx.Tx, c domain.Conversation, m *domain.ConversationMessage, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	m.Attachments = []domain.ConversationAttachment{}
	for position, attachment := range ids {
		var a domain.ConversationAttachment
		err := tx.GetContext(ctx, &a, `UPDATE conversation_attachments SET message_id=$1,position=$2 WHERE id=$3 AND conversation_id=$4 AND uploaded_by=$5 AND message_id IS NULL RETURNING `+attachmentColumns, m.ID, position, attachment, c.ID, c.CreatorID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConversationInvalid
		}
		if err != nil {
			return err
		}
		m.Attachments = append(m.Attachments, a)
	}
	return nil
}

// insertMessageAttachments records images already stored for a new message,
// such as photos received from Telegram.
func insertMessageAttachments(ctx context.Context, tx *sqlx.Tx, ws, conversation, user uuid.UUID, m *domain.ConversationMessage, items []domain.NewConversationAttachment) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) > domain.ConversationAttachmentsPerMessage {
		return ErrConversationInvalid
	}
	m.Attachments = []domain.ConversationAttachment{}
	for position, item := range items {
		if !validNewAttachment(item) {
			return ErrConversationInvalid
		}
		var a domain.ConversationAttachment
		err := tx.GetContext(ctx, &a, `INSERT INTO conversation_attachments(workspace_id,conversation_id,message_id,uploaded_by,storage_key,filename,content_type,size,width,height,position)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+attachmentColumns, ws, conversation, m.ID, user, item.StorageKey, item.Filename, item.ContentType, item.Size, item.Width, item.Height, position)
		if err != nil {
			return err
		}
		m.Attachments = append(m.Attachments, a)
	}
	return nil
}

func sameMessageAttachments(ctx context.Context, tx *sqlx.Tx, message uuid.UUID, ids []uuid.UUID) (bool, error) {
	var bound []uuid.UUID
	if err := tx.SelectContext(ctx, &bound, `SELECT id FROM conversation_attachments WHERE message_id=$1 ORDER BY position`, message); err != nil {
		return false, err
	}
	return slices.Equal(bound, ids), nil
}

func fillMessageAttachments(ctx context.Context, tx *sqlx.Tx, messages []domain.ConversationMessage) error {
	ids := make([]uuid.UUID, 0, len(messages))
	for _, m := range messages {
		if m.Role == "user" {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []domain.ConversationAttachment
	if err := tx.SelectContext(ctx, &rows, `SELECT `+attachmentColumns+` FROM conversation_attachments WHERE message_id=ANY($1) ORDER BY message_id,position`, ids); err != nil {
		return err
	}
	byMessage := make(map[uuid.UUID][]domain.ConversationAttachment)
	for _, a := range rows {
		byMessage[*a.MessageID] = append(byMessage[*a.MessageID], a)
	}
	for i := range messages {
		messages[i].Attachments = byMessage[messages[i].ID]
	}
	return nil
}

// Authorize checks current access to a conversation without reading it.
func (r *ConversationRepository) Authorize(ctx context.Context, ws, id uuid.UUID) error {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return err
	}
	return tx.Rollback()
}

// IssueAttachmentAsset returns the asset a conversation image was already
// filed on the issue as, if any.
func (r *ConversationRepository) IssueAttachmentAsset(ctx context.Context, ws, id, attachment, issue uuid.UUID) (*uuid.UUID, error) {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var asset uuid.UUID
	err = tx.GetContext(ctx, &asset, `SELECT l.asset_id FROM conversation_attachment_issues l JOIN conversation_attachments a ON a.id=l.attachment_id JOIN assets s ON s.id=l.asset_id
		WHERE l.attachment_id=$1 AND l.issue_id=$2 AND a.conversation_id=$3 AND s.workspace_id=$4`, attachment, issue, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &asset, err
}

// LinkAttachmentIssue records that a sent conversation image was filed on an
// issue as the given asset.
func (r *ConversationRepository) LinkAttachmentIssue(ctx context.Context, ws, id, attachment, issue, asset uuid.UUID) error {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO conversation_attachment_issues(attachment_id,issue_id,asset_id)
		SELECT a.id,i.id,s.id FROM conversation_attachments a, issues i, assets s
		WHERE a.id=$1 AND a.conversation_id=$2 AND a.message_id IS NOT NULL AND i.id=$3 AND i.workspace_id=$4 AND s.id=$5 AND s.workspace_id=$4
		ON CONFLICT DO NOTHING`, attachment, id, issue, ws, asset)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrConversationConflict
	}
	return tx.Commit()
}

// SentAttachments lists the conversation's sent images, newest first, or only
// the requested ones.
func (r *ConversationRepository) SentAttachments(ctx context.Context, ws, id uuid.UUID, only []uuid.UUID, limit int) ([]domain.ConversationAttachment, error) {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows := []domain.ConversationAttachment{}
	err = tx.SelectContext(ctx, &rows, `SELECT a.id,a.workspace_id,a.conversation_id,a.message_id,a.uploaded_by,a.storage_key,a.filename,a.content_type,a.size,a.width,a.height,a.position,a.created_at
		FROM conversation_attachments a JOIN conversation_messages m ON m.id=a.message_id
		WHERE a.conversation_id=$1 AND a.workspace_id=$2 AND (cardinality($3::uuid[])=0 OR a.id=ANY($3))
		ORDER BY m.sequence DESC,a.position LIMIT $4`, id, ws, only, limit)
	return rows, err
}

// ExpirePendingAttachments deletes uploads that were never sent. Their
// storage keys move to the garbage list through the delete trigger.
func (r *ConversationRepository) ExpirePendingAttachments(ctx context.Context, age time.Duration, limit int) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM conversation_attachments WHERE id IN (SELECT id FROM conversation_attachments WHERE message_id IS NULL AND created_at<now()-make_interval(secs=>$1) ORDER BY created_at LIMIT $2)`, age.Seconds(), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// AttachmentGarbage lists storage keys whose attachment rows were deleted.
func (r *ConversationRepository) AttachmentGarbage(ctx context.Context, limit int) ([]string, error) {
	keys := []string{}
	err := r.db.SelectContext(ctx, &keys, `SELECT g.storage_key FROM conversation_attachment_garbage g
		WHERE NOT EXISTS(SELECT 1 FROM conversation_attachments a WHERE a.storage_key=g.storage_key)
		AND NOT EXISTS(SELECT 1 FROM agent_task_attachments t WHERE t.storage_key=g.storage_key)
		ORDER BY g.created_at LIMIT $1`, limit)
	return keys, err
}

func (r *ConversationRepository) ForgetAttachmentGarbage(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM conversation_attachment_garbage WHERE storage_key=$1`, key)
	return err
}
