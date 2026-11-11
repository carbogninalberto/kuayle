package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
)

// notificationCreator stores a notification for its recipient, refusing one
// the recipient may not see. NotificationService implements it.
type notificationCreator interface {
	Create(ctx context.Context, userID, workspaceID uuid.UUID, issueID *uuid.UUID, notifType, title string, sourceTeams ...uuid.UUID) error
}

// userEventPublisher delivers an event to one user's connections in a
// workspace. The realtime hub implements it.
type userEventPublisher interface {
	BroadcastToUser(workspaceID, userID uuid.UUID, event realtime.Event)
}

// notificationEvent describes a stored notification to its recipient's open
// clients, so they can show it without reading the inbox first.
type notificationEvent struct {
	Type  string
	Title string
	// IssueID and IssueIdentifier link the notification to an issue the
	// recipient can open; both are omitted when there is none.
	IssueID         *uuid.UUID
	IssueIdentifier string
	// ActorID is the user whose action caused the notification, when a person
	// did; clients use it to stay quiet about the recipient's own actions.
	ActorID *uuid.UUID
	// LiveOnly marks an event that has no stored inbox row behind it, so
	// clients announce it without counting it as unread.
	LiveOnly bool
	// Extra adds the fields of one notification type, such as task_id.
	Extra map[string]string
}

// payload is a flat string map: "type" is always present, every other field
// only when it has a value.
func (e notificationEvent) payload() map[string]string {
	payload := map[string]string{"type": e.Type}
	for key, value := range e.Extra {
		if value != "" {
			payload[key] = value
		}
	}
	if e.Title != "" {
		payload["title"] = e.Title
	}
	if e.IssueID != nil && *e.IssueID != uuid.Nil {
		payload["issue_id"] = e.IssueID.String()
		if e.IssueIdentifier != "" {
			payload["issue_identifier"] = e.IssueIdentifier
		}
	}
	if e.ActorID != nil && *e.ActorID != uuid.Nil {
		payload["actor_id"] = e.ActorID.String()
	}
	if e.LiveOnly {
		payload["live_only"] = "true"
	}
	return payload
}

// publishNotification sends notification.created to the recipient only. Call
// it after the notification was stored: storing checks that the recipient may
// see what the title and issue reveal. Each publication carries a fresh
// event_id, so the recipient's open tabs can tell they received the same
// event and announce it once.
func publishNotification(hub userEventPublisher, workspaceID, userID uuid.UUID, event notificationEvent) {
	payload := event.payload()
	payload["event_id"] = uuid.NewString()
	hub.BroadcastToUser(workspaceID, userID, realtime.Event{Type: "notification.created", Payload: payload})
}

// actorFromContext is the signed-in user of the request being served, for
// services whose methods do not receive the actor.
func actorFromContext(ctx context.Context) *uuid.UUID {
	if access, ok := domain.AccessFromContext(ctx); ok && access.UserID != uuid.Nil {
		return &access.UserID
	}
	return nil
}
