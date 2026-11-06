package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

// agentModelDefaultProviders are the coding agents that take a model and an effort.
var agentModelDefaultProviders = []string{"codex", "claude-code"}

// ListAgentModelDefaults reports every such agent's workspace default, set or not.
func (s *DevMachineService) ListAgentModelDefaults(ctx context.Context, workspaceID uuid.UUID) ([]dto.AgentModelDefaultResponse, error) {
	stored, err := s.store.ListAgentModelDefaults(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.AgentModelDefaultResponse, 0, len(agentModelDefaultProviders))
	for _, provider := range agentModelDefaultProviders {
		value := domain.AgentModelDefault{Provider: provider}
		for _, item := range stored {
			if item.Provider == provider {
				value = item
			}
		}
		responses = append(responses, agentModelDefaultResponse(value))
	}
	return responses, nil
}

// SaveAgentModelDefault replaces an agent's workspace default; an empty model and
// effort remove it. Callers check that the user administers the workspace.
func (s *DevMachineService) SaveAgentModelDefault(ctx context.Context, workspaceID, userID uuid.UUID, provider string, request dto.SaveAgentModelDefaultRequest) (*dto.AgentModelDefaultResponse, error) {
	if !containsString(agentModelDefaultProviders, provider) {
		return nil, fmt.Errorf("%w: only codex and claude-code take a default model", ErrInvalidMachineInput)
	}
	value := &domain.AgentModelDefault{WorkspaceID: workspaceID, Provider: provider, Model: strings.TrimSpace(request.Model), Thinking: request.Thinking, UpdatedBy: &userID}
	if value.Model != "" && !modelNamePattern.MatchString(value.Model) {
		return nil, fmt.Errorf("%w: invalid model name", ErrInvalidMachineInput)
	}
	if value.Thinking != "" && !containsString(modelThinkingLevels, value.Thinking) {
		return nil, fmt.Errorf("%w: unsupported effort level", ErrInvalidMachineInput)
	}
	if err := s.store.SaveAgentModelDefault(ctx, value); err != nil {
		return nil, err
	}
	response := agentModelDefaultResponse(*value)
	return &response, nil
}

func agentModelDefaultResponse(value domain.AgentModelDefault) dto.AgentModelDefaultResponse {
	response := dto.AgentModelDefaultResponse{Provider: value.Provider, Model: value.Model, Thinking: value.Thinking,
		ThinkingLevels: append([]string(nil), modelThinkingLevels...)}
	if (value.Model != "" || value.Thinking != "") && !value.UpdatedAt.IsZero() {
		updated := value.UpdatedAt
		response.UpdatedAt = &updated
	}
	return response
}

// applyAgentModelDefault fills the model and effort a run on the agent's own
// sign-in did not choose from the workspace default. It runs when each run is
// created, so every new attempt of an open task uses the current default,
// while a run that already exists keeps what it started with. A model server
// brings its own default model and thinking level instead.
func (s *DevMachineService) applyAgentModelDefault(ctx context.Context, workspaceID uuid.UUID, req *dto.CreateAgentRunRequest) error {
	if !containsString(agentModelDefaultProviders, req.Provider) || (req.Model != "" && req.Thinking != "") {
		return nil
	}
	value, err := s.store.GetAgentModelDefault(ctx, workspaceID, req.Provider)
	if err != nil || value == nil {
		return err
	}
	if req.Model == "" {
		req.Model = value.Model
	}
	if req.Thinking == "" {
		req.Thinking = value.Thinking
	}
	return nil
}
