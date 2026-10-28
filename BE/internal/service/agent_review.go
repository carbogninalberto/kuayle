package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ReviewHarnesses are the coding agents that can review a pull request.
var ReviewHarnesses = []string{"claude-code", "codex", "opencode"}

// RequestReview queues a review of a task's published pull request for the
// actor in ctx, from the actor's issue conversation. The harness may differ
// from the one that implemented the task; it must be allowed by the workspace
// policy, like any coding task. Reviews never push or publish.
func (s *AgentTaskService) RequestReview(ctx context.Context, ws, conversation, reviewed uuid.UUID, input domain.AgentReviewRequest) (*domain.AgentTask, *domain.AgentReview, error) {
	input.Focus = strings.TrimSpace(input.Focus)
	if err := s.validateRequest(ctx, ws, domain.AgentTaskRequest{Prompt: "Review the published pull request", Provider: input.Provider, Model: input.Model, Thinking: input.Thinking, ModelServerID: input.ModelServerID}); err != nil {
		return nil, nil, err
	}
	return s.Repo.CreateReview(ctx, ws, conversation, reviewed, input)
}

// Sessions returns a task's agent session and the review sessions of its
// published work under the task's current authorization.
func (s *AgentTaskService) Sessions(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTaskSessions, error) {
	return s.Repo.TaskSessions(ctx, ws, id)
}

// Get reads a task under its current authorization.
func (s *AgentTaskService) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	return s.Repo.Get(ctx, ws, id)
}
