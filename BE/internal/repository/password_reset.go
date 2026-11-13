package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type PasswordResetToken struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	TokenHash string    `db:"token_hash"`
	ExpiresAt time.Time `db:"expires_at"`
}

type PasswordResetRepository struct {
	db *sqlx.DB
}

func NewPasswordResetRepository(db *sqlx.DB) *PasswordResetRepository {
	return &PasswordResetRepository{db: db}
}

// Issue stores a new token for the user, replacing any outstanding one, unless a
// token was created within cooldown. The user row is locked so concurrent requests
// serialize and at most one of them wins; it reports whether the token was stored.
func (r *PasswordResetRepository) Issue(ctx context.Context, t *PasswordResetToken, cooldown time.Duration) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, t.UserID).Scan(&locked); err != nil {
		return false, err
	}
	var last sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT MAX(created_at) FROM password_reset_tokens WHERE user_id = $1`, t.UserID).Scan(&last); err != nil {
		return false, err
	}
	if last.Valid && time.Since(last.Time) < cooldown {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, t.UserID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		t.ID, t.UserID, t.TokenHash, t.ExpiresAt); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Reset atomically consumes an unexpired, unused token, sets the user's new
// password hash and revokes all of their sessions, passkeys and personal access tokens.
// It returns sql.ErrNoRows when the token is unknown, expired or already used.
func (r *PasswordResetRepository) Reset(ctx context.Context, hash, passwordHash string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`UPDATE password_reset_tokens SET used_at = NOW() WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW() RETURNING user_id`,
		hash).Scan(&userID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, passwordHash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
		return err
	}
	// A passkey is a credential in its own right: whoever took the account over
	// may have added one, and it must not outlive the recovery.
	if _, err := tx.ExecContext(ctx, `DELETE FROM passkey_credentials WHERE user_id = $1`, userID); err != nil {
		return err
	}
	// Personal access tokens authenticate without a session, so they would
	// otherwise survive the recovery of a compromised account.
	if _, err := tx.ExecContext(ctx, `UPDATE personal_access_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	// A connection link mints a token without a session, so recovery also
	// closes every open link before it can be redeemed again.
	if _, err := tx.ExecContext(ctx, `UPDATE cli_connection_links SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	return tx.Commit()
}
