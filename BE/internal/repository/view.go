package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type ViewRepository struct {
	db *sqlx.DB
}

func NewViewRepository(db *sqlx.DB) *ViewRepository {
	return &ViewRepository{db: db}
}

func (r *ViewRepository) Create(ctx context.Context, view *domain.View) error {
	query := `INSERT INTO views (id, workspace_id, creator_id, name, description, filters, is_shared) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, view.ID, view.WorkspaceID, view.CreatorID, view.Name, view.Description, view.Filters, view.IsShared).Scan(&view.CreatedAt, &view.UpdatedAt)
}

func (r *ViewRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.View, error) {
	var view domain.View
	err := r.db.GetContext(ctx, &view, `SELECT * FROM views WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &view, err
}

func (r *ViewRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, userID uuid.UUID) ([]domain.View, error) {
	var views []domain.View
	err := r.db.SelectContext(ctx, &views, `SELECT * FROM views WHERE workspace_id = $1 AND (creator_id = $2 OR is_shared = true) ORDER BY name`, workspaceID, userID)
	return views, err
}

func (r *ViewRepository) Update(ctx context.Context, view *domain.View) error {
	query := `UPDATE views SET name = $1, description = $2, filters = $3, is_shared = $4, updated_at = NOW() WHERE id = $5 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, view.Name, view.Description, view.Filters, view.IsShared, view.ID).Scan(&view.UpdatedAt)
}

func (r *ViewRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM views WHERE id = $1`, id)
	return err
}
