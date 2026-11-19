package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type RefreshToken struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	TokenHash string    `db:"token_hash"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
	// AuthenticatedAt is when the user signed in; rotation carries it over.
	AuthenticatedAt *time.Time `db:"authenticated_at"`
	// PasskeyCredentialID is the passkey that minted the session, if any. The
	// row is deleted with that passkey.
	PasskeyCredentialID *uuid.UUID `db:"passkey_credential_id"`
	// SSOWorkspaceID is the workspace whose SSO minted the session, if any.
	// Rotation carries it over for require-SSO enforcement.
	SSOWorkspaceID  *uuid.UUID `db:"sso_workspace_id"`
	SessionVersion  int64      `db:"session_version"`
	ExpectedVersion *int64     `db:"-"`
}

type RefreshTokenRepository struct {
	db *sqlx.DB
}

func NewRefreshTokenRepository(db *sqlx.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, rt *RefreshToken) error {
	// Serialize session issuance with deprovisioning. A refresh also has to
	// carry the original session version across rotation.
	query := `WITH scope AS MATERIALIZED (
			SELECT user_id FROM workspace_members WHERE workspace_id=$7 AND user_id=$2 FOR SHARE
		), account AS (
			SELECT session_version FROM users WHERE id=$2 AND ($7::uuid IS NULL OR EXISTS (SELECT 1 FROM scope)) FOR SHARE
		)
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, authenticated_at, passkey_credential_id, sso_workspace_id, session_version)
		SELECT $1, $2, $3, $4, $5, $6, $7, session_version FROM account
		WHERE ($8::bigint IS NULL OR session_version=$8)
		RETURNING created_at, session_version`
	return r.db.QueryRowContext(ctx, query, rt.ID, rt.UserID, rt.TokenHash, rt.ExpiresAt, rt.AuthenticatedAt,
		rt.PasskeyCredentialID, rt.SSOWorkspaceID, rt.ExpectedVersion).Scan(&rt.CreatedAt, &rt.SessionVersion)
}

func (r *RefreshTokenRepository) SessionCurrent(ctx context.Context, userID uuid.UUID, version int64) (bool, error) {
	var current bool
	err := r.db.GetContext(ctx, &current, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1 AND session_version=$2)`, userID, version)
	return current, err
}

func (r *RefreshTokenRepository) GetByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	var rt RefreshToken
	err := r.db.GetContext(ctx, &rt, `SELECT id, user_id, token_hash, expires_at, created_at, authenticated_at, passkey_credential_id, sso_workspace_id, session_version FROM refresh_tokens WHERE token_hash = $1 AND expires_at > NOW()`, hash)
	if err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *RefreshTokenRepository) DeleteByHash(ctx context.Context, hash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, hash)
	return err
}

func (r *RefreshTokenRepository) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID)
	return err
}

func (r *RefreshTokenRepository) DeleteExpired(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < NOW()`)
	return err
}
