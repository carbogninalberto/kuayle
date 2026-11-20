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

type IssueTemplateRepository struct {
	db *sqlx.DB
}

func NewIssueTemplateRepository(db *sqlx.DB) *IssueTemplateRepository {
	return &IssueTemplateRepository{db: db}
}

func (r *IssueTemplateRepository) Create(ctx context.Context, tmpl *domain.IssueTemplate) error {
	query := `INSERT INTO issue_templates (id, workspace_id, team_id, title, default_issue_title, description, status, priority, assignee_id, label_ids, recurrence_rule, next_run_at, is_active, created_by)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14 WHERE ` + projectVisible(ctx, "CAST($2 AS uuid)", "CAST($3 AS uuid)") + ` AND ` + optionalWorkspaceMember("CAST($2 AS uuid)", "CAST($9 AS uuid)") + ` RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		tmpl.ID, tmpl.WorkspaceID, tmpl.TeamID, tmpl.Title, tmpl.DefaultIssueTitle, tmpl.Description,
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

func (r *IssueTemplateRepository) Update(ctx context.Context, tmpl *domain.IssueTemplate, resetSchedule bool) error {
	query := `UPDATE issue_templates SET
		title = $1, default_issue_title = $2, description = $3, status = $4, priority = $5, assignee_id = $6,
		team_id = $7, label_ids = $8, recurrence_rule = CASE WHEN $13 THEN $9 ELSE recurrence_rule END,
		next_run_at = CASE WHEN $13 THEN $10 ELSE next_run_at END,
		last_error = CASE WHEN $13 THEN NULL ELSE last_error END, recurrence_lease_until = NULL,
		is_active = CASE WHEN $13 THEN $11 ELSE is_active END, updated_at = NOW()
		WHERE id = $12 AND ` + projectVisible(ctx, "issue_templates.workspace_id", "CAST($7 AS uuid)") + " AND " + projectVisible(ctx, "issue_templates.workspace_id", "issue_templates.team_id") + ` AND ` + compatibleTeams("issue_templates.team_id", "CAST($7 AS uuid)") + ` AND ` + optionalWorkspaceMember("issue_templates.workspace_id", "CAST($6 AS uuid)") + ` RETURNING updated_at, recurrence_rule, is_active, next_run_at, last_run_at, last_error`
	return r.db.QueryRowContext(ctx, query,
		tmpl.Title, tmpl.DefaultIssueTitle, tmpl.Description, tmpl.Status, tmpl.Priority, tmpl.AssigneeID,
		tmpl.TeamID, tmpl.LabelIDs, tmpl.RecurrenceRule, tmpl.NextRunAt,
		tmpl.IsActive, tmpl.ID, resetSchedule,
	).Scan(&tmpl.UpdatedAt, &tmpl.RecurrenceRule, &tmpl.IsActive, &tmpl.NextRunAt, &tmpl.LastRunAt, &tmpl.LastError)
}

func (r *IssueTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_templates WHERE id = $1 AND `+projectVisible(ctx, "issue_templates.workspace_id", "issue_templates.team_id"), id)
	return err
}

func (r *IssueTemplateRepository) ListDueForRecurrence(ctx context.Context) ([]domain.IssueTemplate, error) {
	var templates []domain.IssueTemplate
	err := r.db.SelectContext(ctx, &templates, `SELECT * FROM issue_templates WHERE `+dueIssueTemplate+`
		ORDER BY next_run_at NULLS FIRST, id LIMIT 100`)
	return templates, err
}

// Templates saved before the worker shipped can have a rule without a slot.
const dueIssueTemplate = `is_active AND recurrence_rule IS NOT NULL AND recurrence_rule <> 'null'::jsonb
	AND (next_run_at <= now() OR (next_run_at IS NULL AND last_error IS NULL))
	AND (recurrence_lease_until IS NULL OR recurrence_lease_until <= now())`

func (r *IssueTemplateRepository) ClaimRecurrence(ctx context.Context, candidate domain.IssueTemplate) (*domain.IssueTemplate, error) {
	var claimed domain.IssueTemplate
	err := r.db.GetContext(ctx, &claimed, `UPDATE issue_templates SET recurrence_lease_until = clock_timestamp() + interval '2 minutes'
		WHERE id = $1 AND next_run_at IS NOT DISTINCT FROM $2 AND `+dueIssueTemplate+` RETURNING *`, candidate.ID, candidate.NextRunAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &claimed, err
}

// CompleteRecurrence fences expired claims and atomically commits the occurrence,
// issue and schedule advance. A failed callback rolls everything back, leaving
// the worker free to record a failure against the same lease.
func (r *IssueTemplateRepository) CompleteRecurrence(ctx context.Context, tmpl *domain.IssueTemplate, next *time.Time, failure *string, create func(*sqlx.Tx) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if create != nil {
		// Match issue creation and key renames: take the numbering lock
		// before the template UPDATE's privacy statement trigger takes its lock.
		if err := lockIssueNumber(ctx, tx, *tmpl.TeamID); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE issue_templates SET next_run_at = $4, last_error = $5, recurrence_lease_until = NULL,
		last_run_at = CASE WHEN $2::timestamptz IS NOT NULL OR $5::text IS NOT NULL THEN now() ELSE last_run_at END
		WHERE id = $1 AND next_run_at IS NOT DISTINCT FROM $2 AND recurrence_lease_until = $3 AND recurrence_lease_until > clock_timestamp()`,
		tmpl.ID, tmpl.NextRunAt, tmpl.RecurrenceLeaseUntil, next, failure)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		return err
	}
	if create != nil {
		result, err := tx.ExecContext(ctx, `INSERT INTO issue_template_occurrences(template_id, scheduled_at)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, tmpl.ID, tmpl.NextRunAt)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 1 {
			if err := create(tx); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
