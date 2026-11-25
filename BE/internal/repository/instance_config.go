package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// InstanceConfigRepository stores the sysadmin overrides of runtime
// configuration (instance_config_overrides), one row per variable. Callers
// validate names and values against the config registry.
type InstanceConfigRepository struct {
	db *sqlx.DB
}

func NewInstanceConfigRepository(db *sqlx.DB) *InstanceConfigRepository {
	return &InstanceConfigRepository{db: db}
}

func (r *InstanceConfigRepository) ListOverrides(ctx context.Context) ([]domain.InstanceConfigOverride, error) {
	rows := []domain.InstanceConfigOverride{}
	err := r.db.SelectContext(ctx, &rows, `SELECT name, value, updated_by, updated_at FROM instance_config_overrides ORDER BY name`)
	return rows, err
}

// SetOverride saves value for name, replacing an earlier override.
func (r *InstanceConfigRepository) SetOverride(ctx context.Context, name, value string, actor uuid.UUID) (*domain.InstanceConfigOverride, error) {
	var row domain.InstanceConfigOverride
	err := r.db.GetContext(ctx, &row, `INSERT INTO instance_config_overrides (name, value, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at
		RETURNING name, value, updated_by, updated_at`, name, value, actor)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteOverride removes the override of name; there may be none.
func (r *InstanceConfigRepository) DeleteOverride(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM instance_config_overrides WHERE name = $1`, name)
	return err
}
