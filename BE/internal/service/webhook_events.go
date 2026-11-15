package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Webhook event names Kuayle delivers. They are a public contract: the
// settings page offers them and the API guide documents their payloads.
const (
	WebhookEventIssueCreated   = "issue.created"
	WebhookEventIssueUpdated   = "issue.updated"
	WebhookEventIssueDeleted   = "issue.deleted"
	WebhookEventIssueTriaged   = "issue.triaged"
	WebhookEventCommentCreated = "comment.created"
)

// WebhookEvents lists every event name that is delivered, in display order.
var WebhookEvents = []string{
	WebhookEventIssueCreated,
	WebhookEventIssueUpdated,
	WebhookEventIssueDeleted,
	WebhookEventIssueTriaged,
	WebhookEventCommentCreated,
}

// webhookEmitter is what write paths call after a change is committed. It
// never blocks on delivery and never returns an error: a failed delivery must
// not fail the user's write. *WebhookService implements it.
//
// actorID is the user who made the change, or uuid.Nil for automation
// (GitHub/forge transitions, status automation run by the system).
type webhookEmitter interface {
	// IssueEvent sends issue.created, issue.updated, issue.deleted or
	// issue.triaged. For issue.deleted, issue is the issue as it was just
	// before deletion.
	IssueEvent(ctx context.Context, workspaceID uuid.UUID, event string, issue *domain.Issue, actorID uuid.UUID)
	// IssueEvents sends event for several issues of one workspace, listing
	// the workspace's webhooks once. Bulk operations use it instead of a
	// loop of IssueEvent calls.
	IssueEvents(ctx context.Context, workspaceID uuid.UUID, event string, issues []domain.Issue, actorID uuid.UUID)
	// CommentCreated sends comment.created for a comment on issue.
	CommentCreated(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, comment *domain.Comment, actorID uuid.UUID)
}
