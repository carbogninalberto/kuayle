package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

var (
	ErrInviteLinkInvalid   = errors.New("invite link is invalid")
	ErrInviteLinkExpired   = errors.New("invite link has expired")
	ErrInviteLinkRevoked   = errors.New("invite link has been revoked")
	ErrInviteLinkExhausted = errors.New("invite link has reached its maximum uses")
)

// Join commits membership and invite consumption together. During signup it also
// creates the account in this transaction, so unusable invites cannot create users.
func (r *WorkspaceInviteLinkRepository) Join(ctx context.Context, id, userID uuid.UUID, newUser *domain.User) (string, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize joins with other consumers and revocation. Read the current row
	// after acquiring the lock rather than trusting the service's earlier preview.
	var link domain.WorkspaceInviteLink
	err = tx.GetContext(ctx, &link, `SELECT * FROM workspace_invite_links WHERE id = $1 FOR UPDATE`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInviteLinkInvalid
	}
	if err != nil {
		return "", err
	}

	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, link.WorkspaceID, userID)
	if err == nil {
		return role, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	// clock_timestamp(), unlike NOW(), advances while a transaction waits on a lock.
	var now time.Time
	if err := tx.GetContext(ctx, &now, `SELECT clock_timestamp()`); err != nil {
		return "", err
	}
	switch {
	case link.RevokedAt != nil:
		return "", ErrInviteLinkRevoked
	case !now.Before(link.ExpiresAt):
		return "", ErrInviteLinkExpired
	case link.MaxUses != nil && link.UseCount >= *link.MaxUses:
		return "", ErrInviteLinkExhausted
	}

	if newUser != nil {
		err := tx.QueryRowContext(ctx, `INSERT INTO users (id, email, name, display_name, password_hash)
			VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`,
			newUser.ID, newUser.Email, newUser.Name, newUser.DisplayName, newUser.PasswordHash).Scan(&newUser.CreatedAt, &newUser.UpdatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return "", ErrDuplicateEmail
			}
			return "", err
		}
	}

	// Concurrent membership via a different invite or the email flow must preserve
	// its role and consume no use. ON CONFLICT also waits for that transaction.
	result, err := tx.ExecContext(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, $3) ON CONFLICT (workspace_id, user_id) DO NOTHING`, link.WorkspaceID, userID, link.Role)
	if err != nil {
		return "", err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if inserted == 0 {
		if err := tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, link.WorkspaceID, userID); err != nil {
			return "", err
		}
	} else {
		role = link.Role
		if _, err := tx.ExecContext(ctx, `UPDATE workspace_invite_links SET use_count = use_count + 1 WHERE id = $1`, id); err != nil {
			return "", err
		}
	}
	return role, tx.Commit()
}
