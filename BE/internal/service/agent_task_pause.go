package service

import (
	"context"
	"errors"

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
// asked to pause), otherwise a task paused by its agent's usage limit or
// because its provider account had no credit left.
func (s *AgentTaskService) Resume(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	return s.Repo.ResumePaused(ctx, ws, id, usageLimitResumeNowText)
}

// SwitchWorkflow plans or applies a creator's requested workflow move.
func (s *AgentTaskService) SwitchWorkflow(ctx context.Context, ws, id uuid.UUID, target domain.WorkflowSwitchTarget, apply bool) (*domain.WorkflowSwitch, *domain.AgentTask, error) {
	if target.WorkflowID == "" {
		return s.Repo.SwitchWorkflow(ctx, ws, id, target, apply)
	}
	plan, _, err := s.Repo.SwitchWorkflow(ctx, ws, id, target, false)
	if err != nil || plan.Refused != "" {
		return plan, nil, err
	}
	versionID := plan.Continuation.ToVersionID
	if s.definitions == nil {
		return nil, nil, ErrInvalidOperation
	}
	version, err := s.definitions.Version(ctx, ws, versionID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err = s.runnableWorkflow(ctx, ws, version); err != nil {
		var step *WorkflowStepError
		if !errors.Is(err, ErrWorkflowUnusable) && !errors.As(err, &step) {
			return nil, nil, err
		}
		plan.Refused, plan.Continuation = domain.WorkflowSwitchUnusable, nil
		return plan, nil, nil
	}
	if !apply {
		return plan, nil, nil
	}
	target.ExpectedVersionID = &versionID
	return s.Repo.SwitchWorkflow(ctx, ws, id, target, true)
}
