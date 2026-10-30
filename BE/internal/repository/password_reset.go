package repository

import (
	"context"
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

// Create replaces any outstanding tokens for the user so only the latest link works.
func (r *PasswordResetRepository) Create(ctx context.Context, t *PasswordResetToken) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, t.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		t.ID, t.UserID, t.TokenHash, t.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

// Reset atomically consumes an unexpired, unused token, sets the user's new
// password hash and revokes all of their sessions. It returns sql.ErrNoRows when
// the token is unknown, expired or already used.
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
	return tx.Commit()
}
