package service

import (
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// KUA-265: a step that acts on what Kuayle read about its pull request's
// branch stores those facts and what it did with the step, so the step's
// detail says why the run moved or stayed without a person.

// branchOf is the branch decision the step recorded.
func branchOf(t *testing.T, step domain.WorkflowStep) *domain.WorkflowBranchRecord {
	t.Helper()
	record := step.CIRecord()
	require.NotNil(t, record, "step %s recorded nothing", step.NodeID)
	require.NotNil(t, record.Branch, "step %s recorded no branch decision", step.NodeID)
	return record.Branch
}

// left is the step the task just left.
func (h *workflowHarness) left() domain.WorkflowStep { return *h.w.steps[h.w.task.WorkflowSequence-1] }

func TestCIStepRecordsTheBranchFactsItActedOn(t *testing.T) {
	t.Run("an update", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		published := h.toReview()
		yes := true
		h.pulls.status.Behind, h.pulls.status.Mergeable = 2, &yes
		verifiedUpdate(h.pulls, published, "head2", "base2")
		h.github(PullRequestCheck{Name: "Tests", State: "pending"})
		h.review("approve", "Looks good")
		require.Equal(t, "ci", h.current().NodeID)
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchUpdated, branch.Action)
		require.Equal(t, published, branch.Head, "the facts as read, before the update")
		require.Equal(t, "main", branch.Base)
		require.Equal(t, 12, branch.PullRequest)
		require.Equal(t, 2, branch.Behind)
		require.Equal(t, "no", branch.Conflict)
		require.Equal(t, "no", branch.MigrationCollision)
		require.Equal(t, "head2", branch.UpdatedTo)
		require.Equal(t, h.w.now, branch.At)
	})

	t.Run("a conflict sent back", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		published := h.toReview()
		h.github(PullRequestCheck{Name: "Tests", State: "pending"})
		h.review("approve", "Looks good")
		baseConflicts(h.pulls)
		require.True(t, h.due())
		require.Equal(t, "implement", h.current().NodeID)
		branch := branchOf(t, h.left())
		require.Equal(t, domain.WorkflowBranchConflicted, branch.Action)
		require.Equal(t, published, branch.Head)
		require.Equal(t, 3, branch.Behind)
		require.Equal(t, "yes", branch.Conflict)
	})

	t.Run("a migration collision sent back", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toReview()
		h.pulls.status.Behind, h.pulls.status.migrationRepair = 1, "Migration number collision with main."
		h.github(PullRequestCheck{Name: "Tests", State: "pending"})
		h.review("approve", "Looks good")
		require.Equal(t, "implement", h.current().NodeID)
		branch := branchOf(t, h.left())
		require.Equal(t, domain.WorkflowBranchMigrationCollision, branch.Action)
		require.Equal(t, "yes", branch.MigrationCollision)
	})

	t.Run("an update an earlier step made", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.agentFinishes("Added a theme toggle")
		published := h.head()
		h.pulls.status.HeadSHA = published
		baseMoves(h.pulls, published, "head2", "base2")
		h.published()
		h.w.shaChecks[published] = passingChecks
		h.w.shaChecks["head2"] = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		h.review("approve", "Looks good")
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchJudgedUpdate, branch.Action)
		require.Equal(t, "head2", branch.Head)
		require.Equal(t, published, branch.Published)
		require.Equal(t, 1, branch.Updates)
		require.Equal(t, 0, branch.Behind)
	})

	t.Run("the update limit", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
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
		h.w.shaChecks["head4"] = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		baseMoves(h.pulls, "head4", "head5", "base5")
		require.True(t, h.later(ciPoll))
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchUpdateLimit, branch.Action)
		require.Equal(t, "head4", branch.Head)
		require.Equal(t, maxMergeUpdates, branch.Updates)
		require.Equal(t, 1, branch.Behind)
		at := branch.At
		require.True(t, h.later(ciPoll))
		require.Equal(t, at, branchOf(t, h.current()).At, "the same facts are not recorded again")
	})
}

func TestWaitingStepsRecordTheBranchFactsTheyActedOn(t *testing.T) {
	t.Run("a ship approval brings its branch up to date", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toShip()
		published := h.head()
		baseMoves(h.pulls, published, "head2", "base2")
		require.True(t, h.due())
		require.Equal(t, "ship", h.current().NodeID)
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchUpdated, branch.Action)
		require.Equal(t, published, branch.Head)
		require.Equal(t, 1, branch.Behind)
		require.Equal(t, "head2", branch.UpdatedTo)
		require.Nil(t, h.current().MergeWait(), "recording it starts no merge")
		require.NoError(t, h.decideAs(WorkflowDecisionApprove), "the person still decides")
	})

	t.Run("a ship approval sends a conflict back", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toShip()
		baseConflicts(h.pulls)
		require.True(t, h.due())
		branch := branchOf(t, h.left())
		require.Equal(t, "ship", h.left().NodeID)
		require.Equal(t, domain.WorkflowBranchConflicted, branch.Action)
		require.Equal(t, "yes", branch.Conflict)
	})

	t.Run("a review sends a conflict back", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toReview()
		baseConflicts(h.pulls)
		require.True(t, h.due())
		require.Equal(t, "review", h.left().NodeID)
		require.Equal(t, domain.WorkflowBranchConflicted, branchOf(t, h.left()).Action)
	})

	t.Run("a review brings its branch up to date", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		published := h.toReview()
		baseMoves(h.pulls, published, "head2", "base2")
		require.True(t, h.due())
		require.Equal(t, domain.WorkflowBranchUpdated, branchOf(t, h.current()).Action)
	})

	t.Run("a ship approval at the update limit", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
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
		h.w.shaChecks["head4"] = passingChecks
		require.True(t, h.later(ciPoll))
		require.Equal(t, "ship", h.current().NodeID)
		baseMoves(h.pulls, "head4", "head5", "base5")
		require.True(t, h.later(time.Minute))
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchUpdateLimit, branch.Action)
		require.Equal(t, "head4", branch.Head)
		require.Equal(t, maxMergeUpdates, branch.Updates)
		require.Equal(t, 1, branch.Behind)
	})
}

func TestAnApprovedMergeRecordsWhyItDidNotMerge(t *testing.T) {
	t.Run("failing checks", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		merger := h.withGitHub()
		h.toShip()
		h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "failed"}}
		h.pulls.checksHead = h.head()
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		require.Empty(t, merger.merged)
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchMergeRefused, branch.Action)
		require.Equal(t, h.head(), branch.Head)
		require.Equal(t, "no", branch.CI)
		require.Equal(t, 0, branch.Behind)
		require.Contains(t, branch.Refusal, "1 check(s) failed")
		require.Equal(t, h.current().MergeWait().Failure, branch.Refusal)
	})

	t.Run("a head someone else pushed", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toShip()
		h.pulls.checks = passingChecks
		h.pulls.status.HeadSHA, h.pulls.checksHead = "pushed1", "pushed1"
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		branch := branchOf(t, h.current())
		require.Equal(t, domain.WorkflowBranchMergeRefused, branch.Action)
		require.Equal(t, "pushed1", branch.Head)
		require.Contains(t, branch.Refusal, "pushed1")
	})

	t.Run("a conflict sent back", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.toShip()
		h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		require.NoError(t, h.decideAs(WorkflowDecisionApprove))
		require.True(t, h.tick())
		baseConflicts(h.pulls)
		require.True(t, h.later(mergeWaitPoll))
		require.Equal(t, "implement", h.current().NodeID)
		branch := branchOf(t, h.left())
		require.Equal(t, domain.WorkflowBranchConflicted, branch.Action)
		require.Equal(t, "yes", branch.Conflict)
	})
}
