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
	Events []string `json:"events" validate:"required,min=1,dive,oneof=issue.created issue.updated issue.deleted issue.triaged comment.created"`
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

// WebhookComment is the comment object in comment.created payloads; its JSON
// is that of domain.Comment.
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

// WebhookCommentPayload is the payload of comment.created. ActorID is null
// when automation posted the comment.
type WebhookCommentPayload struct {
	Comment WebhookComment  `json:"comment"`
	Issue   WebhookIssueRef `json:"issue"`
	ActorID *uuid.UUID      `json:"actor_id"`
}
