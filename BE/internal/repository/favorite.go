package repository

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type FavoriteRepository struct {
	db *sqlx.DB
}

func NewFavoriteRepository(db *sqlx.DB) *FavoriteRepository {
	return &FavoriteRepository{db: db}
}

func (r *FavoriteRepository) Create(ctx context.Context, fav *domain.Favorite) error {
	query := `INSERT INTO favorites (id, workspace_id, user_id, entity_type, entity_id, position)
		VALUES ($1, $2, $3, $4, $5, (SELECT COALESCE(MAX(position), 0) + 1 FROM favorites WHERE workspace_id = $2 AND user_id = $3))
		RETURNING position, created_at`
	return r.db.QueryRowContext(ctx, query, fav.ID, fav.WorkspaceID, fav.UserID, fav.EntityType, fav.EntityID).Scan(&fav.Position, &fav.CreatedAt)
}

func (r *FavoriteRepository) ListByUser(ctx context.Context, workspaceID, userID uuid.UUID) ([]domain.Favorite, error) {
	var favs []domain.Favorite
	err := r.db.SelectContext(ctx, &favs, `SELECT * FROM favorites WHERE workspace_id = $1 AND user_id = $2 ORDER BY position`, workspaceID, userID)
	return favs, err
}

func (r *FavoriteRepository) Delete(ctx context.Context, workspaceID, userID uuid.UUID, entityType string, entityID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM favorites WHERE workspace_id = $1 AND user_id = $2 AND entity_type = $3 AND entity_id = $4`, workspaceID, userID, entityType, entityID)
	return err
}

func (r *FavoriteRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM favorites WHERE id = $1`, id)
	return err
}
