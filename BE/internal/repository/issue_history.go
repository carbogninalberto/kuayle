package repository

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type IssueHistoryRepository struct {
	db *sqlx.DB
}

func NewIssueHistoryRepository(db *sqlx.DB) *IssueHistoryRepository {
	return &IssueHistoryRepository{db: db}
}

func (r *IssueHistoryRepository) Create(ctx context.Context, issueID, userID uuid.UUID, field string, oldValue, newValue *string) error {
	id := uuid.New()
	_, err := r.db.ExecContext(ctx, `INSERT INTO issue_history (id, issue_id, user_id, field, old_value, new_value) VALUES ($1, $2, $3, $4, $5, $6)`, id, issueID, userID, field, oldValue, newValue)
	return err
}

func (r *IssueHistoryRepository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueHistory, error) {
	var entries []domain.IssueHistory
	err := r.db.SelectContext(ctx, &entries, `SELECT * FROM issue_history WHERE issue_id = $1 ORDER BY created_at DESC`, issueID)
	return entries, err
}
