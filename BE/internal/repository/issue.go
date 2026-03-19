package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type IssueRepository struct {
	db *sqlx.DB
}

func NewIssueRepository(db *sqlx.DB) *IssueRepository {
	return &IssueRepository{db: db}
}

func (r *IssueRepository) Create(ctx context.Context, tx *sqlx.Tx, issue *domain.Issue) error {
	query := `
		INSERT INTO issues (id, workspace_id, team_id, project_id, cycle_id, number, identifier_text, title, description, status, priority, creator_id, assignee_id, parent_id, estimate, due_date, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING created_at, updated_at`
	return tx.QueryRowContext(ctx, query,
		issue.ID, issue.WorkspaceID, issue.TeamID, issue.ProjectID, issue.CycleID,
		issue.Number, issue.Identifier, issue.Title, issue.Description,
		issue.Status, issue.Priority, issue.CreatorID, issue.AssigneeID,
		issue.ParentID, issue.Estimate, issue.DueDate, issue.SortOrder,
	).Scan(&issue.CreatedAt, &issue.UpdatedAt)
}

func (r *IssueRepository) NextNumber(ctx context.Context, tx *sqlx.Tx, teamID uuid.UUID) (int, error) {
	// Advisory lock per team to guarantee sequential numbering
	var num int
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, teamID)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE team_id = $1`, teamID).Scan(&num)
	return num, err
}

func (r *IssueRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Issue, error) {
	var issue domain.Issue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

func (r *IssueRepository) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	var issue domain.Issue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE workspace_id = $1 AND identifier_text = $2`, workspaceID, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

func (r *IssueRepository) List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error) {
	where := []string{"i.workspace_id = :workspace_id"}
	args := map[string]interface{}{"workspace_id": workspaceID}

	// Multi-value status filter (comma-separated)
	if params.Status != "" {
		statuses := strings.Split(params.Status, ",")
		if len(statuses) == 1 {
			where = append(where, "i.status = :status")
			args["status"] = statuses[0]
		} else {
			placeholders := make([]string, len(statuses))
			for i, s := range statuses {
				key := fmt.Sprintf("status_%d", i)
				placeholders[i] = ":" + key
				args[key] = strings.TrimSpace(s)
			}
			where = append(where, fmt.Sprintf("i.status IN (%s)", strings.Join(placeholders, ",")))
		}
	}
	// Multi-value priority filter (comma-separated)
	if params.Priority != "" {
		priorities := strings.Split(params.Priority, ",")
		if len(priorities) == 1 {
			where = append(where, "i.priority = :priority")
			args["priority"] = priorities[0]
		} else {
			placeholders := make([]string, len(priorities))
			for i, p := range priorities {
				key := fmt.Sprintf("priority_%d", i)
				placeholders[i] = ":" + key
				args[key] = strings.TrimSpace(p)
			}
			where = append(where, fmt.Sprintf("i.priority IN (%s)", strings.Join(placeholders, ",")))
		}
	}
	if params.AssigneeID != "" {
		if params.AssigneeID == "none" {
			where = append(where, "i.assignee_id IS NULL")
		} else {
			where = append(where, "i.assignee_id = :assignee_id")
			args["assignee_id"] = params.AssigneeID
		}
	}
	if params.CreatorID != "" {
		where = append(where, "i.creator_id = :creator_id")
		args["creator_id"] = params.CreatorID
	}
	if params.TeamID != "" {
		where = append(where, "i.team_id = :team_id")
		args["team_id"] = params.TeamID
	}
	if params.ProjectID != "" {
		if params.ProjectID == "none" {
			where = append(where, "i.project_id IS NULL")
		} else {
			where = append(where, "i.project_id = :project_id")
			args["project_id"] = params.ProjectID
		}
	}
	if params.LabelID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = i.id AND il.label_id = :label_id)")
		args["label_id"] = params.LabelID
	}
	if params.Search != "" {
		where = append(where, "(i.title ILIKE :search OR i.identifier_text ILIKE :search)")
		args["search"] = "%" + params.Search + "%"
	}
	if params.DueBefore != "" {
		where = append(where, "i.due_date <= :due_before")
		args["due_before"] = params.DueBefore
	}
	if params.DueAfter != "" {
		where = append(where, "i.due_date >= :due_after")
		args["due_after"] = params.DueAfter
	}

	whereClause := strings.Join(where, " AND ")

	// Count
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM issues i WHERE %s`, whereClause)
	countQuery, countArgs, err := sqlx.Named(countQuery, args)
	if err != nil {
		return nil, 0, err
	}
	countQuery = r.db.Rebind(countQuery)
	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, countArgs...); err != nil {
		return nil, 0, err
	}

	// Sort
	sortCol := "i.created_at"
	allowedSorts := map[string]bool{"created_at": true, "updated_at": true, "priority": true, "sort_order": true, "status": true}
	if params.Sort != "" && allowedSorts[params.Sort] {
		sortCol = "i." + params.Sort
	}
	order := "DESC"
	if params.Order == "asc" {
		order = "ASC"
	}

	params.Defaults()
	args["limit"] = params.PerPage
	args["offset"] = params.Offset()

	dataQuery := fmt.Sprintf(`SELECT i.* FROM issues i WHERE %s ORDER BY %s %s LIMIT :limit OFFSET :offset`, whereClause, sortCol, order)
	dataQuery, dataArgs, err := sqlx.Named(dataQuery, args)
	if err != nil {
		return nil, 0, err
	}
	dataQuery = r.db.Rebind(dataQuery)

	var issues []domain.Issue
	if err := r.db.SelectContext(ctx, &issues, dataQuery, dataArgs...); err != nil {
		return nil, 0, err
	}

	return issues, total, nil
}

func (r *IssueRepository) Update(ctx context.Context, issue *domain.Issue) error {
	query := `
		UPDATE issues SET
			title = $1, description = $2, status = $3, priority = $4,
			assignee_id = $5, project_id = $6, cycle_id = $7, parent_id = $8,
			estimate = $9, due_date = $10, sort_order = $11, updated_at = NOW()
		WHERE id = $12
		RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		issue.Title, issue.Description, issue.Status, issue.Priority,
		issue.AssigneeID, issue.ProjectID, issue.CycleID, issue.ParentID,
		issue.Estimate, issue.DueDate, issue.SortOrder, issue.ID,
	).Scan(&issue.UpdatedAt)
}

func (r *IssueRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issues WHERE id = $1`, id)
	return err
}

func (r *IssueRepository) SetLabels(ctx context.Context, issueID uuid.UUID, labelIDs []uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels WHERE issue_id = $1`, issueID); err != nil {
		return err
	}

	for _, labelID := range labelIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, issueID, labelID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *IssueRepository) GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error) {
	var labels []domain.Label
	query := `SELECT l.* FROM labels l INNER JOIN issue_labels il ON l.id = il.label_id WHERE il.issue_id = $1 ORDER BY l.name`
	err := r.db.SelectContext(ctx, &labels, query, issueID)
	return labels, err
}

func (r *IssueRepository) ListSubIssues(ctx context.Context, parentID uuid.UUID) ([]domain.Issue, error) {
	var issues []domain.Issue
	err := r.db.SelectContext(ctx, &issues, `SELECT * FROM issues WHERE parent_id = $1 ORDER BY sort_order, created_at`, parentID)
	return issues, err
}

func (r *IssueRepository) CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error) {
	var total, done int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE status IN ('done', 'cancelled')) FROM issues WHERE parent_id = $1`, parentID).Scan(&total, &done)
	return total, done, err
}

func (r *IssueRepository) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	return r.db.BeginTxx(ctx, nil)
}
