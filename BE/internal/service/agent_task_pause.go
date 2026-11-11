package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// PauseWorkflow pauses the workflow of the creator's task: mode is
// domain.WorkflowPauseNow, which stops the agent that is working, or
// domain.WorkflowPauseAfterStep.
func (s *AgentTaskService) PauseWorkflow(ctx context.Context, ws, id uuid.UUID, mode string) (*domain.AgentTask, error) {
	return s.Repo.PauseWorkflow(ctx, ws, id, mode)
}

// Resume continues the creator's paused task: a workflow they paused (or
// asked to pause), otherwise a task paused by its agent's usage limit.
func (s *AgentTaskService) Resume(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	task, err := s.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	if task.WorkflowPausedAt != nil || task.WorkflowPauseRequest != nil {
		return s.Repo.ResumeWorkflow(ctx, ws, id)
	}
	return s.ResumeUsageLimitNow(ctx, ws, id)
}
