package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// taskWatcherClaimsFake hands each watch out once, as the repository's delete
// on claim does.
type taskWatcherClaimsFake struct {
	pending []domain.AgentTaskWatch
	err     error
}

func (f *taskWatcherClaimsFake) ClaimTaskWatchers(context.Context, int) ([]domain.AgentTaskWatch, error) {
	claimed := f.pending
	f.pending = nil
	return claimed, f.err
}

type postedNotice struct {
	ws, conversation, parent uuid.UUID
	content                  string
}

// conversationNoticesFake records posted notices and the actor they were
// posted for, which must be the watch's user.
type conversationNoticesFake struct {
	posted []postedNotice
	actors []uuid.UUID
	err    error
}

func (f *conversationNoticesFake) PostAssistantNotice(ctx context.Context, ws, conversation, parent uuid.UUID, content string) (*domain.ConversationMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	access, _ := domain.AccessFromContext(ctx)
	f.actors = append(f.actors, access.UserID)
	f.posted = append(f.posted, postedNotice{ws, conversation, parent, content})
	return &domain.ConversationMessage{Content: content}, nil
}

func watchFor(state string) domain.AgentTaskWatch {
	return domain.AgentTaskWatch{
		TaskID: uuid.New(), WorkspaceID: uuid.New(), UserID: uuid.New(),
		ConversationID: uuid.New(), ParentMessageID: uuid.New(),
		State: state, IssueID: uuid.New(), IssueIdentifier: "ENG-7", IssueTitle: "Fix login",
	}
}

// KUA-165: a claimed watch pings exactly once, with the user's normal
// notification and a short assistant message in the conversation it was
// registered in, with no model call.
func TestClaimedTaskWatchPingsOnceWithNotificationAndChatMessage(t *testing.T) {
	for _, tc := range []struct {
		state string
		kind  string
	}{
		{"completed", "agent_task_finished"},
		{"failed", "agent_task_finished"},
		{"awaiting_review", "review_needed"},
		{"cancelled", "agent_task_finished"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			notifications, events := &notificationsFake{}, &userEventsFake{}
			conversations := &conversationNoticesFake{}
			watch := watchFor(tc.state)
			store := &taskWatcherClaimsFake{pending: []domain.AgentTaskWatch{watch}}
			svc := &AgentTaskService{}
			svc.SetWatchers(store, conversations)
			svc.SetNotifications(&taskNoticesFake{}, notifications, events)

			title := "Coding task " + map[string]string{"completed": "completed", "failed": "failed", "awaiting_review": "ready for review", "cancelled": "cancelled"}[tc.state] + " on ENG-7: Fix login"

			svc.deliverTaskWatches(context.Background())
			// A later pass finds the watch already claimed and deleted.
			svc.deliverTaskWatches(context.Background())

			require.Equal(t, []createdNotification{{user: watch.UserID, workspace: watch.WorkspaceID, issue: &watch.IssueID, kind: tc.kind, title: title}}, notifications.created)
			require.Equal(t, []userEvent{{workspace: watch.WorkspaceID, user: watch.UserID, kind: "notification.created", payload: map[string]string{
				"type": tc.kind, "issue_id": watch.IssueID.String(), "issue_identifier": "ENG-7",
				"task_id": watch.TaskID.String(), "status": tc.state, "title": title,
			}}}, events.events)
			require.Equal(t, []postedNotice{{ws: watch.WorkspaceID, conversation: watch.ConversationID, parent: watch.ParentMessageID,
				content: "Ping, as you asked: coding task " + map[string]string{"completed": "completed", "failed": "failed", "awaiting_review": "ready for review", "cancelled": "cancelled"}[tc.state] + " on ENG-7: Fix login."}}, conversations.posted)
			require.Equal(t, []uuid.UUID{watch.UserID}, conversations.actors, "the message is posted as the watch's user")
		})
	}
}

// A watch whose notification cannot be stored is not announced and never
// posts; a chat message that fails loses only the message, and nothing is
// required to be wired at all.
func TestTaskWatchDeliveryIsBestEffort(t *testing.T) {
	watch := watchFor("completed")

	// An unstorable notification ends the ping before any message or event.
	events := &userEventsFake{}
	conversations := &conversationNoticesFake{}
	svc := &AgentTaskService{}
	svc.SetWatchers(&taskWatcherClaimsFake{pending: []domain.AgentTaskWatch{watch}}, conversations)
	svc.SetNotifications(&taskNoticesFake{}, &notificationsFake{err: errors.New("not visible")}, events)
	svc.deliverTaskWatches(context.Background())
	require.Empty(t, events.events)
	require.Empty(t, conversations.posted)

	// A chat message that fails loses only the message.
	notifications := &notificationsFake{}
	svc.SetWatchers(&taskWatcherClaimsFake{pending: []domain.AgentTaskWatch{watch}}, &conversationNoticesFake{err: errors.New("conversation gone")})
	svc.SetNotifications(&taskNoticesFake{}, notifications, events)
	svc.deliverTaskWatches(context.Background())
	require.Len(t, notifications.created, 1, "the notification is still stored")
	require.Len(t, events.events, 1, "the notification is still announced")

	// A claim that fails, or nothing wired, changes nothing more.
	svc.SetWatchers(&taskWatcherClaimsFake{err: errors.New("database unavailable")}, conversations)
	svc.deliverTaskWatches(context.Background())
	(&AgentTaskService{}).deliverTaskWatches(context.Background())
	svc.SetWatchers(&taskWatcherClaimsFake{}, conversations)
	svc.deliverTaskWatches(context.Background())
	require.Len(t, notifications.created, 1)
	require.Len(t, events.events, 1)
	require.Empty(t, conversations.posted)
}
