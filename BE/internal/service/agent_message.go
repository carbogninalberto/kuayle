package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

type agentMessageStore interface {
	List(ctx context.Context, ws uuid.UUID, filter domain.AgentMessageFilter) (*domain.AgentMessagePage, error)
	Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentMessageView, []domain.AgentMessageAction, error)
	Act(ctx context.Context, ws, id uuid.UUID, action string) (*domain.AgentMessageView, error)
	Holds(ctx context.Context, ws uuid.UUID) ([]domain.AgentMessageHold, error)
	HoldConversation(ctx context.Context, ws, first, second uuid.UUID) (*domain.AgentMessageHold, error)
	ReleaseConversation(ctx context.Context, ws, id uuid.UUID) error
	PostNextComment(ctx context.Context) (*repository.AgentMessageComment, error)
}

// AgentMessageService exposes the agent messaging registry to people and
// posts comment-channel messages on their recipient issues.
type AgentMessageService struct {
	store  agentMessageStore
	events taskEventPublisher
}

func NewAgentMessageService(store *repository.AgentMessageRepository, hub *realtime.Hub) *AgentMessageService {
	service := &AgentMessageService{store: store}
	if hub != nil {
		service.events = hub
	}
	return service
}

func (s *AgentMessageService) List(ctx context.Context, ws uuid.UUID, filter domain.AgentMessageFilter) (*domain.AgentMessagePage, error) {
	return s.store.List(ctx, ws, filter)
}

func (s *AgentMessageService) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentMessageView, []domain.AgentMessageAction, error) {
	return s.store.Get(ctx, ws, id)
}

// Act holds, releases or drops a message; the repository audits it.
func (s *AgentMessageService) Act(ctx context.Context, ws, id uuid.UUID, action string) (*domain.AgentMessageView, error) {
	return s.store.Act(ctx, ws, id, action)
}

func (s *AgentMessageService) Holds(ctx context.Context, ws uuid.UUID) ([]domain.AgentMessageHold, error) {
	return s.store.Holds(ctx, ws)
}

func (s *AgentMessageService) HoldConversation(ctx context.Context, ws, first, second uuid.UUID) (*domain.AgentMessageHold, error) {
	return s.store.HoldConversation(ctx, ws, first, second)
}

func (s *AgentMessageService) ReleaseConversation(ctx context.Context, ws, id uuid.UUID) error {
	return s.store.ReleaseConversation(ctx, ws, id)
}

// PostComments posts due comment-channel messages, at most limit per call,
// and announces each comment once it is committed.
func (s *AgentMessageService) PostComments(ctx context.Context, limit int) (int, error) {
	posted := 0
	for range limit {
		comment, err := s.store.PostNextComment(ctx)
		if err != nil || comment == nil {
			return posted, err
		}
		if !comment.Posted {
			log.WithFields(log.Fields{"agent_message_id": comment.MessageID}).Info("agent message comment skipped: the sending task's creator can no longer comment on the issue")
			continue
		}
		posted++
		if s.events != nil {
			s.events.Broadcast(comment.WorkspaceID, realtime.Event{Type: "comment.created", Payload: map[string]string{
				"issue_id": comment.IssueID.String(), "comment_id": comment.CommentID.String(), "identifier": comment.Identifier,
			}})
		}
	}
	return posted, nil
}

// Run posts comment-channel messages every interval until ctx ends.
func (s *AgentMessageService) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := s.PostComments(ctx, 20); err != nil && ctx.Err() == nil {
			log.WithError(err).Warn("agent message comments could not be posted")
		}
	}
}
