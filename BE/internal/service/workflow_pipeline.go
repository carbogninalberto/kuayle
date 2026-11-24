package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Pipeline workflows are experimental (KUA-252): a server saves them, and
// starts tasks on them, only with WORKFLOW_PIPELINES_ENABLED on. Their tick
// (team_tick.go) runs those tasks; the graph engine never runs a pipeline.
var ErrPipelinesDisabled = errors.New("pipeline workflows are turned off on this server")

// SetPipelines turns the experimental pipeline workflows on or off.
func (s *AgentTaskService) SetPipelines(enabled bool) { s.pipelines = enabled }

// PipelinesEnabled reports the server's switch; a nil service has it off.
func (s *AgentTaskService) PipelinesEnabled() bool { return s != nil && s.pipelines }

// pipelineRefusal says why raw, when it is a pipeline, cannot be saved or
// start a task; nil for a graph.
func (s *AgentTaskService) pipelineRefusal(raw []byte) error {
	if domain.WorkflowKind(raw) == domain.WorkflowKindPipeline && !s.PipelinesEnabled() {
		return ErrPipelinesDisabled
	}
	return nil
}

// startablePipeline checks a pipeline a task starts on, as runnableWorkflow
// checks a graph: valid with the workspace's active profiles, and every role
// allowed by the workspace policy. It returns the writer's profile, whose
// harness and model the first turn runs.
func (s *AgentTaskService) startablePipeline(ctx context.Context, ws uuid.UUID, version *domain.WorkflowVersion) (*domain.Pipeline, domain.AgentProfile, error) {
	pipeline, err := domain.ParsePipeline(version.Graph)
	if err != nil {
		return nil, domain.AgentProfile{}, ErrWorkflowUnusable
	}
	profiles, err := s.definitions.ActiveProfiles(ctx, ws)
	if err != nil {
		return nil, domain.AgentProfile{}, err
	}
	if len(domain.ValidatePipeline(*pipeline, profiles)) > 0 {
		return nil, domain.AgentProfile{}, ErrWorkflowUnusable
	}
	for _, member := range pipeline.Roster {
		if err = s.profilePolicy(ctx, ws, profiles[member.ProfileID]); err != nil {
			return nil, domain.AgentProfile{}, err
		}
	}
	writer, _ := pipeline.Writer()
	return pipeline, profiles[writer.ProfileID], nil
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
