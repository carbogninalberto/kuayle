package repository

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type CommentRepository struct {
	db *sqlx.DB
}

func NewCommentRepository(db *sqlx.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

func (r *CommentRepository) Create(ctx context.Context, comment *domain.Comment) error {
	query := `INSERT INTO comments (id, issue_id, user_id, body, parent_id) VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, comment.ID, comment.IssueID, comment.UserID, comment.Body, comment.ParentID).Scan(&comment.CreatedAt, &comment.UpdatedAt)
}

func (r *CommentRepository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error) {
	var comments []domain.Comment
	err := r.db.SelectContext(ctx, &comments, `SELECT * FROM comments WHERE issue_id = $1 AND parent_id IS NULL ORDER BY created_at ASC`, issueID)
	return comments, err
}

func (r *CommentRepository) ListReplies(ctx context.Context, parentID uuid.UUID) ([]domain.Comment, error) {
	var comments []domain.Comment
	err := r.db.SelectContext(ctx, &comments, `SELECT * FROM comments WHERE parent_id = $1 ORDER BY created_at ASC`, parentID)
	return comments, err
}

func (r *CommentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.GetContext(ctx, &comment, `SELECT * FROM comments WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *CommentRepository) Resolve(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE comments SET resolved_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *CommentRepository) Reopen(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE comments SET resolved_at = NULL WHERE id = $1`, id)
	return err
}
