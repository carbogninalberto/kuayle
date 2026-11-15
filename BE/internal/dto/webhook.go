package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// CreateWebhookRequest accepts only the event names Kuayle delivers
// (service.WebhookEvents); a test keeps them equal.
type CreateWebhookRequest struct {
	URL    string   `json:"url" validate:"required,url"`
	Secret string   `json:"secret" validate:"required,min=8"`
	Events []string `json:"events" validate:"required,min=1,dive,oneof=issue.created issue.updated issue.deleted issue.triaged comment.created comment.updated"`
}

// UpdateWebhookRequest changes the fields it names. Events, when sent, must
// list at least one event; the service, not a oneof rule, checks the names,
// because the webhook's stored list sent back unchanged is accepted even when
// it names an event Kuayle no longer delivers while a changed list may name
// only delivered events.
type UpdateWebhookRequest struct {
	URL      *string  `json:"url" validate:"omitempty,url"`
	Events   []string `json:"events" validate:"omitempty,min=1"`
	IsActive *bool    `json:"is_active"`
}

type WebhookResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WebhookDeliveryResponse is one entry of a webhook's delivery log. It never
// carries the payload or the webhook's URL.
type WebhookDeliveryResponse struct {
	ID       string `json:"id"`
	Event    string `json:"event"`
	Status   string `json:"status" validate:"oneof=pending delivered failed"`
	Attempts int    `json:"attempts"`
	// LastStatusCode is the receiver's HTTP status in the last attempt, null
	// when it gave none.
	LastStatusCode *int `json:"last_status_code"`
	// LastError is the code of the last failure, null after a success or
	// before the first attempt.
	LastError *string `json:"last_error"`
	// NextAttemptAt is when a pending delivery is tried next; null once it
	// is delivered or failed.
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at"`
}

// WebhookDelivery is the JSON body Kuayle posts to a webhook URL. The
// X-Webhook-Signature header signs these exact bytes.
type WebhookDelivery struct {
	Event     string      `json:"event"`
	Payload   interface{} `json:"payload"`
	Timestamp string      `json:"timestamp"`
}

// WebhookIssue is the issue object in webhook payloads: the stored issue
// with its workspace, number, workflow state and timestamps. It is a separate
// named type only so the OpenAPI document can describe it; its JSON is that of
// domain.Issue.
type WebhookIssue domain.Issue

// WebhookComment is the comment object in comment.created and
// comment.updated payloads; its JSON is that of domain.Comment.
type WebhookComment domain.Comment

// WebhookIssuePayload is the payload of issue.created, issue.updated,
// issue.deleted and issue.triaged. ActorID is null when automation made the
// change. For issue.deleted, Issue is the issue as it was before deletion.
type WebhookIssuePayload struct {
	Issue   WebhookIssue `json:"issue"`
	ActorID *uuid.UUID   `json:"actor_id"`
}

// WebhookIssueRef identifies the issue a comment belongs to.
type WebhookIssueRef struct {
	ID         uuid.UUID `json:"id"`
	Identifier string    `json:"identifier"`
	TeamID     uuid.UUID `json:"team_id"`
}

// WebhookCommentPayload is the payload of comment.created and
// comment.updated. ActorID is null when automation made the change.
type WebhookCommentPayload struct {
	Comment WebhookComment  `json:"comment"`
	Issue   WebhookIssueRef `json:"issue"`
	ActorID *uuid.UUID      `json:"actor_id"`
}
