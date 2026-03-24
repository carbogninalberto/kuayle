package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type SharedLinkRepository struct {
	db *sqlx.DB
}

func NewSharedLinkRepository(db *sqlx.DB) *SharedLinkRepository {
	return &SharedLinkRepository{db: db}
}

func (r *SharedLinkRepository) Create(ctx context.Context, link *domain.SharedLink) error {
	query := `INSERT INTO shared_links (id, token, workspace_id, created_by, scope, scope_id, filters, include_description, is_active, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		link.ID, link.Token, link.WorkspaceID, link.CreatedBy,
		link.Scope, link.ScopeID, link.Filters, link.IncludeDescription,
		link.IsActive, link.ExpiresAt,
	).Scan(&link.CreatedAt, &link.UpdatedAt)
}

func (r *SharedLinkRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.SharedLink, error) {
	var link domain.SharedLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM shared_links WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &link, err
}

func (r *SharedLinkRepository) GetByToken(ctx context.Context, token string) (*domain.SharedLink, error) {
	var link domain.SharedLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM shared_links WHERE token = $1`, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &link, err
}

func (r *SharedLinkRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.SharedLink, error) {
	var links []domain.SharedLink
	err := r.db.SelectContext(ctx, &links,
		`SELECT * FROM shared_links WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
	return links, err
}

func (r *SharedLinkRepository) Update(ctx context.Context, link *domain.SharedLink) error {
	query := `UPDATE shared_links SET is_active = $1, include_description = $2, expires_at = $3, updated_at = NOW()
		WHERE id = $4 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		link.IsActive, link.IncludeDescription, link.ExpiresAt, link.ID,
	).Scan(&link.UpdatedAt)
}

func (r *SharedLinkRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM shared_links WHERE id = $1`, id)
	return err
}
