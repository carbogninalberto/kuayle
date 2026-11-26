package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

const pushInterval = time.Minute

type pushJob struct {
	workspaceID uuid.UUID
	userID      uuid.UUID
	event       notificationEvent
	queuedAt    time.Time
}

func (s *PushService) enqueue(workspaceID, userID uuid.UUID, event notificationEvent) {
	if s == nil || s.queue == nil || (event.ActorID != nil && *event.ActorID == userID) {
		return
	}
	event.Extra = maps.Clone(event.Extra)
	select {
	case s.queue <- pushJob{workspaceID, userID, event, time.Now()}:
	default:
		// Keep requests independent of push provider latency.
	}
}

type pushRateKey struct {
	user  uuid.UUID
	event string
}

func (s *PushService) Run(ctx context.Context) {
	if s.queue == nil {
		return
	}
	lastSent := make(map[pushRateKey]time.Time)
	ticker := time.NewTicker(pushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for key, sent := range lastSent {
				if now.Sub(sent) >= pushInterval {
					delete(lastSent, key)
				}
			}
		case job := <-s.queue:
			now := time.Now()
			key := pushRateKey{job.userID, pushEventKey(job.event.Type)}
			if now.Sub(lastSent[key]) < pushInterval || now.Sub(job.queuedAt) > pushInterval {
				continue
			}
			deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			delivered, err := s.sendJob(deliveryCtx, job)
			cancel()
			if delivered {
				lastSent[key] = time.Now()
			}
			if err != nil && ctx.Err() == nil {
				log.WithError(err).Warn("Could not prepare web push delivery")
			}
		}
	}
}

func (job pushJob) enabled(prefs *domain.UserPreferences) bool {
	return (job.event.ActorID == nil || *job.event.ActorID != job.userID) &&
		prefs.PushNotifications && prefs.NotificationEvents.Complete()[pushEventKey(job.event.Type)]
}

func (s *PushService) sendJob(ctx context.Context, job pushJob) (bool, error) {
	state, err := s.effective(ctx)
	if err != nil || !state.Enabled {
		return false, err
	}
	ctx = domain.WithAccess(ctx, domain.Access{UserID: job.userID, WorkspaceID: job.workspaceID})
	prefs, err := s.prefs.Get(ctx, job.userID)
	if err != nil || !job.enabled(prefs) {
		return false, err
	}
	subs, err := s.store.ListByUser(ctx, job.userID)
	if err != nil || len(subs) == 0 {
		return false, err
	}
	member, err := s.access.workspaces.GetMember(ctx, job.workspaceID, job.userID)
	if err != nil || member == nil {
		return false, err
	}
	requiresSSO := false
	if member.Role != domain.RoleOwner {
		requiresSSO, err = s.access.requiresSSO(ctx, job.workspaceID)
		if err != nil {
			return false, err
		}
	}
	if err := s.access.authorizeEvent(ctx, job); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrConversationNotFound) {
			return false, nil
		}
		return false, err
	}
	workspace, err := s.access.workspaces.GetByID(ctx, job.workspaceID)
	if err != nil || workspace == nil {
		return false, err
	}
	payload, err := json.Marshal(makePushPayload(workspace.Slug, job.event))
	if err != nil {
		return false, err
	}
	attempted := false
	for _, sub := range subs {
		if ctx.Err() != nil {
			break
		}
		if (sub.SSOScoped && sub.WorkspaceID != job.workspaceID) || (!sub.SSOScoped && requiresSSO) {
			continue
		}
		attempted = true
		s.deliver(ctx, state.vapid(), sub, payload)
	}
	return attempted, nil
}

func (a pushAccess) authorizeEvent(ctx context.Context, job pushJob) error {
	event := job.event
	if event.LiveOnly || event.Type == "scheduled_job" {
		// A job can include private chat instructions even when it has an issue anchor.
		conversation, err := uuid.Parse(event.Extra["conversation_id"])
		if err != nil {
			return sql.ErrNoRows
		}
		if err = a.authorizeChat(ctx, job.workspaceID, conversation); err != nil || event.LiveOnly {
			return err
		}
	}
	return a.notifications.AuthorizeDelivery(ctx, &domain.Notification{
		UserID: job.userID, WorkspaceID: job.workspaceID, IssueID: event.IssueID, SourceTeamIDs: event.SourceTeams,
	})
}

func pushEventKey(notificationType string) string {
	switch notificationType {
	case notificationApprovalRequested, notificationApprovalFinished:
		return "approval_requested"
	case domain.NotificationAgentTaskFinished:
		return "task_finished"
	case domain.NotificationReviewNeeded, "assigned", "mentioned":
		return notificationType
	case "commented":
		return "comment"
	default:
		return "issue_updated"
	}
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
	Data  struct {
		URL string `json:"url"`
	} `json:"data"`
}

func makePushPayload(slug string, event notificationEvent) pushPayload {
	key := pushEventKey(event.Type)
	title := map[string]string{
		"approval_requested": "Approval requested", "task_finished": "Task finished", "review_needed": "Review needed",
		"assigned": "Issue assigned", "mentioned": "You were mentioned", "comment": "New comment", "issue_updated": "Issue updated",
	}[key]
	if event.Type == notificationApprovalFinished {
		title = "Approval finished"
	}
	if event.Type == "scheduled_job" {
		title = "Scheduled job"
	}
	payload := pushPayload{Title: title, Body: truncateRunes(event.Title, 350)}
	base := "/" + url.PathEscape(slug)
	payload.Data.URL = base + "/inbox"
	subject := "workspace"
	if id := event.IssueID; id != nil && *id != uuid.Nil {
		subject = id.String()
		identifier := event.IssueIdentifier
		if identifier == "" {
			identifier = subject
		}
		payload.Data.URL = base + "/issue/" + url.PathEscape(identifier)
	} else if id := event.Extra["approval_id"]; id != "" {
		subject = id
	} else if id := event.Extra["task_id"]; id != "" {
		subject = id
	}
	if conversation := event.Extra["conversation_id"]; conversation != "" &&
		(key == "approval_requested" || event.Type == "scheduled_job" || payload.Data.URL == base+"/inbox") {
		payload.Data.URL = base + "/assistant?conversation=" + url.QueryEscape(conversation)
	}
	if event.Type == "scheduled_job" {
		subject = event.Extra["job_id"]
	}
	payload.Tag = event.Type + ":" + subject
	return payload
}
