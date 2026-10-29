package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// RunCheck runs the task's check, or the given command, on its latest
// attempt for the caller, without an agent attempt or a state change.
func (s *AgentTaskService) RunCheck(ctx context.Context, ws, id uuid.UUID, command []string) (*domain.DevMachineAgentRunStep, error) {
	return s.Repo.RunCheck(ctx, ws, id, command)
}

type approvalChecks interface {
	Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error)
	RunCheck(ctx context.Context, ws, id uuid.UUID, command []string) (*domain.DevMachineAgentRunStep, error)
}

// SetChecks enables approving run_check requests.
func (s *AssistantApprovals) SetChecks(checks approvalChecks) {
	s.checks = checks
}

// checkableStates are the task states in which a check can run; the active
// ones wait for the agent.
var checkableStates = []string{"awaiting_review", "blocked", "failed", "completed"}

// prepareRunCheck binds a check request to the issue's newest task, its
// current attempt and the exact command shown to the user. A command is run
// as `sh -c <command>`; without one the task's own check runs.
func (t *AssistantTools) prepareRunCheck(ctx context.Context, ws, user uuid.UUID, arguments string) (uuid.UUID, domain.RunCheckPayload, error) {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, domain.RunCheckPayload{}, toolFailure("you no longer have access to this workspace")
	}
	if t.work == nil {
		return uuid.Nil, domain.RunCheckPayload{}, toolFailure("unknown tool")
	}
	var input struct {
		Identifier string `json:"identifier"`
		Command    string `json:"command"`
	}
	if err = decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, domain.RunCheckPayload{}, err
	}
	var command []string
	if text := strings.TrimSpace(input.Command); text != "" {
		if utf8.RuneCountInString(text) > 2000 {
			return uuid.Nil, domain.RunCheckPayload{}, toolFailure("the command must be at most 2000 characters")
		}
		if !domain.HasPermission(member.Role, domain.PermDevMachineManage) {
			return uuid.Nil, domain.RunCheckPayload{}, toolFailure("your role cannot run a custom check command")
		}
		command = []string{"sh", "-c", text}
		if domain.ValidateTestCommand(command) != nil {
			return uuid.Nil, domain.RunCheckPayload{}, toolFailure("invalid command")
		}
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return uuid.Nil, domain.RunCheckPayload{}, toolFailure("issue not found")
	}
	target, err := t.work.FollowUpTarget(ctx, ws, issue.ID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return uuid.Nil, domain.RunCheckPayload{}, toolFailure("this issue has no coding task")
	}
	if err != nil {
		return uuid.Nil, domain.RunCheckPayload{}, err
	}
	if target.CreatorID != user {
		return uuid.Nil, domain.RunCheckPayload{}, toolFailure("only the task's creator can run its check")
	}
	if !slices.Contains(checkableStates, target.State) {
		if target.State == "cancelled" || target.State == "queued" {
			return uuid.Nil, domain.RunCheckPayload{}, toolFailure("the task is %s; it has no attempt to check", target.State)
		}
		return uuid.Nil, domain.RunCheckPayload{}, toolFailure("the agent is working (the task is %s); try again when it pauses", target.State)
	}
	return issue.ID, domain.RunCheckPayload{Identifier: issue.Identifier, Title: issue.Title, TaskID: target.TaskID, Generation: target.Generation, Command: command}, nil
}

// runCheck queues an approved check on the attempt the user saw, as the
// approver; the repository re-checks that they created the task.
func (s *AssistantApprovals) runCheck(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.RunCheckPayload
	if s.checks == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	task, err := s.checks.Get(ctx, ws, payload.TaskID)
	if err == nil && task.IssueID != approval.IssueID {
		err = repository.ErrConversationNotFound
	}
	if err == nil && task.Generation != payload.Generation {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task started a new attempt after approval was requested; ask again")
	}
	if err == nil {
		_, err = s.checks.RunCheck(ctx, ws, payload.TaskID, payload.Command)
	}
	switch {
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "Only the task's creator can run its check")
	case errors.Is(err, repository.ErrMachineBusy):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The agent is working; try again when it pauses")
	case errors.Is(err, repository.ErrCheckUnavailable), errors.Is(err, repository.ErrNoCheckCommand), errors.Is(err, repository.ErrCheckForbidden):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, upperFirst(err.Error()))
	case errors.Is(err, repository.ErrConversationInvalid):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The check command was rejected")
	case err != nil:
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &payload.TaskID, "")
}

func upperFirst(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
