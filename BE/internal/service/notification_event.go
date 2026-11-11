package service

import (
	"context"

	"github.com/google/uuid"
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
	return payload
}

// publishNotification sends notification.created to the recipient only. Call
// it after the notification was stored: storing checks that the recipient may
// see what the title and issue reveal.
func publishNotification(hub userEventPublisher, workspaceID, userID uuid.UUID, event notificationEvent) {
	hub.BroadcastToUser(workspaceID, userID, realtime.Event{Type: "notification.created", Payload: event.payload()})
}
