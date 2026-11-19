package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type SignInTokenRepository struct {
	db *sqlx.DB
}

func NewSignInTokenRepository(db *sqlx.DB) *SignInTokenRepository {
	return &SignInTokenRepository{db: db}
}

const eligibleTelegramSignInToken = `
 FROM sign_in_tokens t
 JOIN workspaces w ON w.id=t.workspace_id
 JOIN workspace_members m ON m.workspace_id=t.workspace_id AND m.user_id=t.user_id
 JOIN telegram_links l ON l.workspace_id=t.workspace_id AND l.user_id=t.user_id
 JOIN telegram_settings s ON s.workspace_id=t.workspace_id
 WHERE t.token_hash=$1 AND w.slug=$2 AND t.purpose='telegram'
   AND t.used_at IS NULL AND t.expires_at>clock_timestamp()
   AND m.role<>'guest' AND s.enabled AND l.telegram_user_id=ANY(s.allowed_user_ids)`

func (r *SignInTokenRepository) User(ctx context.Context, hash, slug string) (uuid.UUID, error) {
	var user uuid.UUID
	err := r.db.GetContext(ctx, &user, `SELECT t.user_id`+eligibleTelegramSignInToken, hash, slug)
	return user, err
}

// Use rechecks eligibility and atomically records use and the session's refresh
// token. The UPDATE allows only one concurrent exchange to succeed.
func (r *SignInTokenRepository) Use(ctx context.Context, hash, slug string, user uuid.UUID, issue func(store func(context.Context, *RefreshToken) error) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id uuid.UUID
	err = tx.GetContext(ctx, &id, `WITH eligible AS (
 SELECT t.id`+eligibleTelegramSignInToken+` AND t.user_id=$3 FOR SHARE OF m,s
 ) UPDATE sign_in_tokens t SET used_at=clock_timestamp() FROM eligible e
 WHERE t.id=e.id AND t.used_at IS NULL AND t.expires_at>clock_timestamp() RETURNING t.id`, hash, slug, user)
	if err != nil {
		return err
	}
	if err := issue(func(ctx context.Context, token *RefreshToken) error {
		return createRefreshToken(ctx, tx, token)
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SignInTokenRepository) DeleteExpired(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sign_in_tokens WHERE expires_at<=now()`)
	return err
}
