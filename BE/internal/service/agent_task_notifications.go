package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// taskNoticeStore hands out each notifying task transition once.
type taskNoticeStore interface {
	ClaimTaskNotices(ctx context.Context, limit int) ([]domain.AgentTaskNotice, error)
}

// SetNotifications makes a task that finishes, fails or reaches review notify
// its creator. Without it RunNotifications does nothing.
func (s *AgentTaskService) SetNotifications(notices taskNoticeStore, notifications notificationCreator, hub userEventPublisher) {
	s.notices, s.notifications, s.userEvents = notices, notifications, hub
}

// RunNotifications delivers task notifications until ctx ends. Transitions
// are recorded by whichever path changes the task (worker, acceptance,
// workflow, review), so this reads the transition log instead of hooking
// each of them, and only sees committed transitions.
func (s *AgentTaskService) RunNotifications(ctx context.Context) {
	if s.notices == nil || s.notifications == nil || s.userEvents == nil {
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
			log.WithField("task_id", notice.TaskID).WithError(err).Warn("Could not notify about an agent task")
			continue
		}
		publishNotification(s.userEvents, notice.WorkspaceID, notice.CreatorID, notificationEvent{
			Type: notifType, Title: title, IssueID: &notice.IssueID, IssueIdentifier: notice.IssueIdentifier,
			Extra: map[string]string{"task_id": notice.TaskID.String(), "status": notice.State},
		})
	}
}

func taskNoticeTitle(notice domain.AgentTaskNotice) string {
	subject := "Coding task"
	if notice.Kind == domain.AgentTaskKindReview {
		subject = "Agent review"
	}
	outcome := map[string]string{"completed": "completed", "failed": "failed", "awaiting_review": "ready for review"}[notice.State]
	return fmt.Sprintf("%s %s on %s: %s", subject, outcome, notice.IssueIdentifier, notice.IssueTitle)
}
