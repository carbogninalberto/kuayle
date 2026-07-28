package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type WorkspaceInviteLinkRepository struct {
	db *sqlx.DB
}

func NewWorkspaceInviteLinkRepository(db *sqlx.DB) *WorkspaceInviteLinkRepository {
	return &WorkspaceInviteLinkRepository{db: db}
}

func (r *WorkspaceInviteLinkRepository) Create(ctx context.Context, link *domain.WorkspaceInviteLink) error {
	query := `INSERT INTO workspace_invite_links (id, workspace_id, token_hash, role, created_by, expires_at, max_uses)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING use_count, created_at`
	return r.db.QueryRowContext(ctx, query,
		link.ID, link.WorkspaceID, link.TokenHash, link.Role, link.CreatedBy, link.ExpiresAt, link.MaxUses,
	).Scan(&link.UseCount, &link.CreatedAt)
}

func (r *WorkspaceInviteLinkRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.WorkspaceInviteLink, error) {
	var link domain.WorkspaceInviteLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM workspace_invite_links WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *WorkspaceInviteLinkRepository) GetByTokenHash(ctx context.Context, hash string) (*domain.WorkspaceInviteLink, error) {
	var link domain.WorkspaceInviteLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM workspace_invite_links WHERE token_hash = $1`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *WorkspaceInviteLinkRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceInviteLink, error) {
	links := make([]domain.WorkspaceInviteLink, 0)
	err := r.db.SelectContext(ctx, &links,
		`SELECT * FROM workspace_invite_links WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	return links, nil
}

func (r *WorkspaceInviteLinkRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE workspace_invite_links SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

// TryConsumeUse atomically increments use_count when the link has remaining
// uses. It reports false when max_uses is already reached.
func (r *WorkspaceInviteLinkRepository) TryConsumeUse(ctx context.Context, id uuid.UUID) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE workspace_invite_links SET use_count = use_count + 1
		WHERE id = $1 AND (max_uses IS NULL OR use_count < max_uses)`, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ReleaseUse decrements use_count after a consumed use could not be completed
// (e.g. AddMember failed). Best-effort compensation for TryConsumeUse.
func (r *WorkspaceInviteLinkRepository) ReleaseUse(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE workspace_invite_links SET use_count = use_count - 1 WHERE id = $1 AND use_count > 0`, id)
	return err
}
