package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// Kuayle merges a pull request only when its head contains the base branch's
// current tip and the head's checks passed (KUA-194). CI checks the pull
// request head, not a merge with its base, so a head behind its base was never
// tested with the base's newest commits: two branches that each added the same
// migration number merged green one after the other. A branch behind its base
// is brought up to date with GitHub's update-branch and not merged at once, so
// CI runs on the new head first; a branch that conflicts with its base is sent
// back to be rebased. GitHub's branch protection still applies on top of this.
//
// One mechanism carries out every merge a person approved (KUA-207, KUA-209):
// the assistant's merge_pull_request and ship_pull_request approvals and a
// workflow's ship approval or merge step. It tracks the approved merge as a
// domain.MergeWait. After Kuayle's own update-branch the merge does not fail:
// it waits for the checks of the new head and merges it once they pass,
// applying every gate again on each pass. The new head is accepted only when
// it is exactly the update Kuayle asked for (verifyUpdate); any other change of
// the head ends the merge as before. A branch that falls behind again is
// updated again, at most maxMergeUpdates times; a wait ends after
// mergeWaitTimeout.

// mergeAttempts bounds the merges tried when GitHub reports that the base
// moved during the merge; each one is preceded by a fresh check.
const mergeAttempts = 3

const (
	// maxMergeUpdates bounds the branch updates Kuayle makes for one approved
	// merge; a branch behind its base after that many is not merged.
	maxMergeUpdates = 3
	// mergeWaitTimeout ends a wait whose checks never passed.
	mergeWaitTimeout = 2 * time.Hour
	// mergeWaitPoll is how often a waiting merge reads the pull request again,
	// and mergeWaitErrorPoll how long it backs off when GitHub could not be read.
	mergeWaitPoll      = time.Minute
	mergeWaitErrorPoll = 5 * time.Minute
)

// mergeHold is why Kuayle does not merge now; an empty reason lets it merge.
type mergeHold struct {
	reason string
	// GitHub's branch update cannot repair this collision, and the
	// approved commit must not be rewritten during a merge.
	migrationRepair string
	// pending is set when only the head's checks are still running (or not
	// registered yet), so the same head may merge once they pass.
	pending bool
	// updated is set when Kuayle just brought the branch up to date: the wait
	// now names the new head, whose checks have to pass first.
	updated bool
	// transient is set when GitHub's answer may change on its own: the
	// distance to the base is unknown, the check list could not all be read,
	// or GitHub is still creating the head Kuayle asked for.
	transient bool
	// merged is set when the pull request is already merged at the head
	// Kuayle merges (an earlier pass merged it).
	merged bool
	// mergedElsewhere is set when someone merged it at another head; reason
	// says so.
	mergedElsewhere bool
}

// waits reports whether a merge keeps waiting rather than ending: while the
// head's checks run, right after Kuayle updated the branch, and, once Kuayle
// updated it, while GitHub's answer may still change on its own.
func (h mergeHold) waits(wait *domain.MergeWait) bool {
	return h.pending || h.updated || (h.transient && wait.Updates > 0)
}

// newMergeWait is the merge of the approved commit, before any update.
func newMergeWait(commit, method string, now time.Time) *domain.MergeWait {
	return &domain.MergeWait{Approved: commit, Head: commit, Method: method, Since: now, Deadline: now.Add(mergeWaitTimeout)}
}

// mergeGate decides and carries out one approved merge against GitHub.
type mergeGate struct {
	pulls pullRequestReader
	now   func() time.Time
}

// updateMergeReader is what verifying Kuayle's own branch update reads.
// GitHubService implements it.
type updateMergeReader interface {
	CommitParents(ctx context.Context, ws uuid.UUID, repository, sha string) ([]string, error)
	OnBranch(ctx context.Context, ws uuid.UUID, repository, branch, sha string) (bool, error)
	// BranchChanges lists the files head changes on top of its merge base
	// with base (GitHub's base...head comparison); complete is false when the
	// list was cut short.
	BranchChanges(ctx context.Context, ws uuid.UUID, repository, base, head string) (files []gh.CompareFile, complete bool, err error)
}

// merge runs the gate for wait.Head and, when it lets the merge through,
// merges pinned to wait.Head, so a push that lands after the checks is refused
// by GitHub. GitHub's refusal because the base moved during the merge checks
// again, which updates a branch that is now behind, at most mergeAttempts
// times. It returns the hold (merged when the pull request merged); only a
// failure to reach GitHub is an error. wait is updated in place.
func (g mergeGate) merge(ctx context.Context, ws, actor uuid.UUID, repository string, number int, wait *domain.MergeWait, merger approvalMerger) (mergeHold, error) {
	method := wait.Method
	if method == "" {
		method = "squash"
	}
	for attempt := 1; ; attempt++ {
		// Without the pull request reader nothing but GitHub's own rules
		// stands between the approved commit and the merge.
		if g.pulls != nil {
			hold, err := g.check(ctx, ws, actor, repository, number, wait)
			if err != nil || hold.reason != "" || hold.merged {
				return hold, err
			}
		}
		err := merger.MergeTaskPullRequest(ctx, ws, repository, number, wait.Head, method)
		if err == nil {
			return mergeHold{merged: true}, nil
		}
		var refused *gh.ErrMergeRefused
		if errors.As(err, &refused) {
			if baseMovedRefusal(refused.Reason) && g.pulls != nil && attempt < mergeAttempts {
				continue
			}
			return mergeHold{reason: mergeRefusalMessage(refused.Reason, wait.Head)}, nil
		}
		if message, disabled := disabledAbilityMessage(err); disabled {
			return mergeHold{reason: message}, nil
		}
		return mergeHold{}, err
	}
}

// check reads the pull request from GitHub now and decides whether wait.Head
// may merge. A branch behind its base is updated here as the actor, pinned to
// wait.Head, and held. Only a failure to reach GitHub is an error.
func (g mergeGate) check(ctx context.Context, ws, actor uuid.UUID, repository string, number int, wait *domain.MergeWait) (mergeHold, error) {
	status, err := g.pulls.PullRequestStatus(ctx, ws, repository, number)
	if err != nil {
		return mergeHold{}, err
	}
	switch {
	case status.State == "merged" && status.HeadSHA == wait.Head:
		return mergeHold{merged: true}, nil
	case status.State == "merged":
		return mergeHold{mergedElsewhere: true, reason: fmt.Sprintf("Pull request #%d was merged outside Kuayle at %s, not at the approved %s", number, shortSHA(status.HeadSHA), shortSHA(wait.Head))}, nil
	case status.State != "open" || status.Draft:
		return mergeHold{}, nil // GitHub refuses the merge with its own reason
	}
	base := status.BaseRef
	if status.HeadSHA != wait.Head {
		if !wait.Updating {
			return mergeHold{reason: headMovedRefusal(status.HeadSHA)}, nil
		}
		// GitHub made the head Kuayle asked for, or someone pushed: only the
		// exact update is merged.
		why, err := g.verifyUpdate(ctx, ws, repository, base, wait.Head, status.HeadSHA)
		if err != nil {
			return mergeHold{}, err
		}
		if why != "" {
			return mergeHold{reason: updateNotVerifiedRefusal(status.HeadSHA, why)}, nil
		}
		wait.Head, wait.Updating = status.HeadSHA, false
	} else if wait.Updating {
		if status.Behind != 0 {
			return mergeHold{transient: true, reason: fmt.Sprintf("GitHub is still bringing pull request #%d up to date with %s", number, base)}, nil
		}
		wait.Updating = false
	}
	if status.migrationRepair != "" {
		return mergeHold{migrationRepair: status.migrationRepair,
			reason: status.migrationRepair + " Next step: update_pull_request_branch to send the repair to the task agent, then ask to merge again once its checks pass"}, nil
	}
	switch {
	case status.Behind < 0:
		return mergeHold{transient: true, reason: fmt.Sprintf("GitHub did not report whether pull request #%d contains the latest %s, so it was not merged. Next step: ask to merge again in a moment", number, base)}, nil
	case status.Behind > 0:
		if status.MergeableState == "dirty" || (status.Mergeable != nil && !*status.Mergeable) {
			return mergeHold{reason: branchConflictRefusal(number, base)}, nil
		}
		if wait.Updates >= maxMergeUpdates {
			return mergeHold{reason: fmt.Sprintf("Pull request #%d fell behind %s again after Kuayle brought it up to date %d times, so it was not merged. Next step: ask to merge again once %s is quieter", number, base, wait.Updates, base)}, nil
		}
		return g.updateBehindBranch(ctx, ws, actor, repository, number, wait, status)
	}
	head, checks, coverage, err := g.pulls.PullRequestChecks(ctx, ws, repository, number)
	if err != nil {
		return mergeHold{}, err
	}
	if head != wait.Head {
		return mergeHold{reason: headMovedRefusal(head)}, nil
	}
	summary, state, next := summarizeChecks(checks, coverage)
	at := shortSHA(wait.Head)
	switch state {
	case "failing":
		var failed []string
		for _, check := range checks {
			if check.State == "failed" {
				failed = append(failed, check.Name)
			}
		}
		return mergeHold{reason: fmt.Sprintf("Pull request #%d was not merged: %d check(s) failed on %s: %s. Next step: get_pull_request_checks shows the logs; fix them with follow_up_task, then ask to merge again once they pass", number, len(failed), at, truncateRunes(strings.Join(failed, ", "), 300))}, nil
	case "pending":
		return mergeHold{pending: true, reason: fmt.Sprintf("Pull request #%d was not merged: %d check(s) are still running on %s, and Kuayle merges a head only after its checks passed. Next step: follow them with get_pull_request_checks and ask to merge again once they pass", number, summary["pending"], at)}, nil
	case "incomplete":
		return mergeHold{transient: true, reason: fmt.Sprintf("Pull request #%d was not merged: it is not known that CI passed on %s. %s Then ask to merge again", number, at, next)}, nil
	case "none":
		return g.holdUncheckedHead(ctx, ws, repository, number, wait.Head, status)
	}
	return mergeHold{}, nil
}

// mergeGraceWindow is how long a head without checks is assumed to be still
// waiting for CI to register them.
const mergeGraceWindow = 2 * time.Minute

// holdUncheckedHead decides for an up-to-date head that reports no checks. A
// branch update or push registers its checks only after a moment, so such a
// head merges only in a repository without CI: none on the base's tip (which
// the head contains) and committed longer than mergeGraceWindow ago. A hold is
// pending, so a waiting merge keeps waiting for the checks to appear.
func (g mergeGate) holdUncheckedHead(ctx context.Context, ws uuid.UUID, repository string, number int, commit string, status *PullRequestStatus) (mergeHold, error) {
	waiting := fmt.Sprintf("Pull request #%d was not merged: no checks have reported on %s yet", number, shortSHA(commit))
	next := ". Next step: follow them with get_pull_request_checks and ask to merge again once they pass; if the repository's CI never runs on this branch, merge it on GitHub"
	if status.MergeBase == "" {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", and the tip of %s it contains is unknown, so it is not known whether the repository has CI", status.BaseRef) + next}, nil
	}
	checks, coverage, err := g.pulls.CommitChecks(ctx, ws, repository, status.MergeBase)
	if err != nil {
		return mergeHold{}, err
	}
	if len(checks) > 0 {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", though %s's tip has %d, so CI has not run on it", status.BaseRef, len(checks)) + next}, nil
	}
	if coverage.incomplete() {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", and the checks of %s's tip could not all be read, so it is not known whether the repository has CI", status.BaseRef) + next}, nil
	}
	committed, err := g.pulls.CommitDate(ctx, ws, repository, commit)
	if err != nil {
		return mergeHold{}, err
	}
	if g.now().Sub(committed) < mergeGraceWindow {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", and it was committed less than %s ago, so CI may not have started", mergeGraceWindow) + next}, nil
	}
	// No CI anywhere: GitHub's required checks, if any, still refuse the merge.
	return mergeHold{}, nil
}

// updateBehindBranch merges the base into a branch behind it with GitHub's
// update-branch, pinned to wait.Head, and holds the merge until CI ran on the
// new head. The new head is accepted only once verified as that update. A
// conflict is sent back to be rebased.
func (g mergeGate) updateBehindBranch(ctx context.Context, ws, actor uuid.UUID, repository string, number int, wait *domain.MergeWait, status *PullRequestStatus) (mergeHold, error) {
	base := status.BaseRef
	behind := fmt.Sprintf("Pull request #%d was %d commit(s) behind %s, and its CI never ran with the latest %s", number, status.Behind, base, base)
	waitFor := "Kuayle merges it once the checks on the new head pass"
	from := wait.Head
	head, err := g.pulls.UpdatePullRequestBranch(ctx, ws, repository, number, from)
	if err != nil {
		var unsynced *HeadSyncError
		var api *gh.APIError
		switch {
		case errors.As(err, &unsynced):
			// GitHub accepted the update; only reading the new head failed.
			audit.Log("pull_request.branch_updated_before_merge", actor, map[string]interface{}{"workspace_id": ws, "repository": repository, "pull_request": number, "behind": status.Behind})
			wait.Updates, wait.Updating, wait.Base = wait.Updates+1, true, base
			return mergeHold{updated: true, reason: behind + ". Kuayle asked GitHub to bring the branch up to date; " + waitFor}, nil
		case errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(api.Message), "conflict"):
			return mergeHold{reason: branchConflictRefusal(number, base)}, nil
		case errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(api.Message), "head"):
			return mergeHold{reason: "The pull request head moved after approval was requested; ask again to review the new commit"}, nil
		}
		if message, disabled := disabledAbilityMessage(err); disabled {
			return mergeHold{reason: behind + ", so it was not merged, and Kuayle could not update the branch: " + message + ". Next step: update_pull_request_branch, or bring it up to date on GitHub, then ask to merge again once its checks pass"}, nil
		}
		if message, permanent := pullRequestApprovalFailure("update_pull_request_branch", err); permanent {
			return mergeHold{reason: behind + ", so it was not merged, and Kuayle could not update the branch. " + message}, nil
		}
		return mergeHold{}, err
	}
	audit.Log("pull_request.branch_updated_before_merge", actor, map[string]interface{}{"workspace_id": ws, "repository": repository, "pull_request": number, "behind": status.Behind, "head": head})
	wait.Updates, wait.Base = wait.Updates+1, base
	if head == from {
		wait.Updating = true
		return mergeHold{updated: true, reason: behind + ". Kuayle asked GitHub to bring the branch up to date (GitHub is still creating the new head); " + waitFor}, nil
	}
	why, err := g.verifyUpdate(ctx, ws, repository, base, from, head)
	if err != nil {
		// The update happened; the next pass verifies the new head again.
		wait.Updating = true
		return mergeHold{updated: true, reason: behind + ". Kuayle brought the branch up to date; " + waitFor}, nil
	}
	if why != "" {
		return mergeHold{reason: updateNotVerifiedRefusal(head, why)}, nil
	}
	wait.Head, wait.Updating = head, false
	return mergeHold{updated: true, reason: fmt.Sprintf("%s. Kuayle brought the branch up to date with GitHub's update-branch (new head %s); %s", behind, shortSHA(head), waitFor)}, nil
}

// maxVerifiedFiles is the most changed files the update check compares.
const maxVerifiedFiles = 300

// verifyUpdate checks that head is exactly the merge of the base branch into
// from that GitHub's update-branch makes, so merging head merges what was
// approved plus the base, and nothing else. It returns "" when verified, or
// why not:
//
//   - head's parents are exactly from and one commit, B, that is on the base
//     branch (GitHub's update-branch merges the base's tip into the head);
//   - the files and changed lines the pull request adds on top of B (GitHub's
//     B...head comparison) equal those from adds on top of its merge base
//     with B (B...from), the change that was approved. A merge that carries
//     anything else (a reverted base commit, an extra edit) changes some file
//     differently and is refused. Hunk positions and context lines may differ,
//     because base commits moved them; a file without a patch (binary or very
//     large) must have the same blob.
//
// A comparison GitHub cuts short is not verified.
func (g mergeGate) verifyUpdate(ctx context.Context, ws uuid.UUID, repository, base, from, head string) (string, error) {
	reader, ok := g.pulls.(updateMergeReader)
	if !ok {
		return "Kuayle cannot read the new head's commits", nil
	}
	parents, err := reader.CommitParents(ctx, ws, repository, head)
	if err != nil {
		return "", err
	}
	if len(parents) != 2 || parents[0] != from {
		return fmt.Sprintf("it is not a merge of %s into %s", base, shortSHA(from)), nil
	}
	merged := parents[1]
	onBase, err := reader.OnBranch(ctx, ws, repository, base, merged)
	if err != nil {
		return "", err
	}
	if !onBase {
		return fmt.Sprintf("the commit it merges in, %s, is not on %s", shortSHA(merged), base), nil
	}
	after, completeAfter, err := reader.BranchChanges(ctx, ws, repository, merged, head)
	if err != nil {
		return "", err
	}
	before, completeBefore, err := reader.BranchChanges(ctx, ws, repository, merged, from)
	if err != nil {
		return "", err
	}
	if !completeAfter || !completeBefore || len(after) > maxVerifiedFiles || len(before) > maxVerifiedFiles {
		return "the pull request changes too many files to compare", nil
	}
	if file := changedDifferently(before, after); file != "" {
		return fmt.Sprintf("it changes %s differently from the approved commit", file), nil
	}
	return "", nil
}

// changedDifferently names a file the two change lists treat differently, or
// returns "" when they make the same change.
func changedDifferently(before, after []gh.CompareFile) string {
	byName := make(map[string]gh.CompareFile, len(before))
	for _, file := range before {
		byName[file.Filename] = file
	}
	names := make([]string, 0, len(after))
	for _, file := range after {
		names = append(names, file.Filename)
	}
	for _, file := range before {
		names = append(names, file.Filename)
	}
	sort.Strings(names)
	afterByName := make(map[string]gh.CompareFile, len(after))
	for _, file := range after {
		afterByName[file.Filename] = file
	}
	for _, name := range names {
		was, okBefore := byName[name]
		now, okAfter := afterByName[name]
		if !okBefore || !okAfter || was.Status != now.Status {
			return name
		}
		if was.Patch == "" || now.Patch == "" {
			if was.SHA != now.SHA {
				return name
			}
			continue
		}
		if !sameChangedLines(was.Patch, now.Patch) {
			return name
		}
	}
	return ""
}

// sameChangedLines compares the added and removed lines of two patches in
// order, ignoring hunk headers and context lines.
func sameChangedLines(a, b string) bool {
	changed := func(patch string) []string {
		var lines []string
		for _, line := range strings.Split(patch, "\n") {
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
				lines = append(lines, line)
			}
		}
		return lines
	}
	x, y := changed(a), changed(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// branchConflictRefusal sends a branch that cannot be updated on GitHub back
// to the task agent.
func branchConflictRefusal(number int, base string) string {
	return fmt.Sprintf("Pull request #%d was not merged: the branch conflicts with %s; needs a rebase. Next step: update_pull_request_branch so the task agent merges %s and resolves the conflicts, then ask to merge again once its checks pass", number, base, base)
}

func headMovedRefusal(head string) string {
	return fmt.Sprintf("The pull request head moved to %s after approval was requested; ask again to review the new commit", shortSHA(head))
}

// updateNotVerifiedRefusal ends a merge whose new head is not the update
// Kuayle asked for.
func updateNotVerifiedRefusal(head, why string) string {
	return fmt.Sprintf("The pull request head moved to %s, which is not only the branch update Kuayle made (%s), so it was not merged; ask again to review the new commit", shortSHA(head), why)
}

// baseMovedRefusal reports whether GitHub refused a merge because the head
// no longer contains its base: the base moved while merging, or branch
// protection requires an up-to-date branch. Checking again then decides.
func baseMovedRefusal(reason string) bool {
	lower := strings.ToLower(reason)
	return strings.Contains(lower, "base branch was modified") || strings.Contains(lower, "not up to date") || strings.Contains(lower, "out of date")
}

// mergeWaitExpiredRefusal ends a wait whose checks did not pass in time.
func mergeWaitExpiredRefusal(number int, wait *domain.MergeWait) string {
	message := fmt.Sprintf("Pull request #%d was not merged: Kuayle waited %s for the checks on %s after bringing the branch up to date, and they did not pass", number, mergeWaitTimeout, shortSHA(wait.Head))
	if wait.Waiting != "" {
		message += " (" + truncateRunes(wait.Waiting, 300) + ")"
	}
	return message + ". Next step: get_pull_request_checks shows them; ask to merge again once they pass"
}
