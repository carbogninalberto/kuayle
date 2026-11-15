package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

type WebhookRepository struct {
	db *sqlx.DB
}

func NewWebhookRepository(db *sqlx.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

// Create stores a webhook. A workspace that has or had a private team
// delivers no webhooks, so it cannot get a new one: ErrPrivateWorkspaceOperation.
func (r *WebhookRepository) Create(ctx context.Context, w *domain.Webhook) error {
	query := `INSERT INTO webhooks (id, workspace_id, url, secret, events, is_active) SELECT $1, $2, $3, $4, $5, $6 WHERE ` +
		publicMetadataAllowed("CAST($2 AS uuid)") + ` RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query, w.ID, w.WorkspaceID, w.URL, w.Secret, w.Events, w.IsActive).Scan(&w.CreatedAt, &w.UpdatedAt)
	return privacyDenial(ctx, r.db, w.WorkspaceID, err)
}

func (r *WebhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Webhook, error) {
	var w domain.Webhook
	err := r.db.GetContext(ctx, &w, `SELECT * FROM webhooks WHERE id = $1 AND `+workspaceVisible(ctx, "webhooks.workspace_id"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &w, err
}

func (r *WebhookRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Webhook, error) {
	var webhooks []domain.Webhook
	err := r.db.SelectContext(ctx, &webhooks, `SELECT * FROM webhooks WHERE workspace_id = $1 AND `+workspaceVisible(ctx, "webhooks.workspace_id")+` ORDER BY created_at DESC`, workspaceID)
	return webhooks, err
}

// ListDeliverable returns the active webhooks of the workspace that subscribe
// to event, oldest first. It returns none while the workspace is in privacy
// mode (it has or had a private team): the cheap precheck before deliveries
// are stored. ClaimDelivery checks again, under the privacy guard, when one
// is sent.
func (r *WebhookRepository) ListDeliverable(ctx context.Context, workspaceID uuid.UUID, event string) ([]domain.Webhook, error) {
	var webhooks []domain.Webhook
	err := r.db.SelectContext(ctx, &webhooks, `SELECT * FROM webhooks WHERE workspace_id = $1 AND is_active AND $2 = ANY(events) AND `+
		workspaceVisible(ctx, "webhooks.workspace_id")+` AND `+publicMetadataAllowed("webhooks.workspace_id")+` ORDER BY created_at, id`, workspaceID, event)
	return webhooks, err
}

// Update writes the webhook's URL, events and active state. Enabling an
// inactive webhook in a workspace that has or had a private team is refused
// with ErrPrivateWorkspaceOperation; other changes are allowed there.
func (r *WebhookRepository) Update(ctx context.Context, w *domain.Webhook) error {
	query := `UPDATE webhooks SET url = $1, events = $2, is_active = $3, updated_at = NOW() WHERE id = $4 AND ` + workspaceVisible(ctx, "webhooks.workspace_id") +
		` AND (NOT CAST($3 AS boolean) OR webhooks.is_active OR ` + publicMetadataAllowed("webhooks.workspace_id") + `) RETURNING updated_at`
	err := r.db.QueryRowContext(ctx, query, w.URL, w.Events, w.IsActive, w.ID).Scan(&w.UpdatedAt)
	return privacyDenial(ctx, r.db, w.WorkspaceID, err)
}

func (r *WebhookRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = $1 AND `+workspaceVisible(ctx, "webhooks.workspace_id"), id)
	return err
}

func (r *WebhookRepository) BeginPublicOperation(ctx context.Context, workspaceID uuid.UUID) (*sqlx.Tx, error) {
	return beginPublicWorkspaceOperation(ctx, r.db, workspaceID)
}

// WebhookDeliveryLimits bounds the pending deliveries, counted in the store so
// they hold across instances. A delivery's body counts once per webhook.
type WebhookDeliveryLimits struct {
	Jobs, Bytes                   int // pending in all workspaces
	WorkspaceJobs, WorkspaceBytes int // pending in one workspace
	WebhookJobs                   int // pending for one webhook
}

// NewWebhookDelivery is one delivery to store: the body for one webhook.
type NewWebhookDelivery struct {
	WebhookID uuid.UUID
	Body      []byte
}

// EnqueueDeliveries stores the deliveries of one event in a workspace, due
// now, in order and as far as the limits allow, and returns how many it
// stored. Emitters of one workspace take turns, so the workspace and webhook
// bounds are exact; the bounds for all workspaces may be exceeded by
// workspaces that emit at the same moment.
func (r *WebhookRepository) EnqueueDeliveries(ctx context.Context, workspaceID uuid.UUID, event string, deliveries []NewWebhookDelivery, limits WebhookDeliveryLimits) (int, error) {
	if len(deliveries) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('webhook_deliveries:' || $1::text, 0))`, workspaceID); err != nil {
		return 0, err
	}
	type backlog struct {
		Jobs  int   `db:"jobs"`
		Bytes int64 `db:"bytes"`
	}
	var all, workspace backlog
	if err = tx.GetContext(ctx, &all, `SELECT count(*) AS jobs, COALESCE(sum(octet_length(body)), 0) AS bytes FROM webhook_deliveries WHERE status = 'pending'`); err != nil {
		return 0, err
	}
	if err = tx.GetContext(ctx, &workspace, `SELECT count(*) AS jobs, COALESCE(sum(octet_length(body)), 0) AS bytes FROM webhook_deliveries WHERE status = 'pending' AND workspace_id = $1`, workspaceID); err != nil {
		return 0, err
	}
	var perHook []struct {
		WebhookID uuid.UUID `db:"webhook_id"`
		Jobs      int       `db:"jobs"`
	}
	if err = tx.SelectContext(ctx, &perHook, `SELECT webhook_id, count(*) AS jobs FROM webhook_deliveries WHERE status = 'pending' AND workspace_id = $1 GROUP BY webhook_id`, workspaceID); err != nil {
		return 0, err
	}
	hookJobs := make(map[uuid.UUID]int, len(perHook))
	for _, hook := range perHook {
		hookJobs[hook.WebhookID] = hook.Jobs
	}

	ids := make(pq.StringArray, 0, len(deliveries))
	bodies := make(pq.StringArray, 0, len(deliveries))
	for _, delivery := range deliveries {
		size := int64(len(delivery.Body))
		if all.Jobs+1 > limits.Jobs || all.Bytes+size > int64(limits.Bytes) ||
			workspace.Jobs+1 > limits.WorkspaceJobs || workspace.Bytes+size > int64(limits.WorkspaceBytes) ||
			hookJobs[delivery.WebhookID]+1 > limits.WebhookJobs {
			continue
		}
		all.Jobs++
		all.Bytes += size
		workspace.Jobs++
		workspace.Bytes += size
		hookJobs[delivery.WebhookID]++
		ids = append(ids, delivery.WebhookID.String())
		bodies = append(bodies, string(delivery.Body))
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO webhook_deliveries (webhook_id, workspace_id, event, body, next_attempt_at)
		SELECT d.webhook_id, $2, $3, d.body, now() FROM unnest(CAST($1 AS uuid[]), CAST($4 AS text[])) WITH ORDINALITY AS d(webhook_id, body, n) ORDER BY d.n`,
		ids, workspaceID, event, bodies); err != nil {
		return 0, err
	}
	// The lane usually exists; checking first avoids waiting on a lane a
	// worker holds.
	if _, err = tx.ExecContext(ctx, `INSERT INTO webhook_lanes (workspace_id) SELECT $1 WHERE NOT EXISTS (SELECT 1 FROM webhook_lanes WHERE workspace_id = $1) ON CONFLICT DO NOTHING`, workspaceID); err != nil {
		return 0, err
	}
	return len(ids), tx.Commit()
}

// Codes stored as last_error when a delivery is given up without being sent.
const (
	WebhookErrorWorkspacePrivate = "workspace_private"
	WebhookErrorExpired          = "expired"
	WebhookErrorWebhookDisabled  = "webhook_disabled"
	WebhookErrorUnsubscribed     = "unsubscribed"
)

// WebhookDeliveryJob is the delivery a worker sends.
type WebhookDeliveryJob struct {
	ID        uuid.UUID `db:"id"`
	WebhookID uuid.UUID `db:"webhook_id"`
	Event     string    `db:"event"`
	Body      string    `db:"body"`
	Attempts  int       `db:"attempts"`
	URL       string    `db:"url"`
	Secret    string    `db:"secret"`
}

// WebhookDeliveryClaim is one turn of a workspace: the claim holds the
// workspace's lane and its shared privacy guard until Finish or Release, so
// no other worker in any instance sends a delivery of the workspace
// meanwhile, and a privacy transition waits for the delivery in flight. A
// claim that ends without Finish leaves its delivery pending, as if it had
// not been taken.
type WebhookDeliveryClaim struct {
	tx          *sqlx.Tx
	WorkspaceID uuid.UUID
	// Private counts the deliveries given up because the workspace has or
	// had a private team; nothing is sent then.
	Private int64
	// Expired counts the deliveries given up because they waited longer than
	// the maximum age.
	Expired int64
	// Skipped is the code of a delivery given up because its webhook was
	// disabled or no longer subscribes to the event, and SkippedWebhook its
	// webhook.
	Skipped        string
	SkippedWebhook uuid.UUID
	// Delivery is the delivery to send, nil when the turn sends nothing.
	Delivery *WebhookDeliveryJob
}

// ClaimDelivery takes the turn of the workspace whose turn is oldest among
// those with a due delivery to a webhook that is not paused, skipping
// workspaces another worker holds. It returns nil when there is none. In the
// turn, it gives up the workspace's pending deliveries when the workspace is
// private, then those older than maxAge, then picks the workspace's earliest
// due delivery. ctx bounds the whole turn, including the send: canceling it
// releases the claim.
func (r *WebhookRepository) ClaimDelivery(ctx context.Context, maxAge time.Duration) (*WebhookDeliveryClaim, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	claim := &WebhookDeliveryClaim{tx: tx}
	err = tx.GetContext(ctx, &claim.WorkspaceID, `SELECT l.workspace_id FROM webhook_lanes l
		WHERE EXISTS (SELECT 1 FROM webhook_deliveries d LEFT JOIN webhook_circuits c ON c.webhook_id = d.webhook_id
			WHERE d.workspace_id = l.workspace_id AND d.status = 'pending' AND d.next_attempt_at <= now()
				AND (c.paused_until IS NULL OR c.paused_until <= now()))
		ORDER BY l.last_turn_at NULLS FIRST, l.workspace_id
		LIMIT 1 FOR UPDATE OF l SKIP LOCKED`)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err = r.claimTurn(ctx, claim, maxAge); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if claim.Delivery == nil {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		claim.tx = nil
	}
	return claim, nil
}

const giveUpDelivery = `UPDATE webhook_deliveries SET status = 'failed', last_error = $2, body = '', next_attempt_at = NULL,
	completed_at = now(), updated_at = now() WHERE workspace_id = $1 AND status = 'pending'`

func (r *WebhookRepository) claimTurn(ctx context.Context, claim *WebhookDeliveryClaim, maxAge time.Duration) error {
	tx, ws := claim.tx, claim.WorkspaceID
	if _, err := tx.ExecContext(ctx, `UPDATE webhook_lanes SET last_turn_at = now() WHERE workspace_id = $1`, ws); err != nil {
		return err
	}
	err := lockPublicWorkspace(ctx, tx, ws)
	if errors.Is(err, ErrPrivateWorkspaceOperation) {
		claim.Private, err = rowsAffected(tx.ExecContext(ctx, giveUpDelivery, ws, WebhookErrorWorkspacePrivate))
		return err
	}
	if err != nil {
		return err
	}
	if claim.Expired, err = rowsAffected(tx.ExecContext(ctx, giveUpDelivery+` AND created_at < now() - make_interval(secs => $3)`,
		ws, WebhookErrorExpired, maxAge.Seconds())); err != nil {
		return err
	}
	var next struct {
		WebhookDeliveryJob
		Active     bool `db:"is_active"`
		Subscribed bool `db:"subscribed"`
	}
	// FOR KEY SHARE keeps the webhook until the turn ends: deleting it
	// waits for the delivery in flight instead of racing its result.
	err = tx.GetContext(ctx, &next, `SELECT d.id, d.webhook_id, d.event, d.body, d.attempts, h.url, h.secret, h.is_active, d.event = ANY(h.events) AS subscribed
		FROM webhook_deliveries d JOIN webhooks h ON h.id = d.webhook_id LEFT JOIN webhook_circuits c ON c.webhook_id = d.webhook_id
		WHERE d.workspace_id = $1 AND d.status = 'pending' AND d.next_attempt_at <= now() AND (c.paused_until IS NULL OR c.paused_until <= now())
		ORDER BY d.seq LIMIT 1 FOR KEY SHARE OF h`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !next.Active || !next.Subscribed {
		claim.Skipped, claim.SkippedWebhook = WebhookErrorWebhookDisabled, next.WebhookID
		if next.Active {
			claim.Skipped = WebhookErrorUnsubscribed
		}
		_, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET status = 'failed', last_error = $2, body = '', next_attempt_at = NULL,
			completed_at = now(), updated_at = now() WHERE id = $1`, next.ID, claim.Skipped)
		return err
	}
	claim.Delivery = &next.WebhookDeliveryJob
	return nil
}

func rowsAffected(result sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// WebhookAttempt is the outcome of sending a claimed delivery.
type WebhookAttempt struct {
	// Sent reports that a request reached the network, so it counts as an
	// attempt.
	Sent       bool
	Delivered  bool
	StatusCode int    // the receiver's status, 0 when it gave none
	Error      string // the failure code, empty when delivered
	// RetryIn keeps a failed delivery pending for another attempt after it;
	// zero gives the delivery up.
	RetryIn time.Duration
	// Breaker reports that the outcome says something about the receiver:
	// a success closes the webhook's circuit and a failure counts toward
	// pausing it.
	Breaker bool
}

// WebhookBreaker pauses a webhook for Cooldown after Threshold consecutive
// failed deliveries.
type WebhookBreaker struct {
	Threshold int
	Cooldown  time.Duration
}

// Finish records the attempt, updates the webhook's circuit and ends the
// turn. It reports the consecutive failures when this failure paused the
// webhook, zero otherwise. Times are taken when Finish runs, not when the
// turn began.
func (c *WebhookDeliveryClaim) Finish(ctx context.Context, attempt WebhookAttempt, breaker WebhookBreaker) (pausedAfter int, err error) {
	if c.tx == nil || c.Delivery == nil {
		return 0, errors.New("webhook delivery claim has nothing to finish")
	}
	tx := c.tx
	defer func() {
		_ = tx.Rollback()
		c.tx = nil
	}()
	if attempt.Breaker && attempt.Delivered {
		if _, err = tx.ExecContext(ctx, `DELETE FROM webhook_circuits WHERE webhook_id = $1`, c.Delivery.WebhookID); err != nil {
			return 0, err
		}
	} else if attempt.Breaker {
		var failures int
		if err = tx.GetContext(ctx, &failures, `INSERT INTO webhook_circuits (webhook_id, consecutive_failures, paused_until)
			VALUES ($1, 1, CASE WHEN 1 >= $2 THEN clock_timestamp() + make_interval(secs => $3) END)
			ON CONFLICT (webhook_id) DO UPDATE SET consecutive_failures = webhook_circuits.consecutive_failures + 1,
				paused_until = CASE WHEN webhook_circuits.consecutive_failures + 1 >= $2 THEN clock_timestamp() + make_interval(secs => $3) ELSE webhook_circuits.paused_until END
			RETURNING consecutive_failures`, c.Delivery.WebhookID, breaker.Threshold, breaker.Cooldown.Seconds()); err != nil {
			return 0, err
		}
		if failures >= breaker.Threshold {
			pausedAfter = failures
		}
	}
	sent := 0
	if attempt.Sent {
		sent = 1
	}
	var status *int
	if attempt.StatusCode != 0 {
		status = &attempt.StatusCode
	}
	var lastError *string
	if attempt.Error != "" {
		lastError = &attempt.Error
	}
	switch {
	case attempt.Delivered:
		_, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET status = 'delivered', attempts = attempts + $2, last_status_code = $3, last_error = NULL,
			body = '', next_attempt_at = NULL, completed_at = clock_timestamp(), updated_at = clock_timestamp() WHERE id = $1`, c.Delivery.ID, sent, status)
	case attempt.RetryIn > 0:
		_, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET attempts = attempts + $2, last_status_code = $3, last_error = $4,
			next_attempt_at = clock_timestamp() + make_interval(secs => $5), updated_at = clock_timestamp() WHERE id = $1`,
			c.Delivery.ID, sent, status, lastError, attempt.RetryIn.Seconds())
	default:
		_, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET status = 'failed', attempts = attempts + $2, last_status_code = $3, last_error = $4,
			body = '', next_attempt_at = NULL, completed_at = clock_timestamp(), updated_at = clock_timestamp() WHERE id = $1`, c.Delivery.ID, sent, status, lastError)
	}
	if err != nil {
		return 0, err
	}
	return pausedAfter, tx.Commit()
}

// Release ends the turn without recording anything: the delivery stays
// pending, as if it had not been taken. It is safe after Finish.
func (c *WebhookDeliveryClaim) Release() {
	if c.tx != nil {
		_ = c.tx.Rollback()
		c.tx = nil
	}
}

// ListDeliveries returns the webhook's most recent deliveries, newest first,
// when the webhook is visible to ctx.
func (r *WebhookRepository) ListDeliveries(ctx context.Context, webhookID uuid.UUID, limit int) ([]domain.WebhookDelivery, error) {
	deliveries := []domain.WebhookDelivery{}
	err := r.db.SelectContext(ctx, &deliveries, `SELECT d.id, d.webhook_id, d.event, d.status, d.attempts, d.next_attempt_at, d.last_status_code, d.last_error,
		d.created_at, d.updated_at, d.completed_at
		FROM webhook_deliveries d JOIN webhooks h ON h.id = d.webhook_id
		WHERE d.webhook_id = $1 AND `+workspaceVisible(ctx, "h.workspace_id")+` ORDER BY d.seq DESC LIMIT $2`, webhookID, limit)
	return deliveries, err
}

// PurgeDeliveries deletes finished deliveries older than retention, in
// batches, and returns how many it deleted. Pending deliveries are kept.
func (r *WebhookRepository) PurgeDeliveries(ctx context.Context, retention time.Duration, batch int) (int, error) {
	total := 0
	for {
		n, err := rowsAffected(r.db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE id IN (
			SELECT id FROM webhook_deliveries WHERE status <> 'pending' AND completed_at < now() - make_interval(secs => $1) ORDER BY completed_at LIMIT $2)`,
			retention.Seconds(), batch))
		total += int(n)
		if err != nil || int(n) < batch {
			return total, err
		}
	}
}
