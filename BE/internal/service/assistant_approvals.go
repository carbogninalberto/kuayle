package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	// Reviews and follow-ups are unavailable until SetReviews wires them.
	reviews approvalReviews
	// Acceptance is unavailable until SetAccepting wires it.
	acceptor approvalAcceptor
	// Previews and ports are unavailable until SetMachines wires them.
	machineTasks assistantMachineTasks
	machines     assistantMachinePorts
	// Tool switches are ignored until SetToolSettings wires them.
	settings approvalToolSettings
}

type approvalToolSettings interface {
	Disabled(ctx context.Context, ws uuid.UUID) (map[string]bool, error)
}

// SetToolSettings makes approving fail for a tool the workspace has turned off
// since the approval was requested.
func (s *AssistantApprovals) SetToolSettings(settings approvalToolSettings) {
	s.settings = settings
}

type approvalAcceptor interface {
	AcceptAsApprover(ctx context.Context, ws, id uuid.UUID, generation int64) (*domain.AgentTask, error)
}

// SetAccepting enables approving accept_agent_work requests.
func (s *AssistantApprovals) SetAccepting(acceptor approvalAcceptor) {
	s.acceptor = acceptor
}

type approvalReviews interface {
	Options(ctx context.Context, ws, conversation uuid.UUID) (*AgentTaskOptions, error)
	RequestReview(ctx context.Context, ws, conversation, reviewed uuid.UUID, input domain.AgentReviewRequest) (*domain.AgentTask, *domain.AgentReview, error)
	Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error)
	Retry(ctx context.Context, ws, id uuid.UUID, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error)
}

// SetReviews enables approving request_agent_review and follow_up_task. It
// needs SetMerging's merge targets to pin a review to the published commit.
func (s *AssistantApprovals) SetReviews(reviews approvalReviews) {
	s.reviews = reviews
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
	if s.settings != nil {
		disabled, err := s.settings.Disabled(ctx, ws)
		if err != nil {
			// The claim stays in place; approving again resumes it.
			return nil, err
		}
		if disabled[approval.Action] {
			return s.conversations.FinishApproval(ctx, ws, conversation, id, nil, "This tool is disabled in this workspace")
		}
	}
	switch approval.Action {
	case "merge_pull_request":
		return s.merge(ctx, ws, conversation, approval)
	case "accept_agent_work":
		return s.accept(ctx, ws, conversation, approval)
	case "request_agent_review":
		return s.review(ctx, ws, conversation, approval)
	case "follow_up_task":
		return s.followUp(ctx, ws, conversation, approval)
	case "start_preview":
		return s.startPreview(ctx, ws, conversation, approval)
	case "expose_port":
		return s.exposePort(ctx, ws, conversation, approval)
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
	// Match the task composer's defaults: the default model server's model and
	// thinking level, when that server speaks the harness's protocol.
	for _, server := range options.ModelServers {
		if server.IsDefault && containsString(server.Harnesses, request.Provider) {
			request.Model, request.Thinking = server.DefaultModel, server.Thinking
		}
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

// accept completes the approved attempt in Kuayle as the approver, without
// merging. The task must still await review at the attempt the user saw; the
// repository re-checks that the approver is its creator or a workspace
// owner/admin who can see the issue. A repeated approval of an accepted
// attempt converges on the same completed task.
func (s *AssistantApprovals) accept(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.AcceptPayload
	if s.acceptor == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	task, err := s.acceptor.AcceptAsApprover(ctx, ws, payload.TaskID, payload.Generation)
	switch {
	case errors.Is(err, repository.ErrAgentTaskConflict), errors.Is(err, repository.ErrConversationConflict):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task changed after approval was requested or is no longer awaiting review; ask again")
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "Only workspace admins or the task's creator can accept its work")
	case err != nil:
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &task.ID, "")
}

// review starts an approved review as the approver, pinned to the commit the
// user saw. The task must still await review; the harness must be allowed.
func (s *AssistantApprovals) review(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.ReviewPayload
	if s.reviews == nil || s.targets == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	target, err := s.targets.MergeTarget(ctx, ws, nil, &payload.TaskID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task is no longer awaiting review")
	}
	if err != nil {
		return nil, err
	}
	if target.IssueID != approval.IssueID || target.Commit != payload.Commit || target.Number != payload.PullRequest {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The pull request changed after approval was requested; ask again")
	}
	chat, err := s.conversations.OpenIssue(ctx, ws, approval.IssueID)
	if err != nil {
		if message, permanent := delegationFailure(err); permanent {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		return nil, err
	}
	options, err := s.reviews.Options(ctx, ws, chat.ID)
	if err != nil {
		if message, permanent := delegationFailure(err); permanent {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		return nil, err
	}
	allowed := false
	for _, provider := range options.Providers {
		allowed = allowed || provider.ID == payload.Harness
	}
	if !allowed {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "That coding agent is not available in this workspace")
	}
	request := domain.AgentReviewRequest{RequestID: approval.RequestID, Provider: payload.Harness, Focus: payload.Focus, PostToPullRequest: payload.PostToPullRequest, ExpectedCommit: payload.Commit}
	for _, server := range options.ModelServers {
		if server.IsDefault && containsString(server.Harnesses, request.Provider) {
			request.Model, request.Thinking = server.DefaultModel, server.Thinking
		}
	}
	task, _, err := s.reviews.RequestReview(ctx, ws, chat.ID, payload.TaskID, request)
	switch {
	case errors.Is(err, repository.ErrNoReviewTarget):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task is no longer awaiting review")
	case errors.Is(err, repository.ErrReviewTargetChanged):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The pull request changed after approval was requested; ask again")
	case err != nil:
		if message, permanent := delegationFailure(err); permanent {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &task.ID, "")
}

// followUp queues an approved follow-up attempt on the task the user saw, as
// its creator, through the same retry path as review feedback.
func (s *AssistantApprovals) followUp(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.FollowUpPayload
	if s.reviews == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	task, err := s.reviews.Get(ctx, ws, payload.TaskID)
	if err == nil && task.IssueID != approval.IssueID {
		err = repository.ErrConversationNotFound
	}
	if err == nil {
		task, err = s.reviews.Retry(ctx, ws, payload.TaskID, domain.AgentTaskRetryRequest{RequestID: approval.RequestID, Generation: payload.Generation, Prompt: chatFollowUpPrompt(task.Prompt, payload.Instructions)})
	}
	if err != nil {
		if errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "Only the task's creator can start a follow-up")
		}
		if errors.Is(err, repository.ErrAgentTaskConflict) || errors.Is(err, repository.ErrConversationConflict) {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task changed after approval was requested; ask again")
		}
		if message, permanent := delegationFailure(err); permanent {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &task.ID, "")
}

// chatFollowUpPrompt keeps the task's original instructions and replaces any
// earlier follow-up section with the instructions the user approved.
func chatFollowUpPrompt(prompt, instructions string) string {
	base, _, _ := strings.Cut(prompt, followUpMarker)
	base = truncateRunes(base, 16000)
	return base + followUpMarker + "Kuayle chat:\nThe user approved this follow-up in Kuayle's assistant. Continue on the existing branch and pull request:\n\n" + truncateRunes(instructions, 16000)
}
