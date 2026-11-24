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
//     happen keeps the task on that step with the reason, for a person, except
//     a branch that now conflicts with its base, which goes back to the step
//     that changes code at once (KUA-264).
//   - Before each agent review, a branch behind its base is brought up to
//     date, and a conflicting one is sent back as review feedback.
//   - While a review, a ship approval or a merge step waits, each wake (a
//     push to the base wakes it, KUA-257) reads the pull request again and
//     does the same (followBase, KUA-264): behind is brought up to date, a
//     conflict goes back. Every update of a published commit before a
//     person approves its merge, the CI step's included, counts toward one
//     bound (maxMergeUpdates, read from the commits by verifiedUpdates).
//   - The approved merge has its own maxMergeUpdates, counted from the head
//     the person approved (KUA-277): the gate pins it on the first pass,
//     taking Kuayle's verified updates of the published commit made before
//     the approval, however many (mergeGate.pin).
//
// A ship approval whose approved path runs through agent steps to a done end
// (WorkflowGraph.ShipApproval's ShipMergesAfterAgents, KUA-216) is entered
// the same way. Approving it records the merge on the approval, deferred and
// pinned to the published commit and its pull request; the agent steps run;
// the done end they reach carries the merge out instead of completing at
// once, and the task is done only through it. The agent steps' attempts
// publish no pull request of their own, so the merge keeps the one pinned at
// the approval. Commits the agent steps added on top of the approved commit
// are merged once their checks pass; a head that no longer contains the
// approved commit, failing checks, or a merge that cannot happen fail the
// task with the reason, since nobody decides at an end; a pull request a
// person merged on GitHub meanwhile completes it.
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

// prepareBranch reads the task's open pull request (readBranchFacts) and
// brings it up to date with its base when it is behind. Entering a ship
// approval or a review calls it, and so does every wake of a step that waits
// (KUA-264). It returns what it did for the step's reason, or, when the branch
// conflicts or shares a migration version with its base, the instruction that
// sends it back, and, when it acted on the facts, what it did with them for
// the step to store (KUA-265). GitHub that cannot be reached, a fact it has
// not reported or a refusal for another reason changes nothing: the next
// wake, or the merge's own gate, decides later.
//
// Only the published commit or Kuayle's verified updates of it are updated,
// and only while they are fewer than maxMergeUpdates (publishedUpdates):
// every step's updates before a person approves the merge share that bound.
// The approved merge does not: it takes those updates, however many, and has
// its own bound from there (mergeGate.pin, KUA-277).
func (w *WorkflowRunner) prepareBranch(ctx context.Context, task domain.AgentTask, ready bool) (done []string, conflict string, acted *domain.WorkflowBranchRecord) {
	if w.pulls == nil {
		return nil, "", nil
	}
	target, err := w.tasks.WorkflowPullRequest(ctx, task)
	if err != nil {
		return nil, "", nil
	}
	facts, _ := readBranchFacts(ctx, w.pulls, task.WorkspaceID, target.Repository, target.PullRequestNumber)
	if facts.PRExists != FactYes || facts.Merged != FactNo || facts.Closed != FactNo {
		return nil, "", nil
	}
	base := facts.BaseRef
	if base == "" {
		base = target.BaseBranch
	}
	now := w.now()
	if facts.MigrationCollision == FactYes {
		return nil, facts.MigrationRepair, branchRecord(domain.WorkflowBranchMigrationCollision, facts, base, now)
	}
	if ready && facts.PRDraft == FactYes {
		if err := w.pulls.SetPullRequestDraft(ctx, task.WorkspaceID, target.Repository, target.PullRequestNumber, false); err != nil {
			log.WithFields(log.Fields{"task_id": task.ID}).WithError(err).Warn("The workflow could not mark the pull request ready for review")
		} else {
			audit.Log("pull_request.marked_ready_by_workflow", task.CreatorID, map[string]interface{}{"workspace_id": task.WorkspaceID, "task_id": task.ID, "repository": target.Repository, "pull_request": target.PullRequestNumber})
			done = append(done, "marked the pull request ready for review")
		}
	}
	if facts.Conflict == FactYes {
		return done, rebaseInstruction(base), branchRecord(domain.WorkflowBranchConflicted, facts, base, now)
	}
	// Behind and not known to conflict (GitHub's update-branch says if it
	// does); a collision check GitHub could not run holds the update.
	if facts.HeadContainsBase != FactNo || facts.MigrationCollision != FactNo {
		return done, "", nil
	}
	updates := w.publishedUpdates(ctx, task, target, base, facts.HeadSHA)
	switch {
	case updates < 0:
		return done, "", nil
	case updates >= maxMergeUpdates:
		done = append(done, fmt.Sprintf("left the branch behind %s: it already brought it up to date %d times", base, updates))
		acted = branchRecord(domain.WorkflowBranchUpdateLimit, facts, base, now)
		acted.Updates = updates
		return done, "", acted
	}
	switch head, updated, conflicts := w.updateBranch(ctx, task, target, facts.HeadSHA, facts.Behind); {
	case conflicts:
		acted = branchRecord(domain.WorkflowBranchConflicted, facts, base, now)
		acted.Conflict = string(FactYes) // GitHub's update-branch said so
		return done, rebaseInstruction(base), acted
	case updated:
		done = append(done, "brought the branch up to date with "+base)
		acted = branchRecord(domain.WorkflowBranchUpdated, facts, base, now)
		acted.Updates = updates + 1
		if head != facts.HeadSHA {
			acted.UpdatedTo = head
		}
		return done, "", acted
	}
	return done, "", nil
}

// publishedUpdates counts the updates Kuayle made of the published commit
// that head, its pull request's head, is: 0 for the commit itself, or the
// length of the chain of Kuayle's verified updates of it (verifiedUpdates).
// The count is read from the commits themselves, so the CI step and the
// other steps share it, across restarts. It is -1 for a head someone else
// pushed, which is not Kuayle's to update (the merge refuses it), for a chain
// longer than maxMergeUpdates (approved merges made it), and for a chain
// GitHub could not show, which waits for the next pass.
func (w *WorkflowRunner) publishedUpdates(ctx context.Context, task domain.AgentTask, target *domain.AgentReviewTarget, base, head string) int {
	updates, refused, err := w.verifiedUpdates(ctx, task, target, base, head, target.CommitSHA)
	if err != nil || refused != "" {
		return -1
	}
	return updates
}

// branchRecord is what a step did (action, domain.WorkflowBranchRecord) on
// the facts it read about its pull request's branch, to store with the step
// (KUA-265). CI is set only when it was read (a merge).
func branchRecord(action string, f TaskFacts, base string, at time.Time) *domain.WorkflowBranchRecord {
	record := &domain.WorkflowBranchRecord{Action: action, At: at, PullRequest: f.PullRequest, Head: f.HeadSHA, Base: base, Behind: f.Behind,
		Conflict: string(f.Conflict), MigrationCollision: string(f.MigrationCollision)}
	if f.CI.State != "" || f.CI.Err != nil {
		record.CI = string(f.CI.Green)
	}
	return record
}

// updateBranch asks GitHub to merge the base into the task's pull request
// while its head is still head (update-branch). It returns the new head (""
// when it could not be read), whether GitHub took the update, and whether it
// refused it because the branch conflicts with its base. Any other refusal
// is logged and changes nothing.
func (w *WorkflowRunner) updateBranch(ctx context.Context, task domain.AgentTask, target *domain.AgentReviewTarget, head string, behind int) (string, bool, bool) {
	updated, err := w.pulls.UpdatePullRequestBranch(ctx, task.WorkspaceID, target.Repository, target.PullRequestNumber, head)
	var api *gh.APIError
	var unsynced *HeadSyncError
	switch {
	case err == nil, errors.As(err, &unsynced):
		audit.Log("pull_request.branch_updated_by_workflow", task.CreatorID, map[string]interface{}{"workspace_id": task.WorkspaceID, "task_id": task.ID, "repository": target.Repository, "pull_request": target.PullRequestNumber, "behind": behind, "head": updated})
		return updated, true, false
	case errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(api.Message), "conflict"):
		return "", false, true
	}
	log.WithFields(log.Fields{"task_id": task.ID}).WithError(err).Warn("The workflow could not bring the pull request up to date")
	return "", false, false
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
	// The gate pins the head on the first pass (mergeGate.pin).
	wait := newApprovedMerge(target.CommitSHA, "", w.now())
	wait.ActorID = &actor
	wait.Repository, wait.PullRequest, wait.BaseBranch = target.Repository, target.PullRequestNumber, target.BaseBranch
	return wait, target.PullRequestNumber, nil
}

// deferredShipMerge is the merge a done end carries out (KUA-216): the one a
// person approved at the ship approval before the agent steps that led to it
// (repository.DeferredShipMerge), now started. Its head is pinned on its first
// pass, and it may take the commits the agent steps added (AfterAgents). nil
// when there is none or Kuayle cannot merge here.
func (w *WorkflowRunner) deferredShipMerge(ctx context.Context, task domain.AgentTask, now time.Time) (*domain.MergeWait, error) {
	if w.pulls == nil || w.merger == nil {
		return nil, nil
	}
	wait, err := w.tasks.DeferredShipMerge(ctx, task)
	if err != nil || wait == nil || wait.Failure != "" || wait.Repository == "" || wait.PullRequest <= 0 || wait.ActorID == nil {
		return nil, err
	}
	started := newApprovedMerge(wait.Approved, wait.Method, now)
	started.ActorID, started.AfterAgents = wait.ActorID, true
	started.Repository, started.PullRequest, started.BaseBranch = wait.Repository, wait.PullRequest, wait.BaseBranch
	return started, nil
}

// ShipMergeWaiting reports whether approving the step the task waits on
// merges its pull request: it is the workflow's ship approval and Kuayle
// merges here (KUA-216). The task card and the approval say so.
func (w *WorkflowRunner) ShipMergeWaiting(ctx context.Context, task domain.AgentTask) (bool, error) {
	if w.pulls == nil || w.merger == nil || !task.RunsWorkflow() || task.WorkflowNode == nil || task.WorkflowStatus == nil ||
		*task.WorkflowStatus != domain.WorkflowStatusWaiting || task.State != "awaiting_review" || task.WorkflowPausedAt != nil {
		return false, nil
	}
	graph, profiles, err := w.graph(ctx, task)
	if err != nil {
		return false, err
	}
	node, _ := graph.Node(*task.WorkflowNode)
	return graph.ShipApproval(node, profiles) != "", nil
}

// AcceptAtShip is Accept on a task that waits at its workflow's ship
// approval (KUA-216): accepting the work there is the go-ahead to ship, so it
// approves, and Kuayle merges with every gate before the task is done. It
// returns false for any other task, which Accept completes without merging.
// Only the task's creator accepts, as Accept allows; a repeated Accept while
// the approved merge runs returns the task.
func (w *WorkflowRunner) AcceptAtShip(ctx context.Context, ws, id uuid.UUID, generation int64) (*domain.AgentTask, bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok {
		return nil, false, nil
	}
	task, err := w.tasks.WorkflowDecisionTask(ctx, ws, id)
	if errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if task.CreatorID != access.UserID {
		return nil, false, nil
	}
	ship, err := w.ShipMergeWaiting(ctx, *task)
	if err != nil || !ship {
		return nil, false, err
	}
	if task.Generation != generation {
		return nil, true, repository.ErrAgentTaskConflict
	}
	step, err := w.tasks.CurrentWorkflowStep(ctx, *task)
	if err != nil {
		return nil, true, err
	}
	if mergeActive(*step) {
		return task, true, nil
	}
	decided, err := w.Decide(ctx, ws, id, task.WorkflowSequence, WorkflowDecisionApprove)
	if errors.Is(err, ErrWorkflowDecision) {
		err = repository.ErrAgentTaskConflict
	}
	return decided, true, err
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
	// read is what the merge last read of the pull request; a refusal
	// stores it with the step (KUA-265).
	var read *TaskFacts
	readBase := func() string {
		if read.BaseRef != "" {
			return read.BaseRef
		}
		return wait.BaseBranch
	}
	var refused *domain.WorkflowBranchRecord
	record := func(reason string, wake *time.Time, needsPerson bool) error {
		reason = domain.WorkflowReasonPrefix + node.Label + ": " + reason
		if !needsPerson && reason == task.Reason && *wait == before {
			// Nothing changed since the last pass: only the next one is set.
			return w.tasks.SetWorkflowWake(ctx, task, wake)
		}
		_, err := w.tasks.RecordWorkflowMerge(ctx, task, repository.WorkflowMerge{Sequence: step.Sequence, Merge: wait, Reason: reason, Wake: wake, NeedsPerson: needsPerson, Branch: refused})
		return err
	}
	fail := func(reason string) (bool, error) {
		wait.Failure, wait.Waiting = truncateRunes(reason, 1000), ""
		if read != nil {
			refused = branchRecord(domain.WorkflowBranchMergeRefused, *read, readBase(), now)
			refused.Refusal = wait.Failure
		}
		if node.Type == domain.WorkflowNodeEnd {
			// Nobody decides at an end: the approved merge is refused, and the
			// task fails with the reason (KUA-216). Retrying it, or merging on
			// GitHub, is a person's decision.
			apply := repository.WorkflowApply{Sequence: step.Sequence, Version: task.WorkflowVersionID, Hold: true,
				Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: domain.WorkflowReasonPrefix + node.Label + ": the approved merge was refused: " + lowerFirst(wait.Failure)}}
			_, err := w.tasks.ApplyWorkflow(ctx, task, apply)
			if errors.Is(err, repository.ErrAgentTaskConflict) {
				return true, nil
			}
			return true, err
		}
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
	target, err := w.mergeTarget(ctx, task, wait)
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
	// The first pass pins the head (mergeGate.pin): the published commit or
	// Kuayle's verified updates of it, with the merge's own bound from there.
	hold, err := w.gate().merge(ctx, task.WorkspaceID, actor, target.Repository, target.PullRequestNumber, wait, w.merger)
	if hold.facts != nil {
		read = hold.facts
	}
	if err != nil {
		if isGitHubPermissionDenied(err) {
			return fail("GitHub refused: the Kuayle GitHub App lacks the permission to merge or update this pull request")
		}
		return backoff(err)
	}
	if hold.mergedElsewhere && node.Type == domain.WorkflowNodeEnd {
		// A person merged it on GitHub while the end waited: the work shipped,
		// and nothing else completes this task (KUA-216).
		hold.merged = true
	}
	if hold.merged {
		// Kuayle's merge completes the task (MergeTaskPullRequest), which ends
		// its workflow as done; a task still waiting moves on as merged. A
		// done end's merge completes the task on that end: the attempt of the
		// agent steps before it published no pull request of its own, so the
		// merge cannot complete it (KUA-216).
		merged := fmt.Sprintf("Merged pull request #%d at %s", target.PullRequestNumber, shortSHA(wait.Head))
		if hold.mergedElsewhere {
			merged = hold.reason
		}
		var err error
		if node.Type == domain.WorkflowNodeEnd {
			_, err = w.tasks.ApplyWorkflow(ctx, task, repository.WorkflowApply{Sequence: step.Sequence, Version: task.WorkflowVersionID, Hold: true, Outcome: domain.WorkflowEndDone, Detail: merged,
				Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: domain.WorkflowReasonPrefix + node.Label + ": " + lowerFirst(merged)}})
		} else {
			err = w.Outcome(ctx, task, outcome, merged)
		}
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
	sendBack, action := hold.migrationRepair, domain.WorkflowBranchMigrationCollision
	if hold.conflict && node.Type != domain.WorkflowNodeEnd {
		// The base moved into a conflict: back to the step that changes code
		// at once, as entering the ship approval does (KUA-264), rather than
		// waiting for the approver to deny it.
		base := wait.Base
		if base == "" {
			base = target.BaseBranch
		}
		sendBack, action = rebaseInstruction(base), domain.WorkflowBranchConflicted
	}
	if sendBack != "" {
		var acted *domain.WorkflowCIRecord
		if read != nil {
			acted = &domain.WorkflowCIRecord{Branch: branchRecord(action, *read, readBase(), now)}
			if hold.conflict {
				acted.Branch.Conflict = string(FactYes) // GitHub's update-branch may be what said so
			}
		}
		err := w.outcome(ctx, task, domain.WorkflowBranchConflict, sendBack, "", acted)
		if errors.Is(err, repository.ErrAgentTaskConflict) {
			err = nil
		}
		return true, err
	}
	return fail(hold.reason)
}

// mergeTarget is the pull request the approved merge merges: the one the
// approval pinned (a merge a ship approval deferred to the done end after its
// agent steps, whose attempt published none), else the one the task's current
// attempt published.
func (w *WorkflowRunner) mergeTarget(ctx context.Context, task domain.AgentTask, wait *domain.MergeWait) (*domain.AgentReviewTarget, error) {
	if wait.AfterAgents && wait.Repository != "" && wait.PullRequest > 0 {
		return &domain.AgentReviewTarget{Repository: wait.Repository, PullRequestNumber: wait.PullRequest, BaseBranch: wait.BaseBranch, CommitSHA: wait.Approved}, nil
	}
	return w.tasks.WorkflowPullRequest(ctx, task)
}

// verifiedUpdates walks back from head to from through Kuayle's verified
// updates of it (mergeGate.updateChain), at most maxMergeUpdates of them: the
// bound every update before a person approves the merge counts toward (the
// CI step, the waiting steps). It returns how many there were, or why head is
// not from or such an update of it. The approved merge pins its head with a
// longer walk and has its own bound (mergeGate.pin, KUA-277).
func (w *WorkflowRunner) verifiedUpdates(ctx context.Context, task domain.AgentTask, target *domain.AgentReviewTarget, base, head, from string) (int, string, error) {
	return w.gate().updateChain(ctx, task.WorkspaceID, target.Repository, base, head, from, maxMergeUpdates)
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
