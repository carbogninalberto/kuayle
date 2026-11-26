package service

import (
	"context"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// taskNoticeStore hands out each notifying task transition once.
type taskNoticeStore interface {
	ClaimTaskNotices(ctx context.Context, limit int) ([]domain.AgentTaskNotice, error)
}

// taskWatcherStore hands out each claimed task watch once (KUA-165).
type taskWatcherStore interface {
	ClaimTaskWatchers(ctx context.Context, limit int) ([]domain.AgentTaskWatch, error)
}

// conversationNoticePoster posts one assistant message without a model call,
// acting as the conversation's creator.
type conversationNoticePoster interface {
	PostAssistantNotice(ctx context.Context, ws, conversation, parent uuid.UUID, content string) (*domain.ConversationMessage, error)
}

// SetNotifications makes a task that finishes, fails or reaches review notify
// its creator. Without it RunNotifications does nothing.
func (s *AgentTaskService) SetNotifications(notices taskNoticeStore, notifications notificationCreator, hub userEventPublisher) {
	s.notices, s.notifications, s.userEvents = notices, notifications, hub
}

// SetWatchers makes a claimed task watch post its ping in the watcher's own
// conversation and notify them through the normal notification path (KUA-165).
// Without it RunNotifications delivers only the creators' transition notices.
func (s *AgentTaskService) SetWatchers(watchers taskWatcherStore, conversations conversationNoticePoster) {
	s.watchers, s.conversations = watchers, conversations
}

// RunNotifications delivers task notifications and claimed task watches until
// ctx ends. Transitions are recorded by whichever path changes the task
// (worker, acceptance, workflow, review), so this reads the transition log
// instead of hooking each of them, and only sees committed transitions.
func (s *AgentTaskService) RunNotifications(ctx context.Context) {
	if (s.notices == nil || s.notifications == nil || s.userEvents == nil) &&
		(s.watchers == nil || s.conversations == nil || s.notifications == nil || s.userEvents == nil) {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.deliverTaskNotices(ctx)
		if s.Repo != nil && s.userEvents != nil && s.notifications != nil {
			notices, err := s.Repo.ClaimDecisionNotices(ctx)
			if err != nil {
				log.WithError(err).Warn("Could not claim owner decision notices")
			}
			for _, n := range notices {
				recipient := domain.WithAccess(ctx, domain.Access{WorkspaceID: n.WorkspaceID, UserID: n.CreatorID})
				s.publishLeadReports(recipient, n.AgentTask, []uuid.UUID{n.NoticeID})
			}
		}
		s.deliverTaskWatches(ctx)
	}
}

// deliverTaskNotices notifies the creators of the transitions it claims. It is
// best effort: a failure is logged and never reaches the task.
func (s *AgentTaskService) deliverTaskNotices(ctx context.Context) {
	if s.notices == nil || s.notifications == nil || s.userEvents == nil {
		return
	}
	notices, err := s.notices.ClaimTaskNotices(ctx, 50)
	if err != nil {
		if ctx.Err() == nil {
			log.WithError(err).Warn("Could not claim agent task notifications")
		}
		return
	}
	for _, notice := range notices {
		notifType := domain.AgentTaskNotificationType(notice.State)
		if notifType == "" || notice.CreatorID == uuid.Nil {
			continue
		}
		title := taskNoticeTitle(notice)
		if err := s.notifications.Create(ctx, notice.CreatorID, notice.WorkspaceID, &notice.IssueID, notifType, title); err != nil {
			// Claiming cleared the pending mark, so nothing delivers it later.
			log.WithField("task_id", notice.TaskID).WithError(err).Error("Could not store an agent task notification; the notice is lost")
			continue
		}
		publishNotification(s.userEvents, notice.WorkspaceID, notice.CreatorID, notificationEvent{
			Type: notifType, Title: title, IssueID: &notice.IssueID, IssueIdentifier: notice.IssueIdentifier,
			Extra: map[string]string{"task_id": notice.TaskID.String(), "status": notice.State},
		}, s.notifications)
	}
}

// publishLeadReports announces the inbox rows stored with the room posts.
// Only a pass that committed its room cursor publishes. Recheck visibility
// before sending the stored text to a client or the push worker.
func (s *AgentTaskService) publishLeadReports(ctx context.Context, task domain.AgentTask, ids []uuid.UUID) {
	if s.notifications == nil || s.userEvents == nil {
		return
	}
	reports, err := s.Repo.LeadReportNotices(ctx, task, ids)
	if err != nil {
		log.WithField("task_id", task.ID).WithError(err).Warn("Could not announce stored lead reports")
		return
	}
	for _, n := range reports {
		publishNotification(s.userEvents, n.WorkspaceID, n.UserID, notificationEvent{
			Type: n.Type, Title: n.Title, IssueID: n.IssueID, IssueIdentifier: deref(n.IssueIdentifier),
			SourceTeams: n.SourceTeamIDs, Extra: map[string]string{"task_id": task.ID.String()},
		}, s.notifications)
	}
}

// deliverTaskWatches pings each claimed watch once (KUA-165): the user's
// normal notification, plus a short assistant message in the conversation the
// watch was registered in, without a model call. A watcher whom the creator's
// notice for the same event already notifies (watch.Notified) gets only the
// message, so they are notified once. Claiming deleted the row, so a delivery
// that fails is not repeated.
func (s *AgentTaskService) deliverTaskWatches(ctx context.Context) {
	if s.watchers == nil || s.conversations == nil || s.notifications == nil || s.userEvents == nil {
		return
	}
	watches, err := s.watchers.ClaimTaskWatchers(ctx, 50)
	if err != nil {
		if ctx.Err() == nil {
			log.WithError(err).Warn("Could not claim agent task watches")
		}
		return
	}
	for _, watch := range watches {
		title := taskNoticeTitle(domain.AgentTaskNotice{
			TaskID: watch.TaskID, WorkspaceID: watch.WorkspaceID, CreatorID: watch.UserID, IssueID: watch.IssueID,
			Kind: watch.Kind, State: watch.State, IssueIdentifier: watch.IssueIdentifier, IssueTitle: watch.IssueTitle,
		})
		if !watch.Notified {
			notifType := domain.AgentTaskNotificationType(watch.State)
			if notifType == "" {
				// A cancelled task still finished working; its ping says so.
				notifType = domain.NotificationAgentTaskFinished
			}
			if err := s.notifications.Create(ctx, watch.UserID, watch.WorkspaceID, &watch.IssueID, notifType, title); err != nil {
				log.WithField("task_id", watch.TaskID).WithError(err).Error("Could not store a task watch notification; the ping is lost")
				continue
			}
			publishNotification(s.userEvents, watch.WorkspaceID, watch.UserID, notificationEvent{
				Type: notifType, Title: title, IssueID: &watch.IssueID, IssueIdentifier: watch.IssueIdentifier,
				Extra: map[string]string{"task_id": watch.TaskID.String(), "status": watch.State},
			}, s.notifications)
		}
		asWatcher := domain.WithAccess(ctx, domain.Access{WorkspaceID: watch.WorkspaceID, UserID: watch.UserID})
		if _, err := s.conversations.PostAssistantNotice(asWatcher, watch.WorkspaceID, watch.ConversationID, watch.ParentMessageID, watchPingMessage(title)); err != nil {
			log.WithField("task_id", watch.TaskID).WithError(err).Warn("Could not post a task watch message; the chat ping is lost")
		}
	}
}

// watchPingMessage is the chat message a claimed watch posts: the same text as
// the notification, marked as the ping the user asked for.
func watchPingMessage(title string) string {
	first, size := utf8.DecodeRuneInString(title)
	return "Ping, as you asked: " + string(unicode.ToLower(first)) + title[size:] + "."
}

func taskNoticeTitle(notice domain.AgentTaskNotice) string {
	subject := "Coding task"
	if notice.Kind == domain.AgentTaskKindReview {
		subject = "Agent review"
	}
	outcome := map[string]string{"completed": "completed", "failed": "failed", "awaiting_review": "ready for review", "cancelled": "cancelled"}[notice.State]
	return fmt.Sprintf("%s %s on %s: %s", subject, outcome, notice.IssueIdentifier, notice.IssueTitle)
}
