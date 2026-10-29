package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// QueueLiveMessage sends text from the task's creator (sender "user") or from
// the orchestrator acting for them (sender "orchestrator") to the task's
// agent. The manager writes it to the running attempt's inbox; without a
// running attempt the next attempt's prompt carries it. It only delivers
// text and changes no other state.
func (s *AgentTaskService) QueueLiveMessage(ctx context.Context, ws, id uuid.UUID, sender, body string) (*domain.AgentLiveMessage, error) {
	return s.Repo.QueueLiveMessage(ctx, ws, id, sender, body)
}

// LiveMessages lists the newest messages of a task's live channel in both
// directions, oldest first; agent messages are untrusted text to show.
func (s *AgentTaskService) LiveMessages(ctx context.Context, ws, id uuid.UUID, limit int) ([]domain.AgentLiveMessage, error) {
	return s.Repo.LiveMessages(ctx, ws, id, limit)
}
