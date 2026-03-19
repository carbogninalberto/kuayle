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
}

type RefreshTokenRepository struct {
	db *sqlx.DB
}

func NewRefreshTokenRepository(db *sqlx.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, rt *RefreshToken) error {
	query := `INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, rt.ID, rt.UserID, rt.TokenHash, rt.ExpiresAt).Scan(&rt.CreatedAt)
}

func (r *RefreshTokenRepository) GetByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	var rt RefreshToken
	err := r.db.GetContext(ctx, &rt, `SELECT * FROM refresh_tokens WHERE token_hash = $1 AND expires_at > NOW()`, hash)
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
