package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

const notificationApprovalRequested = "approval_requested"

// SetNotifications makes a new approval request notify the person who must
// decide it. Without it requests appear only in their conversation.
func (s *AssistantApprovals) SetNotifications(notifications notificationCreator, hub userEventPublisher) {
	s.notifications, s.userEvents = notifications, hub
}

// notifyRequested tells the approval's actor, the conversation's creator,
// that a decision is waiting. Call it after the approval was committed. It is
// best effort: a failure is logged and never fails the request.
//
// A notification row needs an issue or a team to be traced to. An approval
// with neither (an upgrade, a release, a wiki action in a free chat) is only
// announced live; its title names the action alone.
func (s *AssistantApprovals) notifyRequested(ctx context.Context, conversation domain.Conversation, approval *domain.ConversationApproval, identifier string) {
	if s == nil || s.notifications == nil || s.userEvents == nil || approval == nil {
		return
	}
	action := strings.ReplaceAll(approval.Action, "_", " ")
	event := notificationEvent{
		Type:  notificationApprovalRequested,
		Title: "Approval needed: " + action,
		Extra: map[string]string{"approval_id": approval.ID.String(), "conversation_id": approval.ConversationID.String()},
	}
	var err error
	switch {
	case approval.IssueID != uuid.Nil:
		event.IssueID, event.IssueIdentifier = &approval.IssueID, identifier
		if identifier != "" {
			event.Title = fmt.Sprintf("Approval needed on %s: %s", identifier, action)
		}
		err = s.notifications.Create(ctx, approval.ActorID, conversation.WorkspaceID, event.IssueID, event.Type, event.Title)
	case conversation.TeamID != nil:
		err = s.notifications.Create(ctx, approval.ActorID, conversation.WorkspaceID, nil, event.Type, event.Title, *conversation.TeamID)
	}
	if err != nil {
		log.WithField("approval_id", approval.ID).WithError(err).Warn("Could not notify about an approval request")
		return
	}
	publishNotification(s.userEvents, conversation.WorkspaceID, approval.ActorID, event)
}
