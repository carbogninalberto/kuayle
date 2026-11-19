package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

// approvalEffort previews the settings before asking for approval. Creating or
// retrying the task checks them again, since the approver can change the model.
func (t *AssistantTools) approvalEffort(ctx context.Context, ws, issue uuid.UUID, payload any) (string, string, error) {
	var model, effort string
	var err error
	switch proposal := payload.(type) {
	case domain.DelegationPayload:
		effort = proposal.Effort
		if effort != "" && t.agentTasks != nil {
			model, err = t.agentTasks.delegationEffort(ctx, ws, issue, proposal)
		}
	case domain.FollowUpPayload:
		effort = proposal.Effort
		if effort != "" && t.agentTasks != nil {
			task := &domain.AgentTask{Provider: proposal.Harness, Model: proposal.CurrentModel, ModelServerID: proposal.ModelServerID}
			var request domain.AgentTaskRequest
			request, err = t.agentTasks.retryInvocation(ctx, ws, task, domain.AgentTaskRetryRequest{Model: proposal.Model, RequestedEffort: effort})
			if err == nil {
				model, err = t.agentTasks.validateAssistantEffort(ctx, ws, request)
			}
		}
	}
	if effort == "" {
		return "", "", nil
	}
	if t.agentTasks == nil {
		return "", "", toolFailure("coding agent effort settings are unavailable")
	}
	if message, permanent := delegationFailure(err); permanent {
		return "", "", toolFailure("%s", message)
	}
	return model, effort, err
}

func (s *AgentTaskService) delegationEffort(ctx context.Context, ws, issue uuid.UUID, proposal domain.DelegationPayload) (string, error) {
	options, err := s.issueTaskOptions(ctx, ws, issue)
	if err != nil {
		return "", err
	}
	request, err := delegationRequest(options, uuid.Nil, proposal.Instructions, proposal.Harness, proposal.Model, nil, proposal.WorkflowID, proposal.Effort)
	if err != nil {
		return "", err
	}
	if err = s.resolveIssueWorkflow(ctx, ws, issue, &request); err != nil {
		return "", err
	}
	return s.validateAssistantEffort(ctx, ws, request)
}

func (s *AgentTaskService) validateAssistantEffort(ctx context.Context, ws uuid.UUID, request domain.AgentTaskRequest) (string, error) {
	harness, model, effort := request.Provider, request.Model, request.RequestedEffort
	if !harnessAcceptsModel(harness) {
		return "", &AgentModelError{fmt.Sprintf("%s supports no explicit effort values", s.machines.agentName(harness))}
	}
	if model == "" && harnessRunsOnModelServer(harness) {
		server, err := s.machines.store.GetModelServer(ctx, ws, request.ModelServerID)
		if err != nil {
			return "", err
		}
		if server != nil && server.Serves(harness) {
			model = server.DefaultModel
		} else {
			run := dto.CreateAgentRunRequest{Provider: harness, Thinking: effort}
			if err = s.machines.applyAgentModelDefault(ctx, ws, &run); err != nil {
				return "", err
			}
			model = run.Model
		}
	}
	entry, err := s.machines.catalogModelForValidation(ctx, harness, model)
	if err != nil {
		return "", err
	}
	if model == "" {
		model = "agent default"
	}
	allowed := modelThinkingLevels[1:] // The assistant schema has no off override.
	if len(entry.Efforts) > 0 {
		allowed = entry.Efforts[1:] // A known list starts with off (no variant).
	}
	name := s.machines.agentName(harness)
	if entry.ReasoningUnsupported || len(allowed) == 0 {
		return "", &AgentModelError{fmt.Sprintf("%s model %q supports no explicit effort values", name, model)}
	}
	if !containsString(allowed, effort) {
		return "", &AgentModelError{fmt.Sprintf("%s model %q allows the effort levels %s", name, model, strings.Join(allowed, ", "))}
	}
	if err := s.machines.validateAgentModel(ctx, ws, harness, request.ModelServerID, request.Model, effort); err != nil {
		return "", err
	}
	return model, nil
}
