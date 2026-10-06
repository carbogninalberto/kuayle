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
		INSERT INTO assets (id, workspace_id, storage_key, filename, content_type, size, uploaded_by, team_id)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8 WHERE ` + assetVisible(ctx, "CAST($2 AS uuid)", "CAST($8 AS uuid)") + `
		RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		asset.ID, asset.WorkspaceID, asset.StorageKey, asset.Filename,
		asset.ContentType, asset.Size, asset.UploadedBy, asset.TeamID,
	).Scan(&asset.CreatedAt)
}

func (r *AssetRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Asset, error) {
	var asset domain.Asset
	err := r.db.GetContext(ctx, &asset, `SELECT * FROM assets WHERE id = $1 AND `+assetVisible(ctx, "assets.workspace_id", "assets.team_id"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &asset, err
}

func assetVisible(ctx context.Context, workspace, team string) string {
	// Unattributed legacy uploads cannot be safely classified once a workspace
	// uses private teams. This remains sticky after team deletion/declassification.
	return "(" + workspaceVisible(ctx, workspace) + " AND ((" + team + " IS NULL AND NOT EXISTS(SELECT 1 FROM workspace_privacy ap WHERE ap.workspace_id=" + workspace + ")) OR EXISTS(SELECT 1 FROM teams asset_team WHERE asset_team.id=" + team + " AND asset_team.workspace_id=" + workspace + " AND " + teamVisible(ctx, "asset_team.id") + ")))"
}
