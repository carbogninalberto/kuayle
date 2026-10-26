package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

type AgentTaskOptions struct {
	Providers     []dto.AgentProviderResponse         `json:"providers"`
	ModelProvider *dto.WorkspaceModelProviderResponse `json:"model_provider"`
}

func (s *AgentTaskService) Options(ctx context.Context, ws, conversation uuid.UUID) (*AgentTaskOptions, error) {
	c, err := s.Repo.Conversation(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	if c.IssueID == nil {
		return nil, repository.ErrConversationInvalid
	}
	if _, err = s.machines.enabledPolicy(ctx, ws); err != nil {
		return nil, err
	}
	providers, err := s.machines.AvailableProviders(ctx, ws)
	if err != nil {
		return nil, err
	}
	result := &AgentTaskOptions{Providers: []dto.AgentProviderResponse{}}
	for _, p := range providers {
		// Custom CLIs require an administrator-saved command configuration, which
		// cannot be inferred when allocating a new task environment.
		if !p.Custom && containsString(p.SupportedModes, "autonomous") {
			result.Providers = append(result.Providers, p)
		}
	}
	result.ModelProvider, err = s.machines.GetWorkspaceModelProvider(ctx, ws)
	return result, err
}

func (s *AgentTaskService) validateRequest(ctx context.Context, ws uuid.UUID, input domain.AgentTaskRequest) error {
	policy, err := s.machines.enabledPolicy(ctx, ws)
	if err != nil {
		return err
	}
	provider, ok := s.machines.agents.Get(input.Provider)
	if !ok || provider.Metadata().Custom || !providerAllowed(policy.AllowedProviders, input.Provider) {
		return ErrProviderNotAllowed
	}
	if err = s.machines.validateAgentModel(ctx, ws, input.Provider, input.Model, input.Thinking); err != nil {
		return err
	}
	// Use the native provider's mode/prompt checks too; HTTP input never supplies
	// native harness argv, environment variables, runtime IDs or execution authority.
	_, err = provider.BuildInvocation(agent.RunInput{Mode: agent.ModeAutonomous, Prompt: input.Prompt, Model: input.Model, Thinking: input.Thinking})
	if err != nil {
		return ErrInvalidMachineInput
	}
	return nil
}

func (s *AgentTaskService) Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	if accepted, err := s.Repo.ReplayCreate(ctx, ws, conversation, input); err != nil || accepted != nil {
		return accepted, err
	}
	if err := s.validateRequest(ctx, ws, input); err != nil {
		return nil, err
	}
	return s.Repo.Create(ctx, ws, conversation, input)
}

func (s *AgentTaskService) Retry(ctx context.Context, ws, id uuid.UUID, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	if accepted, err := s.Repo.ReplayRetry(ctx, ws, id, input); err != nil || accepted != nil {
		return accepted, err
	}
	task, err := s.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	if err = s.validateRequest(ctx, ws, domain.AgentTaskRequest{Prompt: input.Prompt, Provider: task.Provider, Model: task.Model, Thinking: task.Thinking}); err != nil {
		return nil, err
	}
	return s.Repo.Retry(ctx, ws, id, input)
}
