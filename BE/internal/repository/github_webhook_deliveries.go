package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ClaimWebhookDelivery records a GitHub webhook delivery and reports whether
// the caller may process it. It refuses a delivery that was already processed
// and one another request is still handling; a claim that was never finished
// (the server stopped midway) can be taken over once it is older than lease.
func (r *GitHubRepository) ClaimWebhookDelivery(ctx context.Context, installationID int64, deliveryID string, lease time.Duration) (bool, error) {
	var claimed bool
	err := r.db.QueryRowContext(ctx, `INSERT INTO github_webhook_deliveries(installation_id,delivery_id) VALUES($1,$2)
 ON CONFLICT (installation_id,delivery_id) DO UPDATE SET claimed_at=now()
 WHERE github_webhook_deliveries.processed_at IS NULL AND github_webhook_deliveries.claimed_at < now() - make_interval(secs => $3)
 RETURNING true`, installationID, deliveryID, lease.Seconds()).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return claimed, err
}

// CompleteWebhookDelivery marks a claimed delivery as processed, so it is
// refused until the marker expires.
func (r *GitHubRepository) CompleteWebhookDelivery(ctx context.Context, installationID int64, deliveryID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE github_webhook_deliveries SET processed_at=now() WHERE installation_id=$1 AND delivery_id=$2`, installationID, deliveryID)
	return err
}

// ReleaseWebhookDelivery forgets a claim whose processing failed, so GitHub
// can redeliver it under the same delivery ID.
func (r *GitHubRepository) ReleaseWebhookDelivery(ctx context.Context, installationID int64, deliveryID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM github_webhook_deliveries WHERE installation_id=$1 AND delivery_id=$2 AND processed_at IS NULL`, installationID, deliveryID)
	return err
}

// DeleteExpiredWebhookDeliveries removes delivery markers older than retention.
func (r *GitHubRepository) DeleteExpiredWebhookDeliveries(ctx context.Context, retention time.Duration) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM github_webhook_deliveries WHERE claimed_at < now() - make_interval(secs => $1)`, retention.Seconds())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
