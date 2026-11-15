package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// Kuayle merges a pull request only when its head contains the base branch's
// current tip and the head's checks passed (KUA-194). CI checks the pull
// request head, not a merge with its base, so a head behind its base was never
// tested with the base's newest commits: two branches that each added the same
// migration number merged green one after the other. A branch behind its base
// is brought up to date with GitHub's update-branch and not merged, so CI runs
// on the new head first; a branch that conflicts with its base is sent back to
// be rebased. GitHub's branch protection still applies on top of this.

// mergeAttempts bounds the merges tried when GitHub reports that the base
// moved during the merge; each one is preceded by a fresh check.
const mergeAttempts = 3

// mergeHold is why Kuayle does not merge now; an empty reason lets it merge.
type mergeHold struct {
	reason string
	// pending is set when only the head's checks are still running, so the
	// same head may merge once they pass.
	pending bool
}

// mergeGate reads the pull request from GitHub now and decides whether the
// approved commit may merge. A branch behind its base is updated here as the
// actor, pinned to commit, and held. Only a failure to reach GitHub is an error.
func (s *AssistantApprovals) mergeGate(ctx context.Context, ws, actor uuid.UUID, repository string, number int, commit string) (mergeHold, error) {
	status, err := s.pulls.PullRequestStatus(ctx, ws, repository, number)
	if err != nil {
		return mergeHold{}, err
	}
	if status.State != "open" || status.Draft {
		return mergeHold{}, nil // GitHub refuses the merge with its own reason
	}
	if status.HeadSHA != commit {
		return mergeHold{reason: headMovedRefusal(status.HeadSHA)}, nil
	}
	base := status.BaseRef
	switch {
	case status.Behind < 0:
		return mergeHold{reason: fmt.Sprintf("GitHub did not report whether pull request #%d contains the latest %s, so it was not merged. Next step: ask to merge again in a moment", number, base)}, nil
	case status.Behind > 0:
		if status.MergeableState == "dirty" || (status.Mergeable != nil && !*status.Mergeable) {
			return mergeHold{reason: branchConflictRefusal(number, base)}, nil
		}
		return s.updateBehindBranch(ctx, ws, actor, repository, number, commit, status)
	}
	head, checks, coverage, err := s.pulls.PullRequestChecks(ctx, ws, repository, number)
	if err != nil {
		return mergeHold{}, err
	}
	if head != commit {
		return mergeHold{reason: headMovedRefusal(head)}, nil
	}
	summary, state, next := summarizeChecks(checks, coverage)
	at := shortSHA(commit)
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
		return mergeHold{reason: fmt.Sprintf("Pull request #%d was not merged: it is not known that CI passed on %s. %s Then ask to merge again", number, at, next)}, nil
	case "none":
		return s.holdUncheckedHead(ctx, ws, repository, number, commit, status)
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
// pending, so the ship chain keeps waiting for the checks to appear.
func (s *AssistantApprovals) holdUncheckedHead(ctx context.Context, ws uuid.UUID, repository string, number int, commit string, status *PullRequestStatus) (mergeHold, error) {
	waiting := fmt.Sprintf("Pull request #%d was not merged: no checks have reported on %s yet", number, shortSHA(commit))
	next := ". Next step: follow them with get_pull_request_checks and ask to merge again once they pass; if the repository's CI never runs on this branch, merge it on GitHub"
	if status.MergeBase == "" {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", and the tip of %s it contains is unknown, so it is not known whether the repository has CI", status.BaseRef) + next}, nil
	}
	checks, coverage, err := s.pulls.CommitChecks(ctx, ws, repository, status.MergeBase)
	if err != nil {
		return mergeHold{}, err
	}
	if len(checks) > 0 {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", though %s's tip has %d, so CI has not run on it", status.BaseRef, len(checks)) + next}, nil
	}
	if coverage.incomplete() {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", and the checks of %s's tip could not all be read, so it is not known whether the repository has CI", status.BaseRef) + next}, nil
	}
	committed, err := s.pulls.CommitDate(ctx, ws, repository, commit)
	if err != nil {
		return mergeHold{}, err
	}
	now := time.Now
	if s.ships != nil {
		now = s.ships.now // the ship chain's clock, which its waits advance
	}
	if now().Sub(committed) < mergeGraceWindow {
		return mergeHold{pending: true, reason: waiting + fmt.Sprintf(", and it was committed less than %s ago, so CI may not have started", mergeGraceWindow) + next}, nil
	}
	// No CI anywhere: GitHub's required checks, if any, still refuse the merge.
	return mergeHold{}, nil
}

// updateBehindBranch merges the base into a branch behind it with GitHub's
// update-branch, pinned to commit, and holds the merge: CI has not run on the
// new head. A conflict is sent back to be rebased.
func (s *AssistantApprovals) updateBehindBranch(ctx context.Context, ws, actor uuid.UUID, repository string, number int, commit string, status *PullRequestStatus) (mergeHold, error) {
	base := status.BaseRef
	behind := fmt.Sprintf("Pull request #%d is %d commit(s) behind %s, so it was not merged: its CI never ran with the latest %s", number, status.Behind, base, base)
	head, err := s.pulls.UpdatePullRequestBranch(ctx, ws, repository, number, commit)
	if err != nil {
		var unsynced *HeadSyncError
		var api *gh.APIError
		switch {
		case errors.As(err, &unsynced):
			// GitHub accepted the update; only reading the new head failed.
			audit.Log("pull_request.branch_updated_before_merge", actor, map[string]interface{}{"workspace_id": ws, "repository": repository, "pull_request": number, "behind": status.Behind})
			return mergeHold{reason: behind + ". Kuayle asked GitHub to bring the branch up to date. Next step: wait for the checks on the new head (get_pull_request_checks), then ask to merge again once they pass"}, nil
		case errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(api.Message), "conflict"):
			return mergeHold{reason: branchConflictRefusal(number, base)}, nil
		case errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(api.Message), "head"):
			return mergeHold{reason: "The pull request head moved after approval was requested; ask again to review the new commit"}, nil
		}
		if message, disabled := disabledAbilityMessage(err); disabled {
			return mergeHold{reason: behind + ", and Kuayle could not update the branch: " + message + ". Next step: update_pull_request_branch, or bring it up to date on GitHub, then ask to merge again once its checks pass"}, nil
		}
		if message, permanent := pullRequestApprovalFailure("update_pull_request_branch", err); permanent {
			return mergeHold{reason: behind + ", and Kuayle could not update the branch. " + message}, nil
		}
		return mergeHold{}, err
	}
	audit.Log("pull_request.branch_updated_before_merge", actor, map[string]interface{}{"workspace_id": ws, "repository": repository, "pull_request": number, "behind": status.Behind, "head": head})
	updated := fmt.Sprintf("Kuayle brought the branch up to date with GitHub's update-branch (new head %s)", shortSHA(head))
	if head == commit {
		updated = "Kuayle asked GitHub to bring the branch up to date (GitHub is still creating the new head)"
	}
	return mergeHold{reason: behind + ". " + updated + ". Next step: wait for the checks on the new head (get_pull_request_checks), then ask to merge again once they pass"}, nil
}

// branchConflictRefusal sends a branch that cannot be updated on GitHub back
// to the task agent.
func branchConflictRefusal(number int, base string) string {
	return fmt.Sprintf("Pull request #%d was not merged: the branch conflicts with %s; needs a rebase. Next step: update_pull_request_branch so the task agent merges %s and resolves the conflicts, then ask to merge again once its checks pass", number, base, base)
}

func headMovedRefusal(head string) string {
	return fmt.Sprintf("The pull request head moved to %s after approval was requested; ask again to review the new commit", shortSHA(head))
}

// baseMovedRefusal reports whether GitHub refused a merge because the head
// no longer contains its base: the base moved while merging, or branch
// protection requires an up-to-date branch. Checking again then decides.
func baseMovedRefusal(reason string) bool {
	lower := strings.ToLower(reason)
	return strings.Contains(lower, "base branch was modified") || strings.Contains(lower, "not up to date") || strings.Contains(lower, "out of date")
}
