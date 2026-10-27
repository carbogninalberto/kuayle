package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

type approvalTasks interface {
	Options(ctx context.Context, ws, conversation uuid.UUID) (*AgentTaskOptions, error)
	Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error)
}

// AssistantApprovals resolves approvals requested by assistant tools. The
// decision and its effect are authorized here, never by UI state: the actor must
// be the bound user, and the effect re-checks their current permissions.
type AssistantApprovals struct {
	conversations *repository.ConversationRepository
	tasks         approvalTasks
}

func NewAssistantApprovals(conversations *repository.ConversationRepository, tasks approvalTasks) *AssistantApprovals {
	return &AssistantApprovals{conversations: conversations, tasks: tasks}
}

// Resolve applies a decision. Concurrent or repeated approvals converge on one
// task because the effect replays the approval's fixed request ID.
func (s *AssistantApprovals) Resolve(ctx context.Context, ws, conversation, id uuid.UUID, approve bool) (*domain.ConversationApproval, error) {
	approval, err := s.conversations.ClaimApproval(ctx, ws, conversation, id, approve)
	if err != nil || approval.Status != "approving" {
		return approval, err
	}
	var payload domain.DelegationPayload
	if approval.Action != "delegate_issue" || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, id, nil, "This approval cannot be executed")
	}
	chat, err := s.conversations.OpenIssue(ctx, ws, approval.IssueID)
	if errors.Is(err, repository.ErrConversationNotFound) {
		return s.conversations.FinishApproval(ctx, ws, conversation, id, nil, "The issue is no longer available to you")
	}
	if err != nil {
		return nil, err
	}
	options, err := s.tasks.Options(ctx, ws, chat.ID)
	if err == nil && len(options.Providers) == 0 {
		err = ErrProviderNotAllowed
	}
	var task *domain.AgentTask
	if err == nil {
		request := domain.AgentTaskRequest{RequestID: approval.RequestID, Prompt: payload.Instructions, Provider: options.Providers[0].ID}
		// Match the task composer's defaults: model selection applies only to
		// harnesses that support the workspace model provider.
		if mp := options.ModelProvider; mp != nil && mp.Configured && (request.Provider == "codex" || request.Provider == "claude-code") {
			request.Model, request.Thinking = mp.DefaultModel, mp.Thinking
		}
		task, err = s.tasks.Create(ctx, ws, chat.ID, request)
	}
	if err != nil {
		if message, permanent := delegationFailure(err); permanent {
			return s.conversations.FinishApproval(ctx, ws, conversation, id, nil, message)
		}
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, id, &task.ID, "")
}

func delegationFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrDevMachinesDisabled):
		return "Coding agents are not enabled for this workspace", true
	case errors.Is(err, ErrProviderNotAllowed):
		return "No allowed coding agent is available", true
	case errors.Is(err, ErrInvalidMachineInput), errors.Is(err, repository.ErrConversationInvalid):
		return "The coding task request was rejected", true
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return "The issue is no longer available to you", true
	case errors.Is(err, repository.ErrAgentTaskConflict), errors.Is(err, repository.ErrConversationConflict):
		return "The coding task could not be started in the issue's current state", true
	}
	return "", false
}
