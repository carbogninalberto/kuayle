package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type PersonalAccessTokenRepository struct {
	db *sqlx.DB
}

func NewPersonalAccessTokenRepository(db *sqlx.DB) *PersonalAccessTokenRepository {
	return &PersonalAccessTokenRepository{db: db}
}

func (r *PersonalAccessTokenRepository) Create(ctx context.Context, token *domain.PersonalAccessToken) error {
	query := `INSERT INTO personal_access_tokens (id, user_id, name, token_hash, token_prefix, scopes, workspace_slugs, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		token.ID, token.UserID, token.Name, token.TokenHash, token.TokenPrefix,
		token.Scopes, token.WorkspaceSlugs, token.ExpiresAt,
	).Scan(&token.CreatedAt)
}

func (r *PersonalAccessTokenRepository) GetByHash(ctx context.Context, hash string) (*domain.PersonalAccessToken, error) {
	var token domain.PersonalAccessToken
	err := r.db.GetContext(ctx, &token, `SELECT * FROM personal_access_tokens WHERE token_hash = $1`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// ListByUser returns the user's non-revoked tokens, newest first.
func (r *PersonalAccessTokenRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error) {
	var tokens []domain.PersonalAccessToken
	err := r.db.SelectContext(ctx, &tokens, `SELECT * FROM personal_access_tokens WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	return tokens, err
}

// Revoke marks a token revoked. The user_id predicate prevents revoking other
// users' tokens; sql.ErrNoRows is returned when no matching active token exists.
func (r *PersonalAccessTokenRepository) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = NOW() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
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

func (r *PersonalAccessTokenRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE personal_access_tokens SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}
