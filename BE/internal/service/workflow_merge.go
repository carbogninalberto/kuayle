package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

// A workflow merges its task's pull request through the same mechanism as the
// assistant's approved merges (mergeGate, KUA-207), when a person approves its
// ship approval (KUA-209). The ship approval is the approval whose approved
// transition leads to a done end or a merge step (WorkflowGraph.ShipApproval).
//
//   - Entering it, Kuayle marks a draft pull request ready for review and
//     brings a branch behind its base up to date; a branch that conflicts
//     goes back to the first step that changes code, as an iteration with
//     the instruction to merge the base and resolve the conflicts.
//   - Approving it records the merge on the step that carries it out, pinned
//     to the commit the task published: the approval itself when it leads to
//     done, or the merge step after it. The worker then merges as the
//     approver once every gate passes, waiting for the checks of a branch it
//     updated; the task completes only through the merge. A merge that cannot
//     happen keeps the task on that step with the reason, for a person.
//   - Before each agent review, a branch behind its base is brought up to
//     date, and a conflicting one is sent back as review feedback.
//
// A merge step reached without an approval keeps waiting for a person to
// merge (from chat or on GitHub), as before.

// SetPullRequests lets ship approvals and merge steps merge the task's pull
// request, and branches be brought up to date before reviews.
func (w *WorkflowRunner) SetPullRequests(pulls pullRequestReader, merger approvalMerger) {
	w.pulls, w.merger = pulls, merger
}

func (w *WorkflowRunner) gate() mergeGate { return mergeGate{pulls: w.pulls, now: w.now} }

// rebaseInstruction is what the step that changes code is told when the
// pull request conflicts with its base. Publication never force-pushes, so
// the base is merged in.
func rebaseInstruction(base string) string {
	return fmt.Sprintf("The pull request conflicts with %s, so it cannot be reviewed or merged. Bring the branch up to date (rebase onto %s in effect): fetch origin, merge origin/%s into the task branch (a merge commit; no rebase or force-push), resolve the conflicts keeping both sides' intent, run the checks, and finish.", base, base, base)
}

func pullRequestConflicts(status *PullRequestStatus) bool {
	return status.MergeableState == "dirty" || (status.Mergeable != nil && !*status.Mergeable)
}

// prepareBranch brings the task's open pull request up to date with its base
// when it is behind. It returns what it did for the step's reason, or, when
// the branch conflicts, the instruction that sends it back. GitHub that
// cannot be reached or refuses for another reason changes nothing: the
// merge's own gate decides later.
func (w *WorkflowRunner) prepareBranch(ctx context.Context, task domain.AgentTask, ready bool) (done []string, conflict string) {
	if w.pulls == nil {
		return nil, ""
	}
	target, err := w.tasks.WorkflowPullRequest(ctx, task)
	if err != nil {
		return nil, ""
	}
	status, err := w.pulls.PullRequestStatus(ctx, task.WorkspaceID, target.Repository, target.PullRequestNumber)
	if err != nil || status.State != "open" {
		return nil, ""
	}
	base := status.BaseRef
	if base == "" {
		base = target.BaseBranch
	}
	if ready && status.Draft {
		if err := w.pulls.SetPullRequestDraft(ctx, task.WorkspaceID, target.Repository, target.PullRequestNumber, false); err != nil {
			log.WithFields(log.Fields{"task_id": task.ID}).WithError(err).Warn("The workflow could not mark the pull request ready for review")
		} else {
			audit.Log("pull_request.marked_ready_by_workflow", task.CreatorID, map[string]interface{}{"workspace_id": task.WorkspaceID, "task_id": task.ID, "repository": target.Repository, "pull_request": target.PullRequestNumber})
			done = append(done, "marked the pull request ready for review")
		}
	}
	if pullRequestConflicts(status) {
		return done, rebaseInstruction(base)
	}
	if status.Behind <= 0 {
		return done, ""
	}
	head, err := w.pulls.UpdatePullRequestBranch(ctx, task.WorkspaceID, target.Repository, target.PullRequestNumber, status.HeadSHA)
	var api *gh.APIError
	var unsynced *HeadSyncError
	switch {
	case err == nil, errors.As(err, &unsynced):
		audit.Log("pull_request.branch_updated_by_workflow", task.CreatorID, map[string]interface{}{"workspace_id": task.WorkspaceID, "task_id": task.ID, "repository": target.Repository, "pull_request": target.PullRequestNumber, "behind": status.Behind, "head": head})
		done = append(done, "brought the branch up to date with "+base)
	case errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(api.Message), "conflict"):
		return done, rebaseInstruction(base)
	default:
		log.WithFields(log.Fields{"task_id": task.ID}).WithError(err).Warn("The workflow could not bring the pull request up to date")
	}
	return done, ""
}

// startMerge records a person's approval of a ship approval as the merge to
// carry out on the step: pinned to the commit the task published, as the
// approver in ctx. The worker merges it (Evaluate). Without a published pull
// request there is nothing to merge, and it returns false.
func (w *WorkflowRunner) startMerge(ctx context.Context, ws uuid.UUID, task domain.AgentTask, node domain.WorkflowNode) (*domain.AgentTask, bool, error) {
	wait, number, err := w.approvedMerge(ctx, task)
	if err != nil || wait == nil {
		return nil, false, err
	}
	now := w.now()
	reason := fmt.Sprintf("%s%s: approved; Kuayle is merging pull request #%d once every check passes", domain.WorkflowReasonPrefix, node.Label, number)
	merged, err := w.tasks.DecideWorkflowMerge(ctx, ws, task.ID, repository.WorkflowMerge{Sequence: task.WorkflowSequence, Merge: wait, Reason: reason, Wake: &now,
		Notes: []domain.NotebookNote{{Event: fmt.Sprintf("A person chose %q at %s; Kuayle merges the pull request", WorkflowDecisionApprove, node.Label)}}})
	return merged, true, err
}

// approvedMerge is the merge a person's approval in ctx starts and its pull
// request's number, or nil when the task published no pull request.
func (w *WorkflowRunner) approvedMerge(ctx context.Context, task domain.AgentTask) (*domain.MergeWait, int, error) {
	target, err := w.tasks.WorkflowPullRequest(ctx, task)
	if errors.Is(err, repository.ErrNoReviewTarget) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.UserID == uuid.Nil {
		return nil, 0, repository.ErrConversationNotFound
	}
	actor := access.UserID
	wait := newMergeWait(target.CommitSHA, "", w.now())
	// Head is pinned on the first pass (pinMerge): the published commit, or
	// Kuayle's verified updates of it.
	wait.Head, wait.ActorID = "", &actor
	return wait, target.PullRequestNumber, nil
}

// mergeActive reports whether the step carries an approved merge that has
// not ended.
func mergeActive(step domain.WorkflowStep) bool {
	wait := step.MergeWait()
	return wait != nil && wait.Failure == ""
}

// driveMerge runs one pass of the merge the step carries, as its approver.
// It returns false when the merge ended without merging, so the step waits
// for a person again and Evaluate goes on as usual.
func (w *WorkflowRunner) driveMerge(ctx context.Context, task domain.AgentTask, node domain.WorkflowNode, step domain.WorkflowStep, wait *domain.MergeWait) (bool, error) {
	now := w.now()
	outcome := domain.WorkflowApprovalApproved
	if node.Type == domain.WorkflowNodeMerge {
		outcome = domain.WorkflowMerged
	}
	before := *wait
	record := func(reason string, wake *time.Time, needsPerson bool) error {
		reason = domain.WorkflowReasonPrefix + node.Label + ": " + reason
		if !needsPerson && reason == task.Reason && *wait == before {
			// Nothing changed since the last pass: only the next one is set.
			return w.tasks.SetWorkflowWake(ctx, task, wake)
		}
		_, err := w.tasks.RecordWorkflowMerge(ctx, task, repository.WorkflowMerge{Sequence: step.Sequence, Merge: wait, Reason: reason, Wake: wake, NeedsPerson: needsPerson})
		return err
	}
	fail := func(reason string) (bool, error) {
		wait.Failure, wait.Waiting = truncateRunes(reason, 1000), ""
		next := "Approve again once it is fixed, or deny"
		if node.Type == domain.WorkflowNodeMerge {
			next = "Merge it from chat or on GitHub once it is fixed"
		}
		return false, record("approved, but "+lowerFirst(reason)+". "+next, nil, true)
	}
	if w.pulls == nil || w.merger == nil || wait.ActorID == nil {
		return fail("Kuayle cannot merge pull requests here")
	}
	actor := *wait.ActorID
	// The approver must still be allowed to decide the task's workflow.
	if _, err := w.tasks.WorkflowDecisionTask(domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: actor}), task.WorkspaceID, task.ID); err != nil {
		if errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return fail("The person who approved can no longer merge this task's pull request")
		}
		return true, err
	}
	target, err := w.tasks.WorkflowPullRequest(ctx, task)
	if errors.Is(err, repository.ErrNoReviewTarget) {
		return fail("The task has no published pull request to merge")
	}
	if err != nil {
		return true, err
	}
	backoff := func(err error) (bool, error) {
		if !now.Before(wait.Deadline) {
			return fail(mergeWaitExpiredRefusal(target.PullRequestNumber, wait))
		}
		log.WithFields(log.Fields{"task_id": task.ID}).WithError(err).Warn("A workflow merge could not reach GitHub; it tries again later")
		at := now.Add(mergeWaitErrorPoll)
		return true, record("approved; Kuayle could not reach GitHub to merge pull request #"+fmt.Sprint(target.PullRequestNumber)+" and tries again shortly", &at, false)
	}
	if wait.Head == "" {
		why, err := w.pinMerge(ctx, task, target, wait)
		if err != nil {
			return backoff(err)
		}
		if why != "" {
			return fail(why)
		}
	}
	hold, err := w.gate().merge(ctx, task.WorkspaceID, actor, target.Repository, target.PullRequestNumber, wait, w.merger)
	if err != nil {
		if isGitHubPermissionDenied(err) {
			return fail("GitHub refused: the Kuayle GitHub App lacks the permission to merge or update this pull request")
		}
		return backoff(err)
	}
	if hold.merged {
		// Kuayle's merge completes the task (MergeTaskPullRequest), which ends
		// its workflow as done; a task still waiting moves on as merged.
		err := w.Outcome(ctx, task, outcome, fmt.Sprintf("Merged pull request #%d at %s", target.PullRequestNumber, shortSHA(wait.Head)))
		if errors.Is(err, repository.ErrAgentTaskConflict) {
			return true, nil
		}
		return true, err
	}
	if hold.waits(wait) {
		wait.Waiting = truncateRunes(hold.reason, 600)
		if !now.Before(wait.Deadline) {
			return fail(mergeWaitExpiredRefusal(target.PullRequestNumber, wait))
		}
		at := now.Add(mergeWaitPoll)
		return true, record("approved; "+lowerFirst(wait.Waiting), &at, false)
	}
	return fail(hold.reason)
}

// pinMerge fixes the head the approved merge merges: the commit the task
// published, or a chain of Kuayle's verified updates of it (made when the
// ship approval was entered). Anything else on the branch was not approved.
// It also resolves the merge method.
func (w *WorkflowRunner) pinMerge(ctx context.Context, task domain.AgentTask, target *domain.AgentReviewTarget, wait *domain.MergeWait) (string, error) {
	status, err := w.pulls.PullRequestStatus(ctx, task.WorkspaceID, target.Repository, target.PullRequestNumber)
	if err != nil {
		return "", err
	}
	head, base := status.HeadSHA, status.BaseRef
	if base == "" {
		base = target.BaseBranch
	}
	var chain []string
	for current := head; current != wait.Approved; {
		if len(chain) >= maxMergeUpdates {
			return updateNotVerifiedRefusal(head, "it is not the published commit or Kuayle's update of it"), nil
		}
		reader, ok := w.pulls.(updateMergeReader)
		if !ok {
			return headMovedRefusal(head), nil
		}
		parents, err := reader.CommitParents(ctx, task.WorkspaceID, target.Repository, current)
		if err != nil {
			return "", err
		}
		if len(parents) != 2 {
			return updateNotVerifiedRefusal(head, "it is not the published commit or Kuayle's update of it"), nil
		}
		why, err := w.gate().verifyUpdate(ctx, task.WorkspaceID, target.Repository, base, parents[0], current)
		if err != nil {
			return "", err
		}
		if why != "" {
			return updateNotVerifiedRefusal(head, why), nil
		}
		chain = append(chain, current)
		current = parents[0]
	}
	method, err := w.pulls.ResolveMergeMethod(ctx, task.WorkspaceID, target.Repository)
	if errors.Is(err, ErrMergeMethodNotAllowed) {
		return upperFirst(truncateRunes(err.Error(), 200)) + ". Choose a method the repository allows in the development settings", nil
	}
	if err != nil {
		return "", err
	}
	wait.Head, wait.Updates, wait.Method, wait.Base = head, len(chain), method, base
	return "", nil
}

// branchConflict is the instruction an entered node finished with at once
// because the pull request conflicts with its base (a review step's changes
// requested, a ship approval's conflict), or "".
func branchConflict(node domain.WorkflowNode, immediate, why string, profiles map[uuid.UUID]domain.AgentProfile) string {
	switch {
	case immediate == domain.WorkflowBranchConflict:
		return why
	case immediate == domain.WorkflowReviewChangesRequested && node.Type == domain.WorkflowNodeAgent && node.ProfileID != nil && profiles[*node.ProfileID].Reviews():
		return why
	}
	return ""
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
