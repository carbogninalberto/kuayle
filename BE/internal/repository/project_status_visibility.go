package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type ProjectStatusVisibilityRepository struct {
	db *sqlx.DB
}

func NewProjectStatusVisibilityRepository(db *sqlx.DB) *ProjectStatusVisibilityRepository {
	return &ProjectStatusVisibilityRepository{db: db}
}

func (r *ProjectStatusVisibilityRepository) SetVisibleStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := requireVisible(ctx, tx, "EXISTS(SELECT 1 FROM projects p WHERE p.id="+uuidSQL(projectID)+" AND "+projectVisible(ctx, "p.workspace_id", "p.team_id")+")"); err != nil {
		return err
	}
	for _, sid := range statusIDs {
		if err := requireVisible(ctx, tx, projectStatusReferenceVisible(ctx, uuidSQL(projectID), uuidSQL(sid))); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_status_visibility WHERE project_id = $1`, projectID); err != nil {
		return err
	}

	for _, sid := range statusIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_status_visibility (project_id, status_id) VALUES ($1, $2)`, projectID, sid); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ProjectStatusVisibilityRepository) ListVisibleStatuses(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.SelectContext(ctx, &ids, `SELECT status_id FROM project_status_visibility WHERE project_id = $1 AND `+projectStatusReferenceVisible(ctx, "project_status_visibility.project_id", "project_status_visibility.status_id"), projectID)
	return ids, err
}

func (r *ProjectStatusVisibilityRepository) ListProjectsForStatus(ctx context.Context, statusID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.SelectContext(ctx, &ids, `SELECT project_id FROM project_status_visibility WHERE status_id = $1 AND `+projectStatusReferenceVisible(ctx, "project_status_visibility.project_id", "project_status_visibility.status_id"), statusID)
	return ids, err
}

func (r *ProjectStatusVisibilityRepository) ListProjectIDsByStatuses(ctx context.Context, statusIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if len(statusIDs) == 0 {
		return make(map[uuid.UUID][]uuid.UUID), nil
	}

	type row struct {
		StatusID  uuid.UUID `db:"status_id"`
		ProjectID uuid.UUID `db:"project_id"`
	}
	var rows []row

	query, args, err := sqlx.In(`SELECT status_id, project_id FROM project_status_visibility WHERE status_id IN (?) AND `+projectStatusReferenceVisible(ctx, "project_status_visibility.project_id", "project_status_visibility.status_id"), statusIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]uuid.UUID, len(statusIDs))
	for _, r := range rows {
		result[r.StatusID] = append(result[r.StatusID], r.ProjectID)
	}
	return result, nil
}

func projectStatusReferenceVisible(ctx context.Context, project, status string) string {
	return "EXISTS(SELECT 1 FROM projects psv_project JOIN team_statuses psv_status ON psv_status.id=" + status + " JOIN teams psv_team ON psv_team.id=psv_status.team_id WHERE psv_project.id=" + project + " AND psv_project.workspace_id=psv_team.workspace_id AND " + projectVisible(ctx, "psv_project.workspace_id", "psv_project.team_id") + " AND " + teamVisible(ctx, "psv_team.id") + " AND " + compatibleTeams("psv_project.team_id", "psv_team.id") + ")"
}
