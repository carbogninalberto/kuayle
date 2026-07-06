package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type AssetRepository struct {
	db *sqlx.DB
}

func NewAssetRepository(db *sqlx.DB) *AssetRepository {
	return &AssetRepository{db: db}
}

func (r *AssetRepository) Create(ctx context.Context, asset *domain.Asset) error {
	query := `
		INSERT INTO assets (id, workspace_id, storage_key, filename, content_type, size, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		asset.ID, asset.WorkspaceID, asset.StorageKey, asset.Filename,
		asset.ContentType, asset.Size, asset.UploadedBy,
	).Scan(&asset.CreatedAt)
}

func (r *AssetRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Asset, error) {
	var asset domain.Asset
	err := r.db.GetContext(ctx, &asset, `SELECT * FROM assets WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &asset, err
}
