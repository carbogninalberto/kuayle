package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
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
	// Merging is unavailable until SetMerging wires it.
	targets approvalMergeTargets
	members assistantMemberRepo
	merger  approvalMerger
}

type approvalMergeTargets interface {
	MergeTarget(ctx context.Context, ws uuid.UUID, issue, task *uuid.UUID) (*repository.MergeTarget, error)
}

type approvalMerger interface {
	MergeTaskPullRequest(ctx context.Context, ws uuid.UUID, repository string, number int, sha string) error
}

// SetMerging enables approving merge_pull_request requests.
func (s *AssistantApprovals) SetMerging(targets approvalMergeTargets, members assistantMemberRepo, merger approvalMerger) {
	s.targets, s.members, s.merger = targets, members, merger
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
	if approval.Action == "merge_pull_request" {
		return s.merge(ctx, ws, conversation, approval)
	}
	var payload domain.DelegationPayload
	if approval.Action != "delegate_issue" || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, id, nil, "This approval cannot be executed")
	}
	task, err := delegateIssue(ctx, s.conversations, s.tasks, ws, approval.IssueID, payload.Instructions, approval.RequestID)
	if err != nil {
		if message, permanent := delegationFailure(err); permanent {
			return s.conversations.FinishApproval(ctx, ws, conversation, id, nil, message)
		}
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, id, &task.ID, "")
}

// delegateIssue starts a coding task on an issue for the actor in ctx, with the
// task composer's defaults. Replaying the same request ID converges on one task.
func delegateIssue(ctx context.Context, conversations *repository.ConversationRepository, tasks approvalTasks, ws, issue uuid.UUID, instructions string, requestID uuid.UUID) (*domain.AgentTask, error) {
	chat, err := conversations.OpenIssue(ctx, ws, issue)
	if err != nil {
		return nil, err
	}
	options, err := tasks.Options(ctx, ws, chat.ID)
	if err != nil {
		return nil, err
	}
	if len(options.Providers) == 0 {
		return nil, ErrProviderNotAllowed
	}
	provider := options.Providers[0].ID
	if options.DefaultProvider != "" {
		provider = options.DefaultProvider
	}
	request := domain.AgentTaskRequest{RequestID: requestID, Prompt: instructions, Provider: provider}
	// Match the task composer's defaults: model selection applies only to
	// harnesses that support the workspace model provider.
	if mp := options.ModelProvider; mp != nil && mp.Configured && (request.Provider == "codex" || request.Provider == "claude-code") {
		request.Model, request.Thinking = mp.DefaultModel, mp.Thinking
	}
	return tasks.Create(ctx, ws, chat.ID, request)
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

// merge applies an approved merge. The task must still await review at the
// commit the user saw, and the approver must own the workspace or the task:
// merging changes the base branch, so issue access alone is not enough.
func (s *AssistantApprovals) merge(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.MergePayload
	if s.merger == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	target, err := s.targets.MergeTarget(ctx, ws, nil, &payload.TaskID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task is no longer awaiting review")
	}
	if err != nil {
		return nil, err
	}
	if target.IssueID != approval.IssueID || target.Repository != payload.Repository || target.Number != payload.PullRequest || target.Commit != payload.Commit {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The pull request changed after approval was requested; ask again")
	}
	member, err := s.members.GetMember(ctx, ws, approval.ActorID)
	if err != nil {
		return nil, err
	}
	if member == nil || (member.Role != domain.RoleOwner && member.Role != domain.RoleAdmin && target.CreatorID != approval.ActorID) {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "Only workspace admins or the task's creator can merge")
	}
	if err = s.merger.MergeTaskPullRequest(ctx, ws, target.Repository, target.Number, target.Commit); err != nil {
		var refused *gh.ErrMergeRefused
		if errors.As(err, &refused) {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, truncateRunes(refused.Error(), 500))
		}
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &target.TaskID, "")
}
