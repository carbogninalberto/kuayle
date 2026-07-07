package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type IssueRelationRepository struct {
	db *sqlx.DB
}

func NewIssueRelationRepository(db *sqlx.DB) *IssueRelationRepository {
	return &IssueRelationRepository{db: db}
}

func (r *IssueRelationRepository) Create(ctx context.Context, rel *domain.IssueRelation) error {
	query := `INSERT INTO issue_relations (id, issue_id, related_issue_id, type) VALUES ($1, $2, $3, $4) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, rel.ID, rel.IssueID, rel.RelatedIssueID, rel.Type).Scan(&rel.CreatedAt)
}

func (r *IssueRelationRepository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueRelation, error) {
	var relations []domain.IssueRelation
	query := `SELECT * FROM issue_relations WHERE issue_id = $1 OR related_issue_id = $1 ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &relations, query, issueID)
	return relations, err
}

func (r *IssueRelationRepository) ListByIssues(ctx context.Context, issueIDs []uuid.UUID) ([]domain.IssueRelation, error) {
	if len(issueIDs) == 0 {
		return []domain.IssueRelation{}, nil
	}

	query, args, err := sqlx.In(`SELECT * FROM issue_relations WHERE issue_id IN (?) OR related_issue_id IN (?) ORDER BY created_at DESC`, issueIDs, issueIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	var relations []domain.IssueRelation
	err = r.db.SelectContext(ctx, &relations, query, args...)
	return relations, err
}

func (r *IssueRelationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_relations WHERE id = $1`, id)
	return err
}

func (r *IssueRelationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueRelation, error) {
	var rel domain.IssueRelation
	err := r.db.GetContext(ctx, &rel, `SELECT * FROM issue_relations WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *IssueRelationRepository) DeleteByIssues(ctx context.Context, issueID, relatedIssueID uuid.UUID, relType domain.IssueRelationType) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_relations WHERE issue_id = $1 AND related_issue_id = $2 AND type = $3`, issueID, relatedIssueID, relType)
	return err
}
