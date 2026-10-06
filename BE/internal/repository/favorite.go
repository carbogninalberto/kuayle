package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type FavoriteRepository struct {
	db *sqlx.DB
}

func NewFavoriteRepository(db *sqlx.DB) *FavoriteRepository {
	return &FavoriteRepository{db: db}
}

func (r *FavoriteRepository) Create(ctx context.Context, fav *domain.Favorite) error {
	query := `INSERT INTO favorites (id, workspace_id, user_id, entity_type, entity_id, position)
		SELECT $1, CAST($2 AS uuid), CAST($3 AS uuid), CAST($4 AS text), CAST($5 AS uuid), (SELECT COALESCE(MAX(position), 0) + 1 FROM favorites WHERE workspace_id = $2 AND user_id = $3) WHERE ` + favoriteVisible(ctx, "CAST($2 AS uuid)", "CAST($3 AS uuid)", "CAST($4 AS text)", "CAST($5 AS uuid)") + `
		RETURNING position, created_at`
	return r.db.QueryRowContext(ctx, query, fav.ID, fav.WorkspaceID, fav.UserID, fav.EntityType, fav.EntityID).Scan(&fav.Position, &fav.CreatedAt)
}

func (r *FavoriteRepository) ListByUser(ctx context.Context, workspaceID, userID uuid.UUID) ([]domain.Favorite, error) {
	var favs []domain.Favorite
	err := r.db.SelectContext(ctx, &favs, `SELECT id,workspace_id,user_id,entity_type,entity_id,ROW_NUMBER() OVER(ORDER BY position)::int AS position,created_at FROM favorites WHERE workspace_id = $1 AND user_id = $2 AND `+favoriteVisible(ctx, "favorites.workspace_id", "favorites.user_id", "favorites.entity_type", "favorites.entity_id")+` ORDER BY position`, workspaceID, userID)
	return favs, err
}

func (r *FavoriteRepository) Delete(ctx context.Context, workspaceID, userID uuid.UUID, entityType string, entityID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM favorites WHERE workspace_id = $1 AND user_id = $2 AND entity_type = $3 AND entity_id = $4 AND `+favoriteOwner(ctx, "favorites.workspace_id", "favorites.user_id"), workspaceID, userID, entityType, entityID)
	return err
}

func (r *FavoriteRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM favorites WHERE id = $1 AND `+favoriteOwner(ctx, "favorites.workspace_id", "favorites.user_id"), id)
	return err
}

func favoriteOwner(ctx context.Context, workspace, user string) string {
	access, ok := domain.AccessFromContext(ctx)
	if !ok {
		return "FALSE"
	}
	return "(" + workspaceVisible(ctx, workspace) + " AND " + user + "=" + uuidSQL(access.UserID) + ")"
}

func favoriteVisible(ctx context.Context, workspace, user, kind, id string) string {
	return "(" + favoriteOwner(ctx, workspace, user) + " AND ((" +
		kind + "='team' AND EXISTS(SELECT 1 FROM teams ft WHERE ft.id=" + id + " AND ft.workspace_id=" + workspace + " AND " + teamVisible(ctx, "ft.id") + ")) OR (" +
		kind + "='project' AND EXISTS(SELECT 1 FROM projects fp WHERE fp.id=" + id + " AND fp.workspace_id=" + workspace + " AND " + projectVisible(ctx, "fp.workspace_id", "fp.team_id") + ")) OR (" +
		kind + "='cycle' AND EXISTS(SELECT 1 FROM cycles fc JOIN teams fct ON fct.id=fc.team_id WHERE fc.id=" + id + " AND fct.workspace_id=" + workspace + " AND " + teamVisible(ctx, "fc.team_id") + ")) OR (" +
		kind + "='view' AND EXISTS(SELECT 1 FROM views fv WHERE fv.id=" + id + " AND fv.workspace_id=" + workspace + " AND " + viewVisible(ctx, "fv.workspace_id", "fv.creator_id", "fv.is_shared") + ")) OR (" +
		kind + "='label' AND EXISTS(SELECT 1 FROM labels fl WHERE fl.id=" + id + " AND fl.workspace_id=" + workspace + "))))"
}
