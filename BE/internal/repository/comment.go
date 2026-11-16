package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type CommentRepository struct {
	db *sqlx.DB
}

func NewCommentRepository(db *sqlx.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

// liveComment is true for a comment that is not soft-deleted and is not a
// reply in a deleted thread. Replies are reached through their parent, so
// deleting a comment hides its replies until it is restored.
func liveComment(alias string) string {
	return "(" + alias + ".deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM comments deleted_parent WHERE deleted_parent.id = " + alias + ".parent_id AND deleted_parent.deleted_at IS NOT NULL))"
}

func (r *CommentRepository) Create(ctx context.Context, comment *domain.Comment) error {
	query := `INSERT INTO comments (id, issue_id, user_id, body, parent_id) SELECT $1, $2, $3, $4, $5 WHERE ` + issueVisible(ctx, "$2") + ` AND (CAST($5 AS uuid) IS NULL OR EXISTS (SELECT 1 FROM comments parent_comment WHERE parent_comment.id=$5 AND parent_comment.issue_id=$2 AND ` + liveComment("parent_comment") + `)) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, comment.ID, comment.IssueID, comment.UserID, comment.Body, comment.ParentID).Scan(&comment.CreatedAt, &comment.UpdatedAt)
}

func (r *CommentRepository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error) {
	var comments []domain.Comment
	err := r.db.SelectContext(ctx, &comments, `SELECT * FROM comments WHERE issue_id = $1 AND parent_id IS NULL AND deleted_at IS NULL AND `+issueVisible(ctx, "comments.issue_id")+` ORDER BY created_at ASC`, issueID)
	return comments, err
}

func (r *CommentRepository) ListReplies(ctx context.Context, parentID uuid.UUID) ([]domain.Comment, error) {
	var comments []domain.Comment
	err := r.db.SelectContext(ctx, &comments, `SELECT * FROM comments WHERE parent_id = $1 AND `+liveComment("comments")+` AND `+issueVisible(ctx, "comments.issue_id")+` ORDER BY created_at ASC`, parentID)
	return comments, err
}

func (r *CommentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.GetContext(ctx, &comment, `SELECT * FROM comments WHERE id = $1 AND `+liveComment("comments")+` AND `+issueVisible(ctx, "comments.issue_id"), id)
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

// Resolve marks an open comment resolved and returns it, or returns nil when
// it was already resolved or is not visible.
func (r *CommentRepository) Resolve(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	return r.changed(ctx, `UPDATE comments SET resolved_at = NOW() WHERE id = $1 AND resolved_at IS NULL AND `+liveComment("comments")+` AND `+issueVisible(ctx, "comments.issue_id")+` RETURNING *`, id)
}

// Reopen clears a resolved comment's resolution and returns it, or returns nil
// when it was open or is not visible.
func (r *CommentRepository) Reopen(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	return r.changed(ctx, `UPDATE comments SET resolved_at = NULL WHERE id = $1 AND resolved_at IS NOT NULL AND `+liveComment("comments")+` AND `+issueVisible(ctx, "comments.issue_id")+` RETURNING *`, id)
}

// Update replaces the body of authorID's comment on issueID, marks it edited
// and returns it. It returns nil when nothing changed: the body is the same,
// or the comment is missing, deleted, someone else's, on another issue or not
// visible.
func (r *CommentRepository) Update(ctx context.Context, issueID, id, authorID uuid.UUID, body string) (*domain.Comment, error) {
	return r.changed(ctx, `UPDATE comments SET body = $4, edited_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND issue_id = $2 AND user_id = $3 AND body IS DISTINCT FROM $4 AND `+liveComment("comments")+` AND `+issueVisible(ctx, "comments.issue_id")+` RETURNING *`,
		id, issueID, authorID, body)
}

// SoftDelete marks the comment on issueID deleted by actorID and returns it,
// or returns nil when it is missing, already deleted, on another issue or not
// visible. Its replies are hidden with it.
func (r *CommentRepository) SoftDelete(ctx context.Context, issueID, id, actorID uuid.UUID) (*domain.Comment, error) {
	return r.changed(ctx, `UPDATE comments SET deleted_at = NOW(), deleted_by = $3
		WHERE id = $1 AND issue_id = $2 AND `+liveComment("comments")+` AND `+issueVisible(ctx, "comments.issue_id")+` RETURNING *`,
		id, issueID, actorID)
}

// Restore brings back the soft-deleted comment on issueID and returns it.
// A comment that is not deleted is returned unchanged. It returns
// ErrRestoreExpired when the comment was deleted before deletedSince and
// sql.ErrNoRows when it is missing, on another issue or not visible.
func (r *CommentRepository) Restore(ctx context.Context, issueID, id uuid.UUID, deletedSince time.Time) (*domain.Comment, error) {
	restored, err := r.changed(ctx, `UPDATE comments SET deleted_at = NULL, deleted_by = NULL
		WHERE id = $1 AND issue_id = $2 AND deleted_at >= $3 AND `+issueVisible(ctx, "comments.issue_id")+` RETURNING *`,
		id, issueID, deletedSince)
	if err != nil || restored != nil {
		return restored, err
	}
	var current domain.Comment
	if err := r.db.GetContext(ctx, &current, `SELECT * FROM comments WHERE id = $1 AND issue_id = $2 AND `+issueVisible(ctx, "comments.issue_id"), id, issueID); err != nil {
		return nil, err
	}
	if current.DeletedAt != nil {
		return nil, ErrRestoreExpired
	}
	return &current, nil
}

// PurgeDeletedComments hard-deletes comments soft-deleted before
// deletedBefore, in batches; replies go with them through the parent_id
// cascade. It returns how many deleted comments it removed. It is a
// background job and applies no caller visibility.
func (r *CommentRepository) PurgeDeletedComments(ctx context.Context, deletedBefore time.Time, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	total := 0
	for {
		result, err := r.db.ExecContext(ctx, `DELETE FROM comments WHERE id IN (
			SELECT id FROM comments WHERE deleted_at < $1 ORDER BY deleted_at LIMIT $2 FOR UPDATE SKIP LOCKED)`,
			deletedBefore, batchSize)
		if err != nil {
			return total, err
		}
		n, _ := result.RowsAffected()
		total += int(n)
		if int(n) < batchSize {
			return total, nil
		}
	}
}

// changed runs an UPDATE ... RETURNING * and returns the row, or nil when no
// row matched.
func (r *CommentRepository) changed(ctx context.Context, query string, args ...any) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.GetContext(ctx, &comment, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &comment, nil
}
