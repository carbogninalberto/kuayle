package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type ProjectRepository struct {
	db *sqlx.DB
}

func NewProjectRepository(db *sqlx.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Create(ctx context.Context, project *domain.Project) error {
	query := `INSERT INTO projects (id, workspace_id, team_id, name, description, status, lead_id, start_date, target_date, sort_order) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, project.ID, project.WorkspaceID, project.TeamID, project.Name, project.Description, project.Status, project.LeadID, project.StartDate, project.TargetDate, project.SortOrder).Scan(&project.CreatedAt, &project.UpdatedAt)
}

func (r *ProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	var project domain.Project
	err := r.db.GetContext(ctx, &project, `SELECT * FROM projects WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &project, err
}

func (r *ProjectRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Project, error) {
	var projects []domain.Project
	err := r.db.SelectContext(ctx, &projects, `SELECT * FROM projects WHERE workspace_id = $1 ORDER BY sort_order, name`, workspaceID)
	return projects, err
}

func (r *ProjectRepository) Update(ctx context.Context, project *domain.Project) error {
	query := `UPDATE projects SET name = $1, description = $2, status = $3, lead_id = $4, start_date = $5, target_date = $6, sort_order = $7, team_id = $8, updated_at = NOW() WHERE id = $9 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, project.Name, project.Description, project.Status, project.LeadID, project.StartDate, project.TargetDate, project.SortOrder, project.TeamID, project.ID).Scan(&project.UpdatedAt)
}

func (r *ProjectRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Project, error) {
	var projects []domain.Project
	err := r.db.SelectContext(ctx, &projects, `SELECT * FROM projects WHERE team_id = $1 ORDER BY sort_order, name`, teamID)
	return projects, err
}

func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

func (r *ProjectRepository) IssueStats(ctx context.Context, projectID uuid.UUID) (total int, completed int, cancelled int, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'done'), COUNT(*) FILTER (WHERE status = 'cancelled') FROM issues WHERE project_id = $1`,
		projectID,
	).Scan(&total, &completed, &cancelled)
	return
}
