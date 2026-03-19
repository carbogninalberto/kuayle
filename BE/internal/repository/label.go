package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type LabelRepository struct {
	db *sqlx.DB
}

func NewLabelRepository(db *sqlx.DB) *LabelRepository {
	return &LabelRepository{db: db}
}

func (r *LabelRepository) Create(ctx context.Context, label *domain.Label) error {
	query := `INSERT INTO labels (id, workspace_id, name, color, description, parent_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, label.ID, label.WorkspaceID, label.Name, label.Color, label.Description, label.ParentID).Scan(&label.CreatedAt, &label.UpdatedAt)
}

func (r *LabelRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Label, error) {
	var label domain.Label
	err := r.db.GetContext(ctx, &label, `SELECT * FROM labels WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &label, err
}

func (r *LabelRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Label, error) {
	var labels []domain.Label
	err := r.db.SelectContext(ctx, &labels, `SELECT * FROM labels WHERE workspace_id = $1 ORDER BY name`, workspaceID)
	return labels, err
}

func (r *LabelRepository) Update(ctx context.Context, label *domain.Label) error {
	query := `UPDATE labels SET name = $1, color = $2, description = $3, updated_at = NOW() WHERE id = $4 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, label.Name, label.Color, label.Description, label.ID).Scan(&label.UpdatedAt)
}

func (r *LabelRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM labels WHERE id = $1`, id)
	return err
}
