package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Webhook struct {
	ID          uuid.UUID      `json:"id" db:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id" db:"workspace_id"`
	URL         string         `json:"url" db:"url"`
	Secret      string         `json:"-" db:"secret"`
	Events      pq.StringArray `json:"events" db:"events"`
	IsActive    bool           `json:"is_active" db:"is_active"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// Webhook delivery statuses. A delivery is pending until it is sent or given
// up.
const (
	WebhookDeliveryPending   = "pending"
	WebhookDeliveryDelivered = "delivered"
	WebhookDeliveryFailed    = "failed"
)

// WebhookDelivery is one stored delivery of a change to a webhook, as the
// delivery log shows it: never its payload or the webhook's URL.
type WebhookDelivery struct {
	ID        uuid.UUID `db:"id"`
	WebhookID uuid.UUID `db:"webhook_id"`
	Event     string    `db:"event"`
	Status    string    `db:"status"`
	Attempts  int       `db:"attempts"`
	// NextAttemptAt is set while the delivery is pending.
	NextAttemptAt *time.Time `db:"next_attempt_at"`
	// LastStatusCode is the receiver's HTTP status in the last attempt, nil
	// when it gave none.
	LastStatusCode *int `db:"last_status_code"`
	// LastError is a short code for the last failure (see the API guide),
	// nil after a success or before the first attempt.
	LastError   *string    `db:"last_error"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	CompletedAt *time.Time `db:"completed_at"`
}
