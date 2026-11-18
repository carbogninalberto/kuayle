package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// acceptAs presses Accept on the task card as the person deciding.
func (h *workflowHarness) acceptAs(generation int64) (*domain.AgentTask, bool, error) {
	h.t.Helper()
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: h.w.task.WorkspaceID, UserID: h.w.decider})
	return h.runner.AcceptAtShip(ctx, h.w.task.WorkspaceID, h.w.task.ID, generation)
}

// KUA-216: KUA-171 waited at "Ready to ship" with CI green, and Accept
// completed it without merging. Accepting a task at its ship approval is the
// go-ahead to ship: it approves, Kuayle merges with every gate, and the task
// is done only after the merge.
func TestAcceptAtTheShipApprovalMergesLikeApprove(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.w.task.CreatorID = h.w.decider
	h.toShip()
	h.pulls.checks = passingChecks
	ship, err := h.runner.ShipMergeWaiting(context.Background(), h.w.task)
	require.NoError(t, err)
	require.True(t, ship, "the card says accepting merges")

	_, handled, err := h.acceptAs(h.w.task.Generation - 1)
	require.True(t, handled)
	require.ErrorIs(t, err, repository.ErrAgentTaskConflict, "an older attempt is not accepted")

	task, handled, err := h.acceptAs(h.w.task.Generation)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "awaiting_review", task.State, "accepting does not finish the task by itself")
	require.NotNil(t, h.current().MergeWait(), "the merge is recorded on the approval, as approving does")
	require.Empty(t, merger.merged)

	// Pressing Accept again while it merges changes nothing.
	again, handled, err := h.acceptAs(h.w.task.Generation)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "awaiting_review", again.State)

	require.True(t, h.tick())
	require.Equal(t, []string{"acme/app#12@" + h.head() + "/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
}

// Accept anywhere else keeps its meaning: complete without merging, which the
// caller does when AcceptAtShip does not handle it.
func TestAcceptElsewhereIsNotAnApproval(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.w.task.CreatorID = h.w.decider
	h.agentFinishes("Added a theme toggle")
	h.pulls.status.HeadSHA, h.pulls.checksHead = h.head(), h.head()
	h.published()
	h.review("approve", "Looks good")
	require.Equal(t, "ci", h.current().NodeID)
	_, handled, err := h.acceptAs(h.w.task.Generation)
	require.NoError(t, err)
	require.False(t, handled, "waiting for CI is no ship approval")

	h.ci("success", "")
	require.Equal(t, "ship", h.current().NodeID)
	// Accept on the task card is the creator's; an admin approves instead.
	h.w.task.CreatorID = uuid.New()
	_, handled, err = h.acceptAs(h.w.task.Generation)
	require.NoError(t, err)
	require.False(t, handled)

	// Without the merge mechanism, approving would not merge either.
	h = newWorkflowHarness(t, domain.WorkflowBudget{})
	h.w.decider = uuid.New()
	h.w.task.CreatorID = h.w.decider
	h.agentFinishes("Added a theme toggle")
	h.published()
	h.review("approve", "Looks good")
	h.ci("success", "")
	require.Equal(t, "ship", h.current().NodeID)
	_, handled, err = h.acceptAs(h.w.task.Generation)
	require.NoError(t, err)
	require.False(t, handled)
}

// headOnGitHub answers what was added to the pull request since a commit, as
// GitHubService.InspectPullRequestHead: "pushed" when commits were added on
// top of it, "rewritten" when it is no longer in the head's history.
type headOnGitHub struct {
	*prGitHub
	kind string
}

func (g *headOnGitHub) InspectPullRequestHead(_ context.Context, _ uuid.UUID, _ string, _ int, published string) (*domain.PullRequestHead, error) {
	read := &domain.PullRequestHead{SHA: g.status.HeadSHA}
	if read.SHA != published {
		read.Change = &domain.PullRequestHeadChange{Kind: g.kind, TreeChanged: true}
	}
	return read, nil
}

// toAfterAgentsShip runs the orchestrated workflow to its ship approval,
// which leads through a last agent step to done.
func toAfterAgentsShip(t *testing.T) (*workflowHarness, *workflowMerger, *headOnGitHub) {
	h := orchestratedHarness(t, domain.WorkflowBudget{MaxIterations: 8})
	merger := h.withGitHub()
	github := &headOnGitHub{prGitHub: h.pulls, kind: "pushed"}
	h.runner.SetPullRequests(github, merger)
	h.w.task.CreatorID = h.w.decider
	h.plans("1. Add the toggle")
	h.agentFinishes("Added a theme toggle")
	h.pulls.status.HeadSHA, h.pulls.checksHead = h.head(), h.head()
	h.published()
	h.review("approve", "Looks good")
	h.ci("success", "")
	require.Equal(t, "ship", h.current().NodeID)
	return h, merger, github
}

// toDone approves the ship approval and lets the last agent step finish: its
// attempt publishes nothing of its own.
func (h *workflowHarness) toDone(approve func() error) string {
	h.t.Helper()
	approved := h.head()
	require.NoError(h.t, approve())
	require.Equal(h.t, "agent-2", h.current().NodeID, "the agent steps after the approval run first")
	require.Equal(h.t, "queued", h.w.task.State)
	require.Contains(h.t, h.w.task.Reason, "Kuayle merges pull request #12 once the workflow reaches its end")
	deferred := h.w.steps[h.w.task.WorkflowSequence-1].MergeWait()
	require.NotNil(h.t, deferred, "the approval records the merge it starts")
	require.True(h.t, deferred.Deferred)
	require.Equal(h.t, approved, deferred.Approved)
	require.Equal(h.t, "acme/app", deferred.Repository)
	require.Equal(h.t, 12, deferred.PullRequest)
	h.agentFinishes("Updated the changelog")
	require.Equal(h.t, "done", h.current().NodeID)
	require.Equal(h.t, "awaiting_review", h.w.task.State, "done waits for the merge")
	require.Equal(h.t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)
	require.Nil(h.t, h.current().FinishedAt)
	require.Contains(h.t, h.w.task.Reason, "Kuayle merges pull request #12, as approved, once every check passes")
	return approved
}

// KUA-216: a ship approval whose approved path runs through agent steps to
// done is the go-ahead to ship too. Entering it marks the draft ready;
// approving (or accepting) lets the agent steps run, and the task is done
// once the done end merged the approved commit.
func TestAShipApprovalBeforeAgentStepsMergesWhenTheWorkflowEnds(t *testing.T) {
	h, merger, _ := toAfterAgentsShip(t)
	require.Contains(t, h.pulls.calls, "ready", "entering the ship approval marks the draft ready")
	ship, err := h.runner.ShipMergeWaiting(context.Background(), h.w.task)
	require.NoError(t, err)
	require.True(t, ship)
	iterations := h.w.task.WorkflowIterations

	approved := h.toDone(func() error {
		_, handled, err := h.acceptAs(h.w.task.Generation)
		require.True(t, handled, "Accept at the ship approval approves")
		return err
	})
	require.Equal(t, iterations+1, h.w.task.WorkflowIterations, "the last agent step is a round of its own")
	require.Empty(t, merger.merged, "nothing merges before the end")
	h.pulls.checks = passingChecks
	require.True(t, h.tick())
	require.Equal(t, []string{"acme/app#12@" + approved + "/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
	done := h.current()
	require.Equal(t, "done", done.NodeID)
	require.NotNil(t, done.FinishedAt)
	require.Equal(t, domain.WorkflowEndDone, *done.Outcome)
	require.Contains(t, done.Detail, "Merged pull request #12")
	require.False(t, h.tick())
	require.Len(t, merger.merged, 1, "merged once")
}

// Commits the agent steps added after the approval are merged only once
// their checks pass.
func TestCommitsAddedAfterTheShipApprovalMergeOnceTheirChecksPass(t *testing.T) {
	h, merger, _ := toAfterAgentsShip(t)
	h.toDone(func() error { return h.decideAs(WorkflowDecisionApprove) })
	h.pulls.status.HeadSHA, h.pulls.checksHead = "agent2head", "agent2head"
	h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	require.True(t, h.tick())
	require.Empty(t, merger.merged)
	require.Equal(t, "agent2head", h.current().MergeWait().Head, "the new head is the one merged")
	require.Contains(t, h.w.task.Reason, "still running")
	h.pulls.checks = passingChecks
	require.True(t, h.later(mergeWaitPoll))
	require.Equal(t, []string{"acme/app#12@agent2head/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
}

// A pull request a person merged on GitHub while the end waited shipped: the
// task completes on the end, since nothing else would complete it.
func TestAPullRequestMergedByHandWhileTheEndWaitsCompletesTheTask(t *testing.T) {
	h, merger, _ := toAfterAgentsShip(t)
	h.toDone(func() error { return h.decideAs(WorkflowDecisionApprove) })
	h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	require.True(t, h.tick())
	require.Equal(t, "awaiting_review", h.w.task.State)
	h.pulls.status.State, h.pulls.status.HeadSHA = "merged", "squashed-by-hand"
	require.True(t, h.later(mergeWaitPoll))
	require.Empty(t, merger.merged, "Kuayle did not merge it")
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
	require.Contains(t, h.current().Detail, "was merged outside Kuayle")
}

// A head that no longer contains the approved commit, or whose checks fail,
// is refused; nobody decides at an end, so the task fails with the reason.
func TestAMergeAtTheEndThatCannotHappenFailsTheTaskWithTheReason(t *testing.T) {
	t.Run("rewritten", func(t *testing.T) {
		h, merger, github := toAfterAgentsShip(t)
		github.kind = "rewritten"
		approved := h.toDone(func() error { return h.decideAs(WorkflowDecisionApprove) })
		h.pulls.status.HeadSHA, h.pulls.checksHead = "forced", "forced"
		h.pulls.checks = passingChecks
		require.True(t, h.tick())
		require.Empty(t, merger.merged)
		require.Equal(t, "cancelling", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)
		require.Contains(t, h.w.task.Reason, "Done: the approved merge was refused")
		require.Contains(t, h.w.task.Reason, "no longer contains the approved commit "+shortSHA(approved))
	})
	t.Run("failing checks", func(t *testing.T) {
		h, merger, _ := toAfterAgentsShip(t)
		h.toDone(func() error { return h.decideAs(WorkflowDecisionApprove) })
		h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "failed"}}
		require.True(t, h.tick())
		require.Empty(t, merger.merged)
		require.Equal(t, "cancelling", h.w.task.State)
		require.Contains(t, h.w.task.Reason, "1 check(s) failed")
	})
}
