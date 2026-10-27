package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

func folderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) {
		return "", ErrConversationInvalid
	}
	return name, nil
}

// Folders lists the actor's own folders.
func (r *ConversationRepository) Folders(ctx context.Context, ws uuid.UUID) ([]domain.ConversationFolder, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	folders := []domain.ConversationFolder{}
	err = r.db.SelectContext(ctx, &folders, `SELECT f.* FROM conversation_folders f JOIN workspace_members m ON m.workspace_id=f.workspace_id AND m.user_id=f.owner_id
 WHERE f.workspace_id=$1 AND f.owner_id=$2 ORDER BY lower(f.name), f.id`, ws, user)
	return folders, err
}

func (r *ConversationRepository) CreateFolder(ctx context.Context, ws uuid.UUID, name string) (*domain.ConversationFolder, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	if name, err = folderName(name); err != nil {
		return nil, err
	}
	folder := &domain.ConversationFolder{}
	err = r.db.GetContext(ctx, folder, `INSERT INTO conversation_folders(workspace_id,owner_id,name)
 SELECT $1,$2,$3 WHERE EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2) RETURNING *`, ws, user, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	return folder, err
}

func (r *ConversationRepository) RenameFolder(ctx context.Context, ws, id uuid.UUID, name string) (*domain.ConversationFolder, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	if name, err = folderName(name); err != nil {
		return nil, err
	}
	folder := &domain.ConversationFolder{}
	err = r.db.GetContext(ctx, folder, `UPDATE conversation_folders SET name=$4,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND owner_id=$3 RETURNING *`, id, ws, user, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	return folder, err
}

// DeleteFolder removes a folder; its conversations move back to the root.
func (r *ConversationRepository) DeleteFolder(ctx context.Context, ws, id uuid.UUID) error {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM conversation_folders WHERE id=$1 AND workspace_id=$2 AND owner_id=$3`, id, ws, user)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrConversationNotFound
	}
	return nil
}

// MoveConversation puts the actor's conversation in one of their folders, or
// back at the root when folder is nil.
func (r *ConversationRepository) MoveConversation(ctx context.Context, ws, id uuid.UUID, folder *uuid.UUID) (*domain.Conversation, error) {
	tx, c, err := r.authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if folder != nil {
		var owned bool
		if err = tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM conversation_folders WHERE id=$1 AND workspace_id=$2 AND owner_id=$3)`, *folder, ws, c.CreatorID); err != nil {
			return nil, err
		}
		if !owned {
			return nil, ErrConversationNotFound
		}
	}
	if err = tx.GetContext(ctx, c, `UPDATE conversations SET folder_id=$2 WHERE id=$1 RETURNING *`, id, folder); err != nil {
		return nil, err
	}
	return c, tx.Commit()
}

// Delete removes the actor's conversation and its messages. A reply in
// progress or active agent work must finish first; finished agent tasks keep
// their history without the conversation.
func (r *ConversationRepository) Delete(ctx context.Context, ws, id uuid.UUID) error {
	tx, _, err := r.authorized(ctx, ws, id)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var busy, active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE conversation_id=$1 AND status IN ('queued','streaming')),
 EXISTS(SELECT 1 FROM agent_tasks WHERE conversation_id=$1 AND state NOT IN ('completed','cancelled','failed'))`, id).Scan(&busy, &active); err != nil {
		return err
	}
	if busy {
		return ErrConversationBusy
	}
	if active {
		return ErrConversationConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM conversations WHERE id=$1 AND workspace_id=$2`, id, ws); err != nil {
		return err
	}
	return tx.Commit()
}
