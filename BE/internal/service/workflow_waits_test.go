package service

import (
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// KUA-264: a step that waits (a ship approval, a review, a merge) reads its
// pull request again whenever it wakes, as a push to its base wakes it
// (KUA-257), and does what entering the step does: a branch behind its base
// is brought up to date, one that conflicts goes back to the implementer.

// branchUpdates lists the update-branch requests among GitHub's calls.
func branchUpdates(calls []string) []string {
	var updates []string
	for _, call := range calls {
		if strings.HasPrefix(call, "update:") {
			updates = append(updates, call)
		}
	}
	return updates
}

// baseConflicts makes the base move into a conflict with the branch.
func baseConflicts(g *prGitHub) {
	no := false
	g.status.Behind, g.status.Mergeable, g.status.MergeableState = 3, &no, "dirty"
}

// baseMoves makes the base move ahead of the branch by commits that merge
// cleanly, and GitHub's update-branch of head make next.
func baseMoves(g *prGitHub, head, next, tip string) {
	yes := true
	g.status.Behind, g.status.Mergeable = 1, &yes
	verifiedUpdate(g, head, next, tip)
}

// sentBack checks that the step left with branch_conflict and the work went
// back to the implementer with Kuayle's instruction to merge the base.
func sentBack(t *testing.T, h *workflowHarness, from string, iterations int) {
	t.Helper()
	require.Equal(t, "implement", h.current().NodeID)
	require.Equal(t, "queued", h.w.task.State)
	require.Equal(t, iterations+1, h.w.task.WorkflowIterations, "the send-back counts as an iteration")
	left := h.w.steps[h.w.task.WorkflowSequence-1]
	require.Equal(t, from, left.NodeID)
	require.Equal(t, domain.WorkflowBranchConflict, *left.Outcome)
	require.Contains(t, h.w.task.Prompt, "conflicts with main")
	require.Contains(t, h.w.task.Prompt, "merge origin/main into the task branch")
}

// KUA-264: a run waiting at its ship approval while the base moves ends up
// updated, or sent back, without a person: the approval then merges the
// branch it updated, once green, with no update of its own.
func TestShipApprovalWaitFollowsItsBaseWithoutAPerson(t *testing.T) {
	t.Run("behind is brought up to date", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShip()
		published := h.head()
		baseMoves(h.pulls, published, "head2", "base2")
		require.True(t, h.due())
		require.Equal(t, "ship", h.current().NodeID, "the approval still waits for its person")
		require.Equal(t, "awaiting_review", h.w.task.State)
		require.Equal(t, []string{"update:" + published}, branchUpdates(h.pulls.calls), "updated, pinned to the published commit")

		h.pulls.checks = passingChecks
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		require.Equal(t, []string{"acme/app#12@head2/squash"}, merger.merged, "the merge takes Kuayle's update")
		require.Len(t, branchUpdates(h.pulls.calls), 1, "nothing was left to update")
		require.Equal(t, "completed", h.w.task.State)
	})

	t.Run("a conflict goes back to the implementer", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShip()
		iterations := h.w.task.WorkflowIterations
		baseConflicts(h.pulls)
		require.True(t, h.due())
		sentBack(t, h, "ship", iterations)
		require.Contains(t, h.current().Reason, "Ready to ship → Implementer")
		require.Empty(t, branchUpdates(h.pulls.calls), "GitHub's update-branch cannot resolve conflicts")
		require.Empty(t, merger.merged)
	})

	t.Run("a migration collision goes back with the repair", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toShip()
		iterations := h.w.task.WorkflowIterations
		const repair = "Migration number collision with main: BE/migrations/000178_a.up.sql and BE/migrations/000178_b.up.sql. Renumber the task migration pair and update its references."
		h.pulls.status.Behind, h.pulls.status.migrationRepair = 1, repair
		require.True(t, h.due())
		require.Equal(t, "implement", h.current().NodeID)
		require.Equal(t, iterations+1, h.w.task.WorkflowIterations)
		require.Contains(t, h.w.task.Prompt, repair)
		require.Empty(t, branchUpdates(h.pulls.calls))
	})

	t.Run("a fact GitHub has not reported changes nothing", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toShip()
		h.pulls.status.Behind, h.pulls.status.migrationCheckUnavailable = -1, true
		require.True(t, h.due())
		require.Equal(t, "ship", h.current().NodeID)
		require.Empty(t, branchUpdates(h.pulls.calls))
	})
}

// KUA-264: the wait acts on what GitHub reports, so waking it again, or
// after a restart, on a branch it already brought up to date changes
// nothing; the next move of the base is followed from the new head.
func TestShipApprovalWaitUpdatesOncePerMoveOfItsBase(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.toShip()
	published := h.head()
	baseMoves(h.pulls, published, "head2", "base2")
	require.True(t, h.due())
	require.True(t, h.due())
	h.restart()
	require.True(t, h.due())
	require.Equal(t, []string{"update:" + published}, branchUpdates(h.pulls.calls))

	baseMoves(h.pulls, "head2", "head3", "base3")
	require.True(t, h.due())
	require.Equal(t, []string{"update:" + published, "update:head2"}, branchUpdates(h.pulls.calls))
	require.Equal(t, "ship", h.current().NodeID)
}

// KUA-264: a head someone else pushed is not Kuayle's to update: the merge
// would refuse it anyway, and updating it would not be bounded.
func TestShipApprovalWaitLeavesAHeadSomeoneElsePushed(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.toShip()
	baseMoves(h.pulls, "pushed1", "head2", "base2")
	h.pulls.status.HeadSHA = "pushed1"
	require.True(t, h.due())
	require.Empty(t, branchUpdates(h.pulls.calls))
	require.Equal(t, "ship", h.current().NodeID)
}

// KUA-264: a review that is still running follows its base the same way:
// the branch is brought up to date under the reviewer, and a conflict ends
// the review and goes back to the implementer.
func TestReviewWaitFollowsItsBase(t *testing.T) {
	t.Run("behind is brought up to date", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		published := h.toReview()
		baseMoves(h.pulls, published, "head2", "base2")
		require.True(t, h.due())
		require.Equal(t, "review", h.current().NodeID, "the review goes on")
		require.Equal(t, []string{"update:" + published}, branchUpdates(h.pulls.calls))
	})

	t.Run("a conflict goes back to the implementer", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toReview()
		iterations := h.w.task.WorkflowIterations
		baseConflicts(h.pulls)
		require.True(t, h.due())
		sentBack(t, h, "review", iterations)
		require.Empty(t, branchUpdates(h.pulls.calls))
	})

	t.Run("a finished review decides as before", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toReview()
		h.github(PullRequestCheck{Name: "Tests", State: "pending"})
		h.review("approve", "Looks good")
		require.Equal(t, "ci", h.current().NodeID)
	})
}

// KUA-264: a merge step that waits for a person to merge (Kuayle does not
// merge here) follows its base too.
func TestMergeStepWaitFollowsItsBase(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.editGraph(func(graph *domain.WorkflowGraph) {
		graph.Nodes = append(graph.Nodes, domain.WorkflowNode{ID: "merge", Type: domain.WorkflowNodeMerge, Label: "Merge"})
		for i := range graph.Edges {
			if graph.Edges[i].ID == "ship-approved" {
				graph.Edges[i].Target = "merge"
			}
		}
		graph.Edges = append(graph.Edges, domain.WorkflowEdge{ID: "merge-done", Source: "merge", Target: "done", Condition: domain.WorkflowMerged})
	})
	h.withGitHub()
	h.merger = nil
	h.restart()
	h.toShip()
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.Equal(t, "merge", h.current().NodeID)
	require.Nil(t, h.current().MergeWait(), "a person merges")
	published := h.head()

	baseMoves(h.pulls, published, "head2", "base2")
	require.True(t, h.due())
	require.Equal(t, "merge", h.current().NodeID)
	require.Equal(t, []string{"update:" + published}, branchUpdates(h.pulls.calls))

	iterations := h.w.task.WorkflowIterations
	baseConflicts(h.pulls)
	require.True(t, h.due())
	sentBack(t, h, "merge", iterations)
}

// KUA-264: an approved merge whose base moved into a conflict goes back to
// the implementer at once, as entering the ship approval does, instead of
// waiting for its person to deny it.
func TestApprovedMergeSendsABranchThatNowConflictsBack(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.toShip()
	h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	require.Equal(t, "ship", h.current().NodeID, "the merge waits for the running checks")
	notices, iterations := h.w.notices, h.w.task.WorkflowIterations

	baseConflicts(h.pulls)
	require.True(t, h.later(mergeWaitPoll))
	sentBack(t, h, "ship", iterations)
	require.Equal(t, notices, h.w.notices, "nobody is asked to deny it")
	require.Empty(t, merger.merged)
}

// KUA-264: every update Kuayle makes to a published commit before a person
// approves its merge counts toward one bound, read from the commits
// themselves: when the CI step used all of them and the base moves once
// more, neither entering the ship approval nor a wake makes another. Once a
// person approves, the merge has updates of its own (KUA-277): it brings
// the branch up to date and merges it once green, with no person.
func TestUpdatesOfAPublishedCommitShareOneBound(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	published := h.toReview()
	heads := []string{published, "head2", "head3", "head4"}
	baseMoves(h.pulls, published, "head2", "base2")
	h.github(PullRequestCheck{Name: "Tests", State: "pending"})
	h.review("approve", "Looks good")
	for i := 2; i < len(heads); i++ {
		h.w.shaChecks[heads[i-1]] = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		baseMoves(h.pulls, heads[i-1], heads[i], "base"+heads[i])
		require.True(t, h.later(ciPoll))
	}
	require.Equal(t, "head4", h.ciRecord().Head)
	require.Equal(t, maxMergeUpdates, h.ciRecord().Updates)

	// The base moves once more, and CI passes on head4.
	baseMoves(h.pulls, "head4", "head5", "base5")
	h.w.shaChecks["head4"] = passingChecks
	require.True(t, h.later(ciPoll))
	require.Equal(t, "ship", h.current().NodeID)
	require.True(t, h.due())
	require.Equal(t, []string{"update:" + published, "update:head2", "update:head3"}, branchUpdates(h.pulls.calls), "no fourth update")

	h.pulls.checks = passingChecks
	notices := h.w.notices
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	require.Empty(t, merger.merged, "it waits for CI on its update")
	require.Equal(t, []string{"update:" + published, "update:head2", "update:head3", "update:head4"}, branchUpdates(h.pulls.calls))
	wait := h.current().MergeWait()
	require.Empty(t, wait.Failure)
	require.Equal(t, "head5", wait.Head)
	require.Equal(t, 1, ownUpdates(wait), "the merge's own first update")
	require.True(t, h.later(mergeWaitPoll))
	require.Equal(t, []string{"acme/app#12@head5/squash"}, merger.merged)
	require.Equal(t, notices, h.w.notices, "no person was needed")
}

// KUA-264: CI that failed on Kuayle's update of the published commit carries
// over when the work comes back without a new commit: the next CI step
// judges that update again (its failed jobs run again once, then a person
// decides), never the published commit's older green CI.
func TestCIFailureOnKuaylesUpdateCarriesOverToTheSameCommit(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 8})
	h.withGitHub()
	checks := h.withRerun()
	published := h.toReview()
	baseMoves(h.pulls, published, "head2", "base2")
	h.github(PullRequestCheck{Name: "Tests", State: "pending"})
	h.review("approve", "Looks good")
	require.Equal(t, "head2", h.ciRecord().Head)

	// CI fails on the update; the published commit's own CI passed.
	h.w.shaChecks[published] = passingChecks
	h.w.shaChecks["head2"] = []PullRequestCheck{flakyJob(1, "failed")}
	require.True(t, h.later(ciPoll))
	require.Equal(t, "implement", h.current().NodeID, "the failure on the update goes back to the agent")

	// The agent pushes nothing: the same commit is published again.
	h.w.heads[h.w.task.RunID] = published
	h.agentFinishes("CI failed on a flaky test; nothing to change")
	h.published()
	h.review("approve", "Still good")
	require.Equal(t, "ci", h.current().NodeID, "the published commit's old green CI does not pass the step")
	require.Equal(t, "head2", h.ciRecord().Head, "the step judges the update CI failed on")
	require.Equal(t, published, h.ciRecord().UpdateOf)
	require.Equal(t, [][]int64{{44137520077}}, checks.reruns, "its failed jobs run again once")

	h.w.shaChecks["head2"] = []PullRequestCheck{flakyJob(2, "failed")}
	require.True(t, h.later(ciPoll))
	require.Equal(t, "blocked", h.w.task.State, "still failing: a person decides")
	require.Contains(t, h.w.task.Reason, ciNoChanges+shortSHA("head2"))
}

// KUA-264: a branch some step already brought up to date is what CI is
// judged on: a CI step never passes on the published commit's CI when the
// pull request's head is Kuayle's update of it.
func TestCIStepJudgesTheUpdateAnEarlierStepMade(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.agentFinishes("Added a theme toggle")
	published := h.head()
	h.pulls.status.HeadSHA = published
	baseMoves(h.pulls, published, "head2", "base2")
	h.published()
	require.Equal(t, []string{"update:" + published}, branchUpdates(h.pulls.calls), "updated before the review")
	h.w.shaChecks[published] = passingChecks
	h.w.shaChecks["head2"] = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	h.review("approve", "Looks good")
	require.Equal(t, "ci", h.current().NodeID, "CI on the stale commit does not pass the step")
	require.Equal(t, "head2", h.ciRecord().Head)
	require.Equal(t, 1, h.ciRecord().Updates)

	h.w.shaChecks["head2"] = passingChecks
	require.True(t, h.later(ciPoll))
	require.Equal(t, "ship", h.current().NodeID)
}

// Phase 1 checkpoint (KUA-252), as service scenarios with the fake GitHub.

// (a) Two approved tasks on one repository: the second falls behind its base
// when the first merges, and both merge without a person, whether the second
// was already approved or still waited for its approval.
func TestCheckpointTwoApprovedTasksBothMergeWithoutAPerson(t *testing.T) {
	for _, approved := range []bool{true, false} {
		name := "the second still waits for its approval"
		if approved {
			name = "both approved"
		}
		t.Run(name, func(t *testing.T) {
			first, second := newWorkflowHarness(t, domain.WorkflowBudget{}), newWorkflowHarness(t, domain.WorkflowBudget{})
			firstMerger, secondMerger := first.withGitHub(), second.withGitHub()
			first.toShip()
			second.toShip()
			first.pulls.checks, second.pulls.checks = passingChecks, passingChecks
			secondHead := second.head()
			// Merging the first moves main: the second is one commit behind it,
			// and GitHub's update-branch merges it in cleanly.
			firstMerger.onMerge = func() { baseMoves(second.pulls, secondHead, "second2", "first-merged") }

			require.NoError(t, first.decideAs(WorkflowDecisionApprove))
			if approved {
				require.NoError(t, second.decideAs(WorkflowDecisionApprove))
			}
			require.True(t, first.tick())
			require.Equal(t, "completed", first.w.task.State)
			// The push to main wakes the second (KUA-257).
			require.True(t, second.due())
			require.Equal(t, []string{"update:" + secondHead}, branchUpdates(second.pulls.calls), "brought up to date once")
			if !approved {
				require.Equal(t, "ship", second.current().NodeID)
				require.NoError(t, second.decideAs(WorkflowDecisionApprove))
			}
			for range 3 {
				if second.w.task.State == "completed" {
					break
				}
				second.later(mergeWaitPoll)
			}
			require.Len(t, firstMerger.merged, 1)
			require.Equal(t, []string{"acme/app#12@second2/squash"}, secondMerger.merged, "the second merges Kuayle's update")
			require.Equal(t, "completed", second.w.task.State)
			require.Len(t, branchUpdates(second.pulls.calls), 1)
		})
	}
}

// (b) A run waiting at CI whose base moved into a conflict goes back to the
// implementer as soon as it wakes, without waiting for CI or its timeout.
func TestCheckpointACIWaitWhoseBaseConflictsGoesBackAtOnce(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.toReview()
	h.github(PullRequestCheck{Name: "Tests", State: "pending"})
	h.review("approve", "Looks good")
	require.Equal(t, "ci", h.current().NodeID)
	iterations := h.w.task.WorkflowIterations
	baseConflicts(h.pulls)
	require.True(t, h.due(), "the push to the base wakes it (KUA-257)")
	sentBack(t, h, "ci", iterations)
	require.Contains(t, h.current().Reason, "CI → Implementer")
}

// (c) A waiting run is woken by a push to its base: KUA-257 sets its wake-up
// (WakeBaseBranchWaits, tested with the webhooks in #353); here nothing else
// would wake the approval, and the wake alone is what makes it act.
func TestCheckpointAWakeIsWhatMakesAWaitingRunReadItsBase(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.toShip()
	require.True(t, h.tick(), "evaluated once as it is entered")
	require.Nil(t, h.w.task.WorkflowWakeAt, "an approval without a timeout waits for an event")
	published := h.head()
	baseMoves(h.pulls, published, "head2", "base2")
	require.False(t, h.later(mergeWaitTimeout), "nothing polls it")
	require.Empty(t, branchUpdates(h.pulls.calls))
	require.True(t, h.due(), "the push to main wakes it")
	require.Equal(t, []string{"update:" + published}, branchUpdates(h.pulls.calls))
}
