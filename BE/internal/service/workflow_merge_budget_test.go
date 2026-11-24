package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// KUA-277 (option B): once a person approved the merge, it has its own
// maxMergeUpdates updates, counted from the head that was approved. Kuayle's
// own verified updates made before the approval never strand it; the merge
// stays bounded, and a head someone else pushed is still refused.

var runningChecks = []PullRequestCheck{{Name: "Tests", State: "pending"}}

// toShipOnABusyBase runs the example workflow to its ship approval while the
// base moves on every pass of the CI step, which brings the branch up to date
// each time: published → head2 → head3 → head4, every update the published
// commit has before a person approves. CI then passes on head4. It returns
// the published commit.
func (h *workflowHarness) toShipOnABusyBase() string {
	h.t.Helper()
	published := h.toReview()
	heads := []string{published, "head2", "head3", "head4"}
	baseMoves(h.pulls, published, "head2", "base2")
	h.github(runningChecks...)
	h.review("approve", "Looks good")
	for i := 2; i < len(heads); i++ {
		h.w.shaChecks[heads[i-1]] = runningChecks
		baseMoves(h.pulls, heads[i-1], heads[i], "base"+heads[i])
		require.True(h.t, h.later(ciPoll))
	}
	require.Equal(h.t, "head4", h.ciRecord().Head)
	require.Equal(h.t, maxMergeUpdates, h.ciRecord().Updates, "the CI step spent every update before approval")
	h.w.shaChecks["head4"] = passingChecks
	require.True(h.t, h.later(ciPoll))
	require.Equal(h.t, "ship", h.current().NodeID)
	return published
}

// approveWhileChecksRun approves the ship approval while CI runs on the
// head, so the merge pins it and waits.
func (h *workflowHarness) approveWhileChecksRun() *domain.MergeWait {
	h.t.Helper()
	h.pulls.checks = runningChecks
	require.NoError(h.t, h.decideAs(WorkflowDecisionApprove))
	require.True(h.t, h.tick())
	wait := h.current().MergeWait()
	require.NotNil(h.t, wait)
	require.Empty(h.t, wait.Failure)
	return wait
}

func TestAnApprovedMergeHasItsOwnUpdatesAfterABusyBase(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	published := h.toShipOnABusyBase()
	wait := h.approveWhileChecksRun()
	require.Equal(t, "head4", wait.Head, "the head the person approved")
	require.Equal(t, maxMergeUpdates, wait.PinnedUpdates, "Kuayle's updates before the approval")
	require.Equal(t, 0, ownUpdates(wait), "none of its own yet")
	notices := h.w.notices

	// The base moves again while the merge waits for the checks.
	baseMoves(h.pulls, "head4", "head5", "base5")
	require.True(t, h.later(mergeWaitPoll))
	require.Equal(t, []string{"update:" + published, "update:head2", "update:head3", "update:head4"}, branchUpdates(h.pulls.calls))
	wait = h.current().MergeWait()
	require.Equal(t, "head5", wait.Head)
	require.Equal(t, 1, ownUpdates(wait))
	require.Empty(t, merger.merged, "it waits for CI on the update")

	h.pulls.checks = passingChecks
	require.True(t, h.later(mergeWaitPoll))
	require.Equal(t, []string{"acme/app#12@head5/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, notices, h.w.notices, "no person was needed")
}

func TestAnApprovedMergeMakesAtMostItsOwnUpdates(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.toShipOnABusyBase()
	h.approveWhileChecksRun()
	heads := []string{"head4", "head5", "head6", "head7"}
	for i := 1; i < len(heads); i++ {
		baseMoves(h.pulls, heads[i-1], heads[i], "base"+heads[i])
		require.True(t, h.later(mergeWaitPoll))
		wait := h.current().MergeWait()
		require.Empty(t, wait.Failure)
		require.Equal(t, heads[i], wait.Head)
		require.Equal(t, i, ownUpdates(wait))
	}

	// A fourth move after the approval: the merge does not follow it.
	baseMoves(h.pulls, "head7", "head8", "base8")
	require.True(t, h.later(mergeWaitPoll))
	require.Empty(t, merger.merged)
	require.Len(t, branchUpdates(h.pulls.calls), 2*maxMergeUpdates, "no fourth update of the merge's own")
	require.Equal(t, "ship", h.current().NodeID)
	failure := h.current().MergeWait().Failure
	require.Contains(t, failure, "fell behind main again after Kuayle brought it up to date 3 times")
	require.NotContains(t, failure, "not only the branch update Kuayle made")

	// Approving again starts from head7, six verified updates on top of the
	// published commit: a fresh budget, not a head Kuayle cannot verify.
	h.pulls.checks = runningChecks
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	wait := h.current().MergeWait()
	require.Empty(t, wait.Failure)
	require.Equal(t, "head8", wait.Head)
	require.Equal(t, 2*maxMergeUpdates, wait.PinnedUpdates)
	require.Equal(t, 1, ownUpdates(wait))
	h.pulls.checks = passingChecks
	require.True(t, h.later(mergeWaitPoll))
	require.Equal(t, []string{"acme/app#12@head8/squash"}, merger.merged)
}

func TestAnApprovedMergeStillRefusesAHeadSomeoneElsePushed(t *testing.T) {
	// pushedOnTop makes someone push a commit of their own on top of head.
	pushedOnTop := func(h *workflowHarness, head, pushed string) {
		h.pulls.parents[pushed] = []string{head}
		h.pulls.status.HeadSHA, h.pulls.checksHead, h.pulls.status.Behind = pushed, pushed, 0
	}

	t.Run("before the approval, on top of Kuayle's updates", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShipOnABusyBase()
		pushedOnTop(h, "head4", "pushed5")
		h.pulls.checks = passingChecks
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		require.Empty(t, merger.merged)
		require.Contains(t, h.current().MergeWait().Failure, "not only the branch update Kuayle made")
	})

	t.Run("before the approval, under a merge of the base", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShipOnABusyBase()
		pushedOnTop(h, "head4", "pushed5")
		// Someone brings the pushed commit up to date on GitHub: the head is
		// a verified merge of the base, on top of a commit nobody approved.
		baseMoves(h.pulls, "pushed5", "head6", "base6")
		h.pulls.onUpdate()
		h.pulls.checks = passingChecks
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		require.Empty(t, merger.merged)
		require.Contains(t, h.current().MergeWait().Failure, "not only the branch update Kuayle made")
	})

	t.Run("a merge of the base that changes more", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShipOnABusyBase()
		baseMoves(h.pulls, "head4", "head5", "base5")
		h.pulls.onUpdate()
		h.pulls.changes["base5...head5"] = append(h.pulls.changes["base5...head5"], gh.CompareFile{Filename: "extra.go", Status: "added", SHA: "blob-x", Patch: "@@ -0,0 +1 @@\n+package app\n"})
		h.pulls.checks = passingChecks
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		require.Empty(t, merger.merged)
		require.Contains(t, h.current().MergeWait().Failure, "changes extra.go differently from the approved commit")
	})

	t.Run("after the approval", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShipOnABusyBase()
		h.approveWhileChecksRun()
		pushedOnTop(h, "head4", "pushed5")
		h.pulls.checks = passingChecks
		require.True(t, h.later(mergeWaitPoll))
		require.Empty(t, merger.merged)
		require.Contains(t, h.current().MergeWait().Failure, "head moved to pushed5")
	})
}

// Pinning walks Kuayle's updates made before the approval however many there
// are, up to maxUpdateChain, which bounds what one pass reads from GitHub.
func TestPinningTakesKuaylesUpdatesBeforeTheApprovalUpToTheChainBound(t *testing.T) {
	github := &prGitHub{method: "squash", status: PullRequestStatus{State: "open", BaseRef: "main", MergeableState: "clean"}}
	gate := mergeGate{pulls: github, now: time.Now}
	pin := func(head string) (*domain.MergeWait, mergeHold) {
		github.status.HeadSHA = head
		wait := newApprovedMerge("published", "", time.Now())
		hold, err := gate.pin(context.Background(), uuid.New(), "acme/app", 12, wait)
		require.NoError(t, err)
		return wait, hold
	}
	head := "published"
	for i := 1; i <= maxUpdateChain; i++ {
		next := fmt.Sprintf("update%d", i)
		verifiedUpdate(github, head, next, fmt.Sprintf("base%d", i))
		head = next
	}
	wait, hold := pin(head)
	require.Empty(t, hold.reason)
	require.Equal(t, head, wait.Head)
	require.Equal(t, maxUpdateChain, wait.PinnedUpdates)
	require.Equal(t, 0, ownUpdates(wait))
	require.Equal(t, "squash", wait.Method)

	verifiedUpdate(github, head, "beyond", "base-beyond")
	wait, hold = pin("beyond")
	require.Contains(t, hold.reason, fmt.Sprintf("more than %d merges of main", maxUpdateChain))
	require.Empty(t, wait.Head, "not pinned")
}
