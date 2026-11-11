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
	return s.Repo.ResumePaused(ctx, ws, id, usageLimitResumeNowText)
}

// SwitchWorkflow says what moving the creator's task to the newest version
// of its workflow would do, and with apply does it. A refusal is an answer,
// not an error: the plan says why.
func (s *AgentTaskService) SwitchWorkflow(ctx context.Context, ws, id uuid.UUID, apply bool) (*domain.WorkflowSwitch, *domain.AgentTask, error) {
	return s.Repo.SwitchWorkflow(ctx, ws, id, apply)
}
