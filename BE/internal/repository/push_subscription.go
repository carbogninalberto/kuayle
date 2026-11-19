package repository

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrPushSubscriptionConflict = errors.New("push subscription belongs to another enrollment")

type PushSubscriptionRepository struct {
	db *sqlx.DB
}

func NewPushSubscriptionRepository(db *sqlx.DB) *PushSubscriptionRepository {
	return &PushSubscriptionRepository{db: db}
}

func (r *PushSubscriptionRepository) Register(ctx context.Context, sub domain.PushSubscription, replaceID *uuid.UUID) (uuid.UUID, error) {
	if replaceID == nil {
		return upsertPushSubscription(ctx, r.db, sub)
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback()
	var previousEndpoint string
	err = tx.GetContext(ctx, &previousEndpoint, `SELECT endpoint FROM push_subscriptions
		WHERE id=$1 AND user_id=$2 AND workspace_id=$3 FOR UPDATE`, *replaceID, sub.UserID, sub.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrPushSubscriptionConflict
	}
	if err != nil {
		return uuid.Nil, err
	}
	id, err := upsertPushSubscription(ctx, tx, sub)
	if err != nil {
		return uuid.Nil, err
	}
	if previousEndpoint != sub.Endpoint {
		if _, err := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, *replaceID); err != nil {
			return uuid.Nil, err
		}
	}
	return id, tx.Commit()
}

func upsertPushSubscription(ctx context.Context, db sqlx.QueryerContext, sub domain.PushSubscription) (uuid.UUID, error) {
	var id uuid.UUID
	err := sqlx.GetContext(ctx, db, &id, `INSERT INTO push_subscriptions
		(user_id, workspace_id, endpoint, p256dh, auth, user_agent, sso_scoped)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (endpoint) DO UPDATE SET workspace_id=EXCLUDED.workspace_id, p256dh=EXCLUDED.p256dh,
		auth=EXCLUDED.auth, user_agent=EXCLUDED.user_agent, sso_scoped=EXCLUDED.sso_scoped,
		failure_count=0, last_success_at=NULL
		WHERE push_subscriptions.user_id=EXCLUDED.user_id
		AND (NOT EXCLUDED.sso_scoped OR push_subscriptions.workspace_id=EXCLUDED.workspace_id)
		RETURNING id`, sub.UserID, sub.WorkspaceID, sub.Endpoint, sub.P256DH, sub.Auth, sub.UserAgent, sub.SSOScoped)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrPushSubscriptionConflict
	}
	return id, err
}

func (r *PushSubscriptionRepository) Delete(ctx context.Context, userID, id uuid.UUID, ssoWorkspace *uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id=$1 AND user_id=$2
		AND ($3::uuid IS NULL OR (sso_scoped AND workspace_id=$3))`, id, userID, ssoWorkspace)
	return err
}

func (r *PushSubscriptionRepository) CountByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, "SELECT count(*) FROM push_subscriptions WHERE user_id=$1", userID)
	return count, err
}

func (r *PushSubscriptionRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PushSubscription, error) {
	var subscriptions []domain.PushSubscription
	err := r.db.SelectContext(ctx, &subscriptions, `SELECT * FROM push_subscriptions WHERE user_id=$1 ORDER BY created_at, id`, userID)
	return subscriptions, err
}

func (r *PushSubscriptionRepository) RecordDelivery(ctx context.Context, sub domain.PushSubscription, status int) error {
	// A send using stale subscription keys must not update or remove their replacement.
	where := ` WHERE id=$1 AND p256dh=$2 AND auth=$3`
	query := `UPDATE push_subscriptions SET failure_count=failure_count+1`
	switch {
	case status == http.StatusNotFound || status == http.StatusGone:
		query = `DELETE FROM push_subscriptions`
	case status >= 200 && status < 300:
		query = `UPDATE push_subscriptions SET failure_count=0, last_success_at=NOW()`
	}
	_, err := r.db.ExecContext(ctx, query+where, sub.ID, sub.P256DH, sub.Auth)
	return err
}
