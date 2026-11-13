package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Errors classifying why a connection-link code cannot be redeemed. They are
// deliberately distinct so the public endpoint can explain the state without
// revealing whether a guessed code ever existed.
var (
	ErrCLILinkInvalid = errors.New("connection link is invalid")
	ErrCLILinkExpired = errors.New("connection link has expired")
	ErrCLILinkUsed    = errors.New("connection link has already been used")
	ErrCLILinkRevoked = errors.New("connection link has been revoked")
)

type CLILinkRepository struct {
	db *sqlx.DB
}

func NewCLILinkRepository(db *sqlx.DB) *CLILinkRepository {
	return &CLILinkRepository{db: db}
}

func (r *CLILinkRepository) Create(ctx context.Context, link *domain.CLIConnectionLink) error {
	query := `INSERT INTO cli_connection_links
		(id, user_id, name, code_hash, code_prefix, scopes, workspace_slugs, token_expires_at, expires_at, max_uses)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING uses, created_at`
	return r.db.QueryRowContext(ctx, query,
		link.ID, link.UserID, link.Name, link.CodeHash, link.CodePrefix,
		link.Scopes, link.WorkspaceSlugs, link.TokenExpiresAt, link.ExpiresAt, link.MaxUses,
	).Scan(&link.Uses, &link.CreatedAt)
}

// GetByHash returns the link with the given code hash, or nil when none
// exists. It does not filter by state; callers decide which state is usable.
func (r *CLILinkRepository) GetByHash(ctx context.Context, hash string) (*domain.CLIConnectionLink, error) {
	var link domain.CLIConnectionLink
	err := r.db.GetContext(ctx, &link, `SELECT * FROM cli_connection_links WHERE code_hash = $1`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// ListActiveByUser returns the user's links that can still be redeemed,
// newest first.
func (r *CLILinkRepository) ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]domain.CLIConnectionLink, error) {
	var links []domain.CLIConnectionLink
	err := r.db.SelectContext(ctx, &links, `SELECT * FROM cli_connection_links
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW() AND uses < max_uses
		ORDER BY created_at DESC`, userID)
	return links, err
}

// Revoke blocks future redemptions. Tokens minted earlier are untouched.
func (r *CLILinkRepository) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cli_connection_links SET revoked_at = NOW() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Redeem consumes one use of the link and inserts the token the build
// function returns in the same transaction. The guarded UPDATE serializes
// concurrent redemptions: only a request that wins a per-link update may
// mint a token, so max_uses=1 never produces two tokens.
func (r *CLILinkRepository) Redeem(ctx context.Context, codeHash string, build func(link *domain.CLIConnectionLink) (*domain.PersonalAccessToken, error)) (*domain.PersonalAccessToken, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var link domain.CLIConnectionLink
	err = tx.GetContext(ctx, &link, `UPDATE cli_connection_links
		SET uses = uses + 1
		WHERE code_hash = $1 AND revoked_at IS NULL AND expires_at > NOW() AND uses < max_uses
		RETURNING *`, codeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyUnredeemable(ctx, tx, codeHash)
	}
	if err != nil {
		return nil, err
	}

	token, err := build(&link)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO personal_access_tokens
		(id, user_id, name, token_hash, token_prefix, scopes, workspace_slugs, expires_at, connection_link_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		token.ID, token.UserID, token.Name, token.TokenHash, token.TokenPrefix,
		token.Scopes, token.WorkspaceSlugs, token.ExpiresAt, token.ConnectionLinkID); err != nil {
		return nil, err
	}
	return token, tx.Commit()
}

func (r *CLILinkRepository) classifyUnredeemable(ctx context.Context, tx *sqlx.Tx, codeHash string) error {
	var link domain.CLIConnectionLink
	err := tx.GetContext(ctx, &link, `SELECT * FROM cli_connection_links WHERE code_hash = $1`, codeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCLILinkInvalid
	}
	if err != nil {
		return err
	}
	switch {
	case link.RevokedAt != nil:
		return ErrCLILinkRevoked
	case !time.Now().Before(link.ExpiresAt):
		return ErrCLILinkExpired
	case link.Uses >= link.MaxUses:
		return ErrCLILinkUsed
	default:
		return ErrCLILinkInvalid
	}
}
