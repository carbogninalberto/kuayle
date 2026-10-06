package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type IssueTemplateRepository struct {
	db *sqlx.DB
}

func NewIssueTemplateRepository(db *sqlx.DB) *IssueTemplateRepository {
	return &IssueTemplateRepository{db: db}
}

func (r *IssueTemplateRepository) Create(ctx context.Context, tmpl *domain.IssueTemplate) error {
	query := `INSERT INTO issue_templates (id, workspace_id, team_id, title, description, status, priority, assignee_id, label_ids, recurrence_rule, next_run_at, is_active, created_by)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13 WHERE ` + projectVisible(ctx, "CAST($2 AS uuid)", "CAST($3 AS uuid)") + ` RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		tmpl.ID, tmpl.WorkspaceID, tmpl.TeamID, tmpl.Title, tmpl.Description,
		tmpl.Status, tmpl.Priority, tmpl.AssigneeID, tmpl.LabelIDs,
		tmpl.RecurrenceRule, tmpl.NextRunAt, tmpl.IsActive, tmpl.CreatedBy,
	).Scan(&tmpl.CreatedAt, &tmpl.UpdatedAt)
}

func (r *IssueTemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueTemplate, error) {
	var tmpl domain.IssueTemplate
	err := r.db.GetContext(ctx, &tmpl, `SELECT * FROM issue_templates WHERE id = $1 AND `+projectVisible(ctx, "issue_templates.workspace_id", "issue_templates.team_id"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &tmpl, err
}

func (r *IssueTemplateRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.IssueTemplate, error) {
	var templates []domain.IssueTemplate
	err := r.db.SelectContext(ctx, &templates, `SELECT * FROM issue_templates WHERE workspace_id = $1 AND `+projectVisible(ctx, "issue_templates.workspace_id", "issue_templates.team_id")+` ORDER BY title`, workspaceID)
	return templates, err
}

func (r *IssueTemplateRepository) Update(ctx context.Context, tmpl *domain.IssueTemplate) error {
	query := `UPDATE issue_templates SET
		title = $1, description = $2, status = $3, priority = $4, assignee_id = $5,
		team_id = $6, label_ids = $7, recurrence_rule = $8, next_run_at = $9,
		is_active = $10, updated_at = NOW()
		WHERE id = $11 AND ` + projectVisible(ctx, "issue_templates.workspace_id", "CAST($6 AS uuid)") + " AND " + projectVisible(ctx, "issue_templates.workspace_id", "issue_templates.team_id") + ` AND ` + compatibleTeams("issue_templates.team_id", "CAST($6 AS uuid)") + ` RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		tmpl.Title, tmpl.Description, tmpl.Status, tmpl.Priority, tmpl.AssigneeID,
		tmpl.TeamID, tmpl.LabelIDs, tmpl.RecurrenceRule, tmpl.NextRunAt,
		tmpl.IsActive, tmpl.ID,
	).Scan(&tmpl.UpdatedAt)
}

func (r *IssueTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_templates WHERE id = $1 AND `+projectVisible(ctx, "issue_templates.workspace_id", "issue_templates.team_id"), id)
	return err
}

func (r *IssueTemplateRepository) ListDueForRecurrence(ctx context.Context) ([]domain.IssueTemplate, error) {
	var templates []domain.IssueTemplate
	err := r.db.SelectContext(ctx, &templates, `SELECT * FROM issue_templates WHERE is_active = true AND next_run_at IS NOT NULL AND next_run_at <= NOW() AND (team_id IS NULL OR `+teamVisible(context.Background(), "issue_templates.team_id")+`)`)
	return templates, err
}
