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
	if err := s.requireMachineUse(ctx); err != nil {
		return nil, err
	}
	return s.Repo.QueueLiveMessage(ctx, ws, id, sender, body)
}

// SendLiveMessage sends the creator's own message to the task's agent. mode
// is empty (the working attempt reads it, otherwise the next attempt's
// prompt carries it), domain.AgentLiveSendInterrupt (the working attempt is
// stopped and its session resumed with the message) or
// domain.AgentLiveSendStart (an attempt is started for it when none runs). A
// harness that cannot be handed a message while it works is interrupted
// whatever the mode.
func (s *AgentTaskService) SendLiveMessage(ctx context.Context, ws, id uuid.UUID, body, mode string) (*domain.AgentLiveMessage, error) {
	if err := s.requireMachineUse(ctx); err != nil {
		return nil, err
	}
	return s.Repo.SendLiveMessage(ctx, ws, id, body, mode)
}

// LiveMessages lists the newest messages of a task's live channel in both
// directions, oldest first; agent messages are untrusted text to show.
func (s *AgentTaskService) LiveMessages(ctx context.Context, ws, id uuid.UUID, limit int) ([]domain.AgentLiveMessage, error) {
	return s.Repo.LiveMessages(ctx, ws, id, limit)
}
