package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// pauseForProviderCredit pauses an attempt the provider refused because the
// account has no credit left (KUA-218). Like a usage limit the work and the
// session are kept and nothing failed; unlike one nothing resets on its own,
// so the task waits for its creator to top the account up and resume it. A
// workflow task stays on its step: no visit, iteration or edge is used.
func (s *AgentTaskService) pauseForProviderCredit(ctx context.Context, task domain.AgentTask, message string) error {
	agentName := agentDisplayNames[task.Provider]
	if agentName == "" {
		agentName = "The agent"
	}
	// The message never carries a link: DetectProviderCredit dropped it.
	reason := fmt.Sprintf("Paused: %s's provider account has no credit left (%s). Top up the provider account, then resume the task; the work done so far is kept.",
		agentName, strings.TrimRight(message, ". "))
	return s.store.PauseForProviderCredit(ctx, task, message, truncateRunes(reason, 2000))
}

// modelNotFoundReason is why a task fails when the provider does not offer
// its model (KUA-218): which model, and where it is set. Another attempt
// with the same model fails the same way, so the task fails instead of
// retrying, as it does for a missing sign-in or a resource limit.
func modelNotFoundReason(task domain.AgentTask, model string) string {
	if model == "" {
		model = task.Model
	}
	named := "the provider has no model " + fmt.Sprintf("%q", truncateRunes(model, 100))
	if model == "" {
		named = "the provider does not offer the model the agent chose from its own configuration"
	}
	hint := "Retry the task with a model it offers"
	switch {
	case task.Kind == domain.AgentTaskKindReview:
		hint = "Set a model it offers on the reviewer's agent profile in Settings → Agents, or request the review with another model"
	case task.RunsWorkflow():
		hint = "Set a model it offers on this step's agent profile in Settings → Agents, then retry the task with that model"
	case task.AgentProfileID != nil:
		hint = "Set a model it offers on the agent's profile in Settings → Agents, then retry the task with that model"
	}
	return domain.TaskFailureReason(domain.FailureProvider, named+". "+hint)
}

// reviewModelNotFound reports whether a reviewer step's review task failed
// because the provider does not offer the reviewer's model.
func reviewModelNotFound(review *repository.WorkflowReview) bool {
	return review.State == "failed" && review.FailureRule != nil && *review.FailureRule == domain.FailureRuleModelNotFound
}

// failForReviewer fails a workflow whose reviewer step cannot run because the
// provider does not offer the reviewer's model (KUA-218), with the review
// task's reason, which names the model and where it is set. The reviewer's
// agent_failed edge would start the same review with the same model again.
func (w *WorkflowRunner) failForReviewer(ctx context.Context, task domain.AgentTask, node domain.WorkflowNode, cause string) error {
	reason := domain.WorkflowReasonPrefix + node.Label + " cannot review · " + cause
	apply := repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID, Hold: true, FailureClass: domain.FailureProvider,
		Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: truncateRunes(reason, 2000)}}
	_, err := w.tasks.ApplyWorkflow(ctx, task, apply)
	return err
}
