package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
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
// whatever the mode. Starting a new iteration on an issue whose work was
// merged needs confirmMerged (domain.MergedWorkError otherwise).
func (s *AgentTaskService) SendLiveMessage(ctx context.Context, ws, id uuid.UUID, body, mode string, confirmMerged bool, attachments ...uuid.UUID) (*domain.AgentLiveMessage, error) {
	if err := s.requireMachineUse(ctx); err != nil {
		return nil, err
	}
	return s.Repo.SendLiveMessage(ctx, ws, id, body, mode, confirmMerged, attachments...)
}

// LiveMessages lists the newest messages of a task's live channel in both
// directions, oldest first; agent messages are untrusted text to show.
func (s *AgentTaskService) LiveMessages(ctx context.Context, ws, id uuid.UUID, limit int) ([]domain.AgentLiveMessage, error) {
	return s.Repo.LiveMessages(ctx, ws, id, limit)
}

// PostRoomMessage posts the creator's message to the room of a pipeline
// task; RoomMessages lists the room for them (repository.RoomMessages).
func (s *AgentTaskService) PostRoomMessage(ctx context.Context, ws, id uuid.UUID, post domain.TaskRoomPost) (*domain.TaskMessage, error) {
	if err := s.requireMachineUse(ctx); err != nil {
		return nil, err
	}
	return s.Repo.PostRoomMessage(ctx, ws, id, post)
}

// ApproveTaskSplit announces the children created by a committed approval.
// Replaying the same approval returns no new children to announce.
func (s *AgentTaskService) ApproveTaskSplit(ctx context.Context, ws, id uuid.UUID, seq int64) error {
	if err := s.requireMachineUse(ctx); err != nil {
		return err
	}
	issues, err := s.Repo.ApproveTaskSplit(ctx, ws, id, seq)
	if err != nil {
		return err
	}
	s.announceSplit(ctx, ws, issues)
	return nil
}

// carryOutGrantedSplits creates the sub-issues of the lead's split requests
// that the task's creator granted (KUA-313), without the approval step, and
// announces them as an approved split's are.
func (s *AgentTaskService) carryOutGrantedSplits(ctx context.Context, task domain.AgentTask) error {
	issues, err := s.Repo.CarryOutGrantedSplits(ctx, task.WorkspaceID, task.ID)
	s.announceSplit(ctx, task.WorkspaceID, issues)
	return err
}

// announceSplit tells the workspace and its webhooks of a split's children.
func (s *AgentTaskService) announceSplit(ctx context.Context, ws uuid.UUID, issues []domain.Issue) {
	if len(issues) == 0 {
		return
	}
	if s.events != nil {
		for i := range issues {
			s.events.Broadcast(ws, realtime.Event{Type: WebhookEventIssueCreated, Payload: &issues[i]})
		}
	}
	if s.webhooks != nil {
		s.webhooks.IssueEvents(ctx, ws, WebhookEventIssueCreated, issues, issues[0].CreatorID)
	}
}
