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
	err := r.db.SelectContext(ctx, &ids, `SELECT status_id FROM project_status_visibility WHERE project_id = $1`, projectID)
	return ids, err
}

func (r *ProjectStatusVisibilityRepository) ListProjectsForStatus(ctx context.Context, statusID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.SelectContext(ctx, &ids, `SELECT project_id FROM project_status_visibility WHERE status_id = $1`, statusID)
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

	query, args, err := sqlx.In(`SELECT status_id, project_id FROM project_status_visibility WHERE status_id IN (?)`, statusIDs)
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
