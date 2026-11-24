package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Pipeline workflows are experimental (KUA-252): a server saves them only
// with WORKFLOW_PIPELINES_ENABLED on, and no task starts on one yet. The
// tick that will run them does not exist, and the graph engine must never
// run a pipeline.
var (
	ErrPipelinesDisabled   = errors.New("pipeline workflows are turned off on this server")
	ErrPipelineNotRunnable = errors.New("pipeline workflows cannot run tasks yet")
)

// SetPipelines turns the experimental pipeline workflows on or off.
func (s *AgentTaskService) SetPipelines(enabled bool) { s.pipelines = enabled }

// PipelinesEnabled reports the server's switch; a nil service has it off.
func (s *AgentTaskService) PipelinesEnabled() bool { return s != nil && s.pipelines }

// pipelineRefusal says why raw, when it is a pipeline, cannot be saved (or,
// with starting, start a task); nil for a graph.
func (s *AgentTaskService) pipelineRefusal(raw []byte, starting bool) error {
	switch {
	case domain.WorkflowKind(raw) != domain.WorkflowKindPipeline:
		return nil
	case !s.PipelinesEnabled():
		return ErrPipelinesDisabled
	case starting:
		return ErrPipelineNotRunnable
	}
	return nil
}

// pipelineIssues is every problem of a pipeline (domain.ValidatePipeline).
func (s *WorkflowService) pipelineIssues(ctx context.Context, ws uuid.UUID, raw json.RawMessage) ([]domain.WorkflowIssue, error) {
	pipeline, err := domain.ParsePipeline(raw)
	if err != nil {
		var invalid *domain.WorkflowInvalidError
		if errors.As(err, &invalid) {
			return invalid.Issues, nil
		}
		return nil, err
	}
	profiles, err := s.repo.ActiveProfiles(ctx, ws)
	if err != nil {
		return nil, err
	}
	return domain.ValidatePipeline(*pipeline, profiles), nil
}
