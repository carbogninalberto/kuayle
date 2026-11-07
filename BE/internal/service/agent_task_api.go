package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"strings"
)

type AgentTaskOptions struct {
	Providers []dto.AgentProviderResponse `json:"providers"`
	// ModelServers lists the workspace's model servers; the default comes first.
	ModelServers []dto.ModelServerResponse `json:"model_servers"`
	// DefaultProvider is the inherited preferred coding agent, when allowed.
	DefaultProvider string `json:"default_provider,omitempty"`
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
	preferred, err := s.Repo.DefaultProvider(ctx, ws, *c.IssueID)
	if err != nil {
		return nil, err
	}
	for _, provider := range result.Providers {
		if provider.ID == preferred {
			result.DefaultProvider = preferred
		}
	}
	result.ModelServers, err = s.machines.ListModelServers(ctx, ws)
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
	if err = s.machines.validateAgentModel(ctx, ws, input.Provider, input.ModelServerID, input.Model, input.Thinking); err != nil {
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
	if err := s.resolveWorkflow(ctx, ws, conversation, &input); err != nil {
		return nil, err
	}
	if err := s.validateRequest(ctx, ws, input); err != nil {
		return nil, err
	}
	return s.Repo.Create(ctx, ws, conversation, input)
}

func (s *AgentTaskService) Retry(ctx context.Context, ws, id uuid.UUID, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	input.Model = strings.TrimSpace(input.Model)
	if accepted, err := s.Repo.ReplayRetry(ctx, ws, id, input); err != nil || accepted != nil {
		return accepted, err
	}
	task, err := s.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	model, thinking, serverID := task.Model, task.Thinking, task.ModelServerID
	if input.Model != "" {
		// Keep thinking and the model server consistent with the new model.
		servers, err := s.machines.ListModelServers(ctx, ws)
		if err != nil {
			return nil, err
		}
		model, thinking, serverID = retryModel(servers, task, input.Model, input.RequestedModelServerID)
		input.Thinking, input.ModelServerID = thinking, serverID
	}
	if err = s.validateRequest(ctx, ws, domain.AgentTaskRequest{Prompt: input.Prompt, Provider: task.Provider, Model: model, Thinking: thinking, ModelServerID: serverID}); err != nil {
		return nil, err
	}
	return s.Repo.Retry(ctx, ws, id, input)
}

// Accept completes the creator's task awaiting review in Kuayle without
// merging its pull request; administrators accept through the assistant's
// approval instead (AcceptAsApprover).
func (s *AgentTaskService) Accept(ctx context.Context, ws, id uuid.UUID, generation int64) (*domain.AgentTask, error) {
	return s.accept(ctx, ws, id, generation, false)
}

// AcceptAsApprover lets the task's creator or a workspace owner/admin accept
// it from an approved assistant request.
func (s *AgentTaskService) AcceptAsApprover(ctx context.Context, ws, id uuid.UUID, generation int64) (*domain.AgentTask, error) {
	return s.accept(ctx, ws, id, generation, true)
}

func (s *AgentTaskService) accept(ctx context.Context, ws, id uuid.UUID, generation int64, administrators bool) (*domain.AgentTask, error) {
	task, issue, err := s.Repo.Accept(ctx, ws, id, generation, administrators)
	if err != nil {
		return nil, err
	}
	if issue != nil && s.events != nil {
		s.events.Broadcast(ws, realtime.Event{Type: "issue.updated", Payload: issue})
	}
	return task, nil
}

// ErrWorkflowUnusable means the chosen or default workflow cannot start a
// task now: it is archived, or one of its agent profiles was archived.
var ErrWorkflowUnusable = errors.New("the workflow cannot start a task")

// resolveWorkflow pins the workflow a new task runs: the one the request
// chose, else the issue's project, team or workspace default. The Standard
// workflow (no version) keeps the request as it is. A custom workflow's
// first agent step decides the harness and model, and every agent step's
// profile must be allowed by the workspace policy now; each step is checked
// again when it runs.
func (s *AgentTaskService) resolveWorkflow(ctx context.Context, ws, conversation uuid.UUID, input *domain.AgentTaskRequest) error {
	input.Workflow, input.WorkflowInstructions = nil, ""
	if s.definitions == nil {
		if input.WorkflowID != nil && *input.WorkflowID != domain.StandardWorkflowID && *input.WorkflowID != "" {
			return repository.ErrWorkflowNotFound
		}
		return nil
	}
	c, err := s.Repo.Conversation(ctx, ws, conversation)
	if err != nil {
		return err
	}
	if c.IssueID == nil {
		return repository.ErrConversationInvalid
	}
	version, err := s.definitions.ResolveWorkflow(ctx, ws, *c.IssueID, input.WorkflowID)
	if err != nil || version == nil {
		return err
	}
	graph, err := domain.ParseWorkflowGraph(version.Graph)
	if err != nil {
		return ErrWorkflowUnusable
	}
	profiles, err := s.definitions.ActiveProfiles(ctx, ws)
	if err != nil {
		return err
	}
	if len(domain.ValidateWorkflowGraph(*graph, profiles)) > 0 {
		return ErrWorkflowUnusable
	}
	for _, node := range graph.Nodes {
		if node.Type == domain.WorkflowNodeAgent {
			if err = s.profilePolicy(ctx, ws, profiles[*node.ProfileID]); err != nil {
				return err
			}
		}
	}
	start, _ := graph.Start()
	first := graph.Decide(domain.WorkflowCursor{Node: start.ID, Visits: map[string]int{}}, domain.WorkflowElse, time.Now(), profiles).To
	profile := profiles[*first.ProfileID]
	input.Provider, input.Model, input.Thinking, input.ModelServerID = profile.Harness, profile.Model, profile.Effort, nil
	input.Workflow = version
	input.WorkflowInstructions = workflowInstructions("", profile, first)
	return nil
}
