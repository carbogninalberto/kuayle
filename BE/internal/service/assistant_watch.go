package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// watchAgentWork registers a one-shot watch (KUA-165) on the issue's newest
// coding task for this conversation: when the task records a transition into
// the watched state (or any end state, when until is omitted), Kuayle posts a
// short assistant message here and sends the user's normal notification, with
// no model call.
//
// The watch never fires for a state the task already reached at registration:
// the call fails with the current state so the assistant answers directly,
// and a task that already finished is reported instead of watched.
func (t *AssistantTools) watchAgentWork(ctx context.Context, a domain.ConversationAttempt, arguments string) (any, *uuid.UUID, error) {
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	if t.work == nil || t.watchers == nil {
		return nil, nil, toolFailure("unknown tool")
	}
	var input struct {
		Identifier string `json:"identifier"`
		Until      string `json:"until"`
	}
	if err = decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if input.Until != "" && !slices.Contains([]string{"awaiting_review", "completed", "failed"}, input.Until) {
		return nil, nil, toolFailure("until must be awaiting_review, completed or failed, or omitted for any end state")
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return nil, nil, toolFailure("issue not found")
	}
	target, err := t.work.FollowUpTarget(ctx, ws, issue.ID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return nil, nil, toolFailure("this issue has no coding task")
	}
	if err != nil {
		return nil, nil, err
	}
	if (input.Until != "" && target.State == input.Until) || (input.Until == "" && domain.IsAgentTaskEndState(target.State)) {
		return nil, nil, toolFailure("the coding task on %s is already %s; report that state instead of watching", issue.Identifier, target.State)
	}
	if a.Message.ParentID == nil {
		return nil, nil, toolFailure("the watch could not be registered in this conversation")
	}
	if err = t.watchers.WatchTask(ctx, ws, user, target.TaskID, a.Conversation.ID, *a.Message.ParentID, input.Until); err != nil {
		return nil, nil, err
	}
	until := input.Until
	if until == "" {
		until = "any end state"
	}
	return map[string]any{"watching": true, "identifier": issue.Identifier, "state": target.State, "until": until}, &issue.ID, nil
}
