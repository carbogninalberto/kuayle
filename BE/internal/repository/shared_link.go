package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type SharedLinkRepository struct {
	db *sqlx.DB
}

func NewSharedLinkRepository(db *sqlx.DB) *SharedLinkRepository {
	return &SharedLinkRepository{db: db}
}

func (r *SharedLinkRepository) Create(ctx context.Context, link *domain.SharedLink) error {
	query := `INSERT INTO shared_links (id, token, workspace_id, created_by, scope, scope_id, filters, include_description, is_active, expires_at)
		SELECT $1, $2, CAST($3 AS uuid), $4, CAST($5 AS text), CAST($6 AS uuid), CAST($7 AS jsonb), $8, $9, $10 WHERE ` + sharedLinkVisible(ctx, "CAST($3 AS uuid)", "CAST($5 AS text)", "CAST($6 AS uuid)", "CAST($7 AS jsonb)") + `
		RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		link.ID, link.Token, link.WorkspaceID, link.CreatedBy,
		link.Scope, link.ScopeID, link.Filters, link.IncludeDescription,
		link.IsActive, link.ExpiresAt,
	).Scan(&link.CreatedAt, &link.UpdatedAt)
	return privacyDenial(ctx, r.db, link.WorkspaceID, err)
}

func (r *SharedLinkRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.SharedLink, error) {
	var link domain.SharedLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM shared_links WHERE id = $1 AND `+sharedLinkVisible(ctx, "shared_links.workspace_id", "shared_links.scope", "shared_links.scope_id", "shared_links.filters"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &link, err
}

func (r *SharedLinkRepository) GetByToken(ctx context.Context, token string) (*domain.SharedLink, error) {
	var link domain.SharedLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM shared_links WHERE token = $1 AND `+sharedLinkVisible(domain.PublicContext(ctx), "shared_links.workspace_id", "shared_links.scope", "shared_links.scope_id", "shared_links.filters"), token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &link, err
}

func (r *SharedLinkRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.SharedLink, error) {
	var links []domain.SharedLink
	err := r.db.SelectContext(ctx, &links,
		`SELECT * FROM shared_links WHERE workspace_id = $1 AND `+sharedLinkVisible(ctx, "shared_links.workspace_id", "scope", "scope_id", "filters")+` ORDER BY created_at DESC`, workspaceID)
	return links, err
}

func (r *SharedLinkRepository) Update(ctx context.Context, link *domain.SharedLink) error {
	query := `UPDATE shared_links SET is_active = $1, include_description = $2, expires_at = $3, updated_at = NOW()
		WHERE id = $4 AND ` + sharedLinkVisible(ctx, "shared_links.workspace_id", "scope", "scope_id", "filters") + ` RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query,
		link.IsActive, link.IncludeDescription, link.ExpiresAt, link.ID,
	).Scan(&link.UpdatedAt)
}

func (r *SharedLinkRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM shared_links WHERE id = $1 AND `+workspaceVisible(ctx, "workspace_id"), id)
	return err
}

// A share is an anonymous publication even when created/read by an admin.
// Arbitrary legacy filter JSON cannot establish safe provenance in privacy mode.
func sharedLinkVisible(ctx context.Context, workspace, scope, id, filters string) string {
	public := domain.PublicContext(ctx)
	return "(" + workspaceVisible(ctx, workspace) + " AND (" + publicMetadataAllowed(workspace) + " OR (" + filters + " = '{}'::jsonb AND " + scope + " <> 'view')) AND (" +
		"(" + scope + "='workspace') OR (" + scope + "='team' AND EXISTS(SELECT 1 FROM teams slt WHERE slt.id=" + id + " AND slt.workspace_id=" + workspace + " AND " + teamVisible(public, "slt.id") + ")) OR (" +
		scope + "='project' AND EXISTS(SELECT 1 FROM projects slp WHERE slp.id=" + id + " AND slp.workspace_id=" + workspace + " AND " + projectVisible(public, "slp.workspace_id", "slp.team_id") + ")) OR (" +
		scope + "='view' AND EXISTS(SELECT 1 FROM views slv WHERE slv.id=" + id + " AND slv.workspace_id=" + workspace + " AND " + publicMetadataAllowed("slv.workspace_id") + "))))"
}
