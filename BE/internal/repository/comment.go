package repository

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
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
	query := `INSERT INTO comments (id, issue_id, user_id, body) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, comment.ID, comment.IssueID, comment.UserID, comment.Body).Scan(&comment.CreatedAt, &comment.UpdatedAt)
}

func (r *CommentRepository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error) {
	var comments []domain.Comment
	err := r.db.SelectContext(ctx, &comments, `SELECT * FROM comments WHERE issue_id = $1 ORDER BY created_at ASC`, issueID)
	return comments, err
}
