package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type createdNotification struct {
	user, workspace uuid.UUID
	issue           *uuid.UUID
	kind, title     string
	teams           []uuid.UUID
}

// notificationsFake records stored notifications; err refuses them, as the
// repository does for a recipient who may not see the issue.
type notificationsFake struct {
	created []createdNotification
	err     error
}

func (f *notificationsFake) Create(_ context.Context, user, workspace uuid.UUID, issue *uuid.UUID, kind, title string, teams ...uuid.UUID) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, createdNotification{user, workspace, issue, kind, title, teams})
	return nil
}

type userEvent struct {
	workspace, user uuid.UUID
	kind            string
	payload         map[string]string
}

type userEventsFake struct{ events []userEvent }

func (f *userEventsFake) BroadcastToUser(workspace, user uuid.UUID, event realtime.Event) {
	f.events = append(f.events, userEvent{workspace, user, event.Type, event.Payload.(map[string]string)})
}

func TestApprovalRequestNotifiesTheActor(t *testing.T) {
	notifications, events := &notificationsFake{}, &userEventsFake{}
	approvals := NewAssistantApprovals(nil, nil)
	approvals.SetNotifications(notifications, events)
	ws, actor, issue := uuid.New(), uuid.New(), uuid.New()
	conversation := domain.Conversation{ID: uuid.New(), WorkspaceID: ws, CreatorID: actor}
	approval := &domain.ConversationApproval{ID: uuid.New(), ConversationID: conversation.ID, Action: "merge_pull_request", ActorID: actor, IssueID: issue}

	approvals.notifyRequested(context.Background(), conversation, approval, "ENG-7")

	title := "Approval needed on ENG-7: merge pull request"
	// The repository derives the source team from the issue.
	require.Equal(t, []createdNotification{{user: actor, workspace: ws, issue: &issue, kind: "approval_requested", title: title}}, notifications.created)
	require.Equal(t, []userEvent{{workspace: ws, user: actor, kind: "notification.created", payload: map[string]string{
		"type": "approval_requested", "issue_id": issue.String(), "issue_identifier": "ENG-7",
		"approval_id": approval.ID.String(), "conversation_id": conversation.ID.String(), "title": title,
	}}}, events.events)
}

func TestApprovalRequestWithoutIssue(t *testing.T) {
	ws, actor, team := uuid.New(), uuid.New(), uuid.New()
	approval := &domain.ConversationApproval{ID: uuid.New(), ConversationID: uuid.New(), Action: "publish_release", ActorID: actor}
	want := map[string]string{"type": "approval_requested", "approval_id": approval.ID.String(), "conversation_id": approval.ConversationID.String(), "title": "Approval needed: publish release"}

	t.Run("a team chat stores a notification traced to its team", func(t *testing.T) {
		notifications, events := &notificationsFake{}, &userEventsFake{}
		approvals := NewAssistantApprovals(nil, nil)
		approvals.SetNotifications(notifications, events)
		approvals.notifyRequested(context.Background(), domain.Conversation{WorkspaceID: ws, CreatorID: actor, TeamID: &team}, approval, "")
		require.Equal(t, []createdNotification{{user: actor, workspace: ws, kind: "approval_requested", title: want["title"], teams: []uuid.UUID{team}}}, notifications.created)
		require.Equal(t, []userEvent{{ws, actor, "notification.created", want}}, events.events)
	})
	t.Run("a free chat has nothing to trace a row to and is announced live only", func(t *testing.T) {
		notifications, events := &notificationsFake{}, &userEventsFake{}
		approvals := NewAssistantApprovals(nil, nil)
		approvals.SetNotifications(notifications, events)
		approvals.notifyRequested(context.Background(), domain.Conversation{WorkspaceID: ws, CreatorID: actor}, approval, "")
		require.Empty(t, notifications.created)
		require.Equal(t, []userEvent{{ws, actor, "notification.created", want}}, events.events)
	})
}

// A notification the recipient may not see, or that cannot be stored, is
// dropped without an event, and nothing is required to be wired at all.
func TestApprovalRequestNotificationIsBestEffort(t *testing.T) {
	events := &userEventsFake{}
	approvals := NewAssistantApprovals(nil, nil)
	approvals.SetNotifications(&notificationsFake{err: errors.New("not visible")}, events)
	approval := &domain.ConversationApproval{ID: uuid.New(), Action: "delegate_issue", ActorID: uuid.New(), IssueID: uuid.New()}

	approvals.notifyRequested(context.Background(), domain.Conversation{WorkspaceID: uuid.New()}, approval, "ENG-7")
	require.Empty(t, events.events)

	NewAssistantApprovals(nil, nil).notifyRequested(context.Background(), domain.Conversation{}, approval, "ENG-7")
	var unset *AssistantApprovals
	unset.notifyRequested(context.Background(), domain.Conversation{}, approval, "ENG-7")
}

type taskNoticesFake struct {
	pending []domain.AgentTaskNotice
	err     error
}

// Like the repository, a notice is handed out once.
func (f *taskNoticesFake) ClaimTaskNotices(context.Context, int) ([]domain.AgentTaskNotice, error) {
	claimed := f.pending
	f.pending = nil
	return claimed, f.err
}

func TestAgentTaskTransitionsNotifyTheCreator(t *testing.T) {
	ws, creator, issue := uuid.New(), uuid.New(), uuid.New()
	notice := func(state string) domain.AgentTaskNotice {
		return domain.AgentTaskNotice{TaskID: uuid.New(), WorkspaceID: ws, CreatorID: creator, IssueID: issue, Kind: domain.AgentTaskKindImplement, State: state, IssueIdentifier: "ENG-7", IssueTitle: "Fix login"}
	}
	for _, tc := range []struct{ state, kind, title string }{
		{"completed", "agent_task_finished", "Coding task completed on ENG-7: Fix login"},
		{"failed", "agent_task_finished", "Coding task failed on ENG-7: Fix login"},
		{"awaiting_review", "review_needed", "Coding task ready for review on ENG-7: Fix login"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			notifications, events := &notificationsFake{}, &userEventsFake{}
			pending := notice(tc.state)
			store := &taskNoticesFake{pending: []domain.AgentTaskNotice{pending}}
			svc := &AgentTaskService{}
			svc.SetNotifications(store, notifications, events)

			svc.deliverTaskNotices(context.Background())
			// A later pass finds the transition already claimed.
			svc.deliverTaskNotices(context.Background())

			require.Equal(t, []createdNotification{{user: creator, workspace: ws, issue: &issue, kind: tc.kind, title: tc.title}}, notifications.created)
			require.Equal(t, []userEvent{{workspace: ws, user: creator, kind: "notification.created", payload: map[string]string{
				"type": tc.kind, "issue_id": issue.String(), "issue_identifier": "ENG-7", "task_id": pending.TaskID.String(), "status": tc.state, "title": tc.title,
			}}}, events.events)
		})
	}
}

func TestAgentTaskNotificationsSkipOtherStatesAndMissingCreators(t *testing.T) {
	notifications, events := &notificationsFake{}, &userEventsFake{}
	store := &taskNoticesFake{}
	for _, state := range []string{"queued", "preparing", "running", "waiting_input", "validating", "publishing", "blocked", "cancelling", "cancelled"} {
		require.Empty(t, domain.AgentTaskNotificationType(state), state)
		store.pending = append(store.pending, domain.AgentTaskNotice{TaskID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New(), IssueID: uuid.New(), State: state})
	}
	store.pending = append(store.pending, domain.AgentTaskNotice{TaskID: uuid.New(), WorkspaceID: uuid.New(), IssueID: uuid.New(), State: "completed"})
	svc := &AgentTaskService{}
	svc.SetNotifications(store, notifications, events)

	svc.deliverTaskNotices(context.Background())

	require.Empty(t, notifications.created)
	require.Empty(t, events.events)
}

func TestAgentTaskNotificationFailuresOnlySkipThatNotice(t *testing.T) {
	events := &userEventsFake{}
	store := &taskNoticesFake{pending: []domain.AgentTaskNotice{{TaskID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New(), IssueID: uuid.New(), State: "failed"}}}
	svc := &AgentTaskService{}
	svc.SetNotifications(store, &notificationsFake{err: errors.New("not visible")}, events)
	svc.deliverTaskNotices(context.Background())
	require.Empty(t, events.events)

	svc.SetNotifications(&taskNoticesFake{err: errors.New("database unavailable")}, &notificationsFake{}, events)
	svc.deliverTaskNotices(context.Background())
	require.Empty(t, events.events)

	// Nothing wired: nothing happens.
	(&AgentTaskService{}).deliverTaskNotices(context.Background())
}

// The issue notification event keeps "type" and adds what a client needs to
// show and open it; it follows the stored notification.
func TestIssueNotificationEventDescribesTheIssue(t *testing.T) {
	notifRepo := new(mockNotificationRepo)
	notifRepo.On("Create", mock.Anything, mock.Anything).Return(nil)
	hub := realtime.NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil })
	svc := NewIssueService(new(mockIssueRepo), new(mockTeamRepo), new(mockTeamStatusRepo), new(mockIssueHistoryRepo), hub, NewNotificationService(notifRepo))
	ws, recipient := uuid.New(), uuid.New()
	issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: uuid.New(), Identifier: "ENG-7", Title: "Fix login"}
	next := workspaceEvents(t, hub, ws, recipient)

	svc.notify(context.Background(), recipient, issue, "assigned", "You were assigned to ENG-7: Fix login")

	eventType, raw, err := next(2 * time.Second)
	require.NoError(t, err)
	require.Equal(t, "notification.created", eventType)
	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.Equal(t, map[string]string{"type": "assigned", "issue_id": issue.ID.String(), "issue_identifier": "ENG-7", "title": "You were assigned to ENG-7: Fix login"}, payload)

	// A notice that is not linked to an issue names none.
	svc.notifyIssue(context.Background(), recipient, issue, "issue_deleted", "ENG-7 was deleted", false)
	_, raw, err = next(2 * time.Second)
	require.NoError(t, err)
	payload = nil
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.Equal(t, map[string]string{"type": "issue_deleted", "title": "ENG-7 was deleted"}, payload)
}
