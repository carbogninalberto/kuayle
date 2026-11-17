package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The workflow side of the shared merge mechanism (KUA-209): the world's
// pull request on GitHub, the merge records on its steps and what merging
// does to the task.

func (s worldStore) WorkflowPullRequest(_ context.Context, task domain.AgentTask) (*domain.AgentReviewTarget, error) {
	if !s.w.published[task.RunID] {
		return nil, repository.ErrNoReviewTarget
	}
	return &domain.AgentReviewTarget{Repository: "acme/app", PullRequestNumber: 12, PullRequestURL: "https://github.com/acme/app/pull/12", BaseBranch: "main", CommitSHA: task.RunID.String()}, nil
}

func (s worldStore) RecordWorkflowMerge(_ context.Context, task domain.AgentTask, merge repository.WorkflowMerge) (*domain.AgentTask, error) {
	if task.Generation != s.w.task.Generation {
		return nil, repository.ErrAgentTaskConflict
	}
	return s.w.recordMerge(merge)
}

func (s worldStore) DecideWorkflowMerge(_ context.Context, _, _ uuid.UUID, merge repository.WorkflowMerge) (*domain.AgentTask, error) {
	actor := s.w.decider
	merge.Actor = &actor
	return s.w.recordMerge(merge)
}

// recordMerge applies repository.RecordWorkflowMerge's checks and writes.
func (w *workflowWorld) recordMerge(merge repository.WorkflowMerge) (*domain.AgentTask, error) {
	step := w.steps[merge.Sequence]
	if w.task.WorkflowSequence != merge.Sequence || w.task.State != "awaiting_review" || *w.task.WorkflowStatus != domain.WorkflowStatusWaiting || step == nil || step.FinishedAt != nil {
		return nil, repository.ErrAgentTaskConflict
	}
	data, err := json.Marshal(merge.Merge)
	if err != nil {
		return nil, err
	}
	raw := json.RawMessage(data)
	step.Merge = &raw
	if merge.Actor != nil {
		step.DecidedBy = merge.Actor
	}
	w.task.WorkflowWakeAt, w.task.Reason = merge.Wake, merge.Reason
	if merge.NeedsPerson {
		w.notices++
		if w.task.WorkflowHeldSince == nil {
			at := w.now
			w.task.WorkflowHeldSince = &at
		}
	}
	w.notes = append(w.notes, merge.Notes...)
	copy := w.task
	return &copy, nil
}

// workflowMerger merges on the fake GitHub; as GitHubService does, a merge
// completes the task awaiting review, which ends its workflow as done.
type workflowMerger struct {
	h      *workflowHarness
	merged []string
}

func (m *workflowMerger) MergeTaskPullRequest(_ context.Context, _ uuid.UUID, repository string, number int, sha, method string) error {
	m.merged = append(m.merged, fmt.Sprintf("%s#%d@%s/%s", repository, number, sha, method))
	m.h.pulls.status.State = "merged"
	if m.h.w.task.State == "awaiting_review" {
		done := domain.WorkflowStatusDone
		m.h.w.task.State, m.h.w.task.WorkflowStatus, m.h.w.task.WorkflowWakeAt = "completed", &done, nil
		m.h.w.task.Reason = fmt.Sprintf("Pull request #%d was merged on GitHub", number)
	}
	return nil
}

// withGitHub gives the harness a pull request on GitHub and the merge
// mechanism, as main.go wires them.
func (h *workflowHarness) withGitHub() *workflowMerger {
	h.pulls = &prGitHub{method: "squash", status: PullRequestStatus{State: "open", Draft: true, BaseRef: "main", MergeBase: "base1", MergeableState: "clean"}}
	merger := &workflowMerger{h: h}
	h.merger = merger
	h.w.decider = uuid.New()
	h.restart()
	return merger
}

// head is the published commit of the current attempt, which GitHub reports
// as the pull request's head until something moves it.
func (h *workflowHarness) head() string { return h.w.task.RunID.String() }

// toShip runs the example workflow to its ship approval with CI green.
func (h *workflowHarness) toShip() {
	h.t.Helper()
	h.agentFinishes("Added a theme toggle")
	h.pulls.status.HeadSHA, h.pulls.checksHead = h.head(), h.head()
	h.published()
	h.review("approve", "Looks good")
	h.ci("success", "")
	require.Equal(h.t, "ship", h.current().NodeID)
}

func (h *workflowHarness) decideAs(decision string) error {
	h.t.Helper()
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: h.w.task.WorkspaceID, UserID: h.w.decider})
	_, err := h.runner.Decide(ctx, h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence, decision)
	return err
}

// later advances the clock and runs the worker when the task is due.
func (h *workflowHarness) later(d time.Duration) bool {
	h.t.Helper()
	h.w.now = h.w.now.Add(d)
	return h.tick()
}

// KUA-209: entering the ship approval takes the draft out of draft so it can
// merge, and brings a branch that is behind its base up to date.
func TestShipApprovalMarksTheDraftReadyAndUpdatesABranchBehindItsBase(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.agentFinishes("Added a theme toggle")
	h.pulls.status.HeadSHA, h.pulls.checksHead = h.head(), h.head()
	h.published()
	h.review("approve", "Looks good")
	h.pulls.status.Behind = 2
	h.ci("success", "")
	require.Equal(t, "ship", h.current().NodeID)
	require.Equal(t, []string{"ready", "update:" + h.head()}, h.pulls.calls[len(h.pulls.calls)-2:], "marked ready, then brought up to date pinned to the head")
	require.Contains(t, h.w.task.Reason, "waiting for approval: Ready to ship")
	require.Contains(t, h.w.task.Reason, "marked the pull request ready for review")
	require.Contains(t, h.w.task.Reason, "brought the branch up to date with main")
}

// KUA-209: a branch that conflicts with its base when the ship approval is
// reached goes back to the step that changes code, as an iteration.
func TestShipApprovalSendsAConflictingBranchBackToTheImplementer(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withGitHub()
	h.agentFinishes("Added a theme toggle")
	h.pulls.status.HeadSHA, h.pulls.checksHead = h.head(), h.head()
	h.published()
	h.review("approve", "Looks good")
	no := false
	h.pulls.status.Behind, h.pulls.status.Mergeable, h.pulls.status.MergeableState = 3, &no, "dirty"
	iterations := h.w.task.WorkflowIterations
	h.ci("success", "")
	require.Equal(t, "implement", h.current().NodeID)
	require.Equal(t, "queued", h.w.task.State)
	require.Equal(t, iterations+1, h.w.task.WorkflowIterations, "the send-back counts as an iteration")
	ship := h.w.steps[h.w.task.WorkflowSequence-1]
	require.Equal(t, "ship", ship.NodeID)
	require.Equal(t, domain.WorkflowBranchConflict, *ship.Outcome)
	require.Contains(t, h.w.task.Prompt, "conflicts with main")
	require.Contains(t, h.w.task.Prompt, "merge origin/main into the task branch")
	require.Contains(t, h.current().Reason, "Ready to ship → Implementer")
	require.NotContains(t, h.pulls.calls, "update:"+h.head(), "GitHub cannot resolve conflicts")
}

// KUA-209: approving the ship approval of a workflow without a merge step
// merges the pull request; the task is done only after the merge.
func TestApprovingTheShipApprovalMergesBeforeTheTaskIsDone(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.toShip()
	h.github(passingChecks...)
	h.pulls.checks = passingChecks
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.Equal(t, "awaiting_review", h.w.task.State, "approving does not finish the task by itself")
	require.Equal(t, "ship", h.current().NodeID)
	require.Contains(t, h.w.task.Reason, "merging pull request #12")
	wait := h.current().MergeWait()
	require.NotNil(t, wait)
	require.Equal(t, h.head(), wait.Approved, "the merge is pinned to the published commit the person approved")
	require.Equal(t, h.w.decider, *wait.ActorID)
	require.Empty(t, merger.merged)

	require.ErrorIs(t, h.decideAs(WorkflowDecisionApprove), ErrWorkflowDecision, "a second click while it merges changes nothing")
	require.Equal(t, wait, h.current().MergeWait())
	require.True(t, h.tick())
	require.Equal(t, []string{"acme/app#12@" + h.head() + "/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
}

// KUA-209 with KUA-207: a branch that fell behind is updated at approval and
// merged once the updated head is green.
func TestApprovingTheShipApprovalOfABranchBehindMergesTheUpdateOnceGreen(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.toShip()
	h.pulls.status.Behind = 1
	verifiedUpdate(h.pulls, h.head(), "head2", "base2")
	update := h.pulls.onUpdate
	h.pulls.onUpdate = func() {
		update()
		h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	}
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "merges it once the checks on the new head pass")
	require.Equal(t, "head2", h.current().MergeWait().Head)
	require.Empty(t, merger.merged)

	require.False(t, h.later(30*time.Second), "polled once a minute, not busy")
	require.True(t, h.later(30*time.Second))
	require.Empty(t, merger.merged, "running checks keep it waiting")
	require.Equal(t, "ship", h.current().NodeID)

	h.pulls.checks = passingChecks
	require.True(t, h.later(time.Minute))
	require.Equal(t, []string{"acme/app#12@head2/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
}

// KUA-209: a merge that cannot happen keeps the task at its approval with the
// reason, and a person can approve again once it is fixed.
func TestAShipMergeThatFailsStaysAtTheApproval(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.toShip()
	h.pulls.checks = []PullRequestCheck{{Name: "Tests", State: "failed"}}
	notices := h.w.notices
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	require.Empty(t, merger.merged)
	require.Equal(t, "ship", h.current().NodeID, "the task stays at the approval")
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.NotEqual(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
	require.Contains(t, h.w.task.Reason, "was not merged")
	require.Contains(t, h.w.task.Reason, "1 check(s) failed")
	require.Contains(t, h.current().MergeWait().Failure, "1 check(s) failed")
	require.Greater(t, h.w.notices, notices, "the person is told it needs them again")

	h.pulls.checks = passingChecks
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	require.Len(t, merger.merged, 1)
	require.Equal(t, "completed", h.w.task.State)
}

// A head someone else pushed is not what the person approved.
func TestAShipMergeRefusesAHeadSomeoneElsePushed(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	merger := h.withGitHub()
	h.toShip()
	h.pulls.checks = passingChecks
	h.pulls.status.HeadSHA, h.pulls.checksHead = "pushed1", "pushed1"
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.True(t, h.tick())
	require.Empty(t, merger.merged)
	require.Equal(t, "ship", h.current().NodeID)
	require.Contains(t, h.w.task.Reason, "pushed1")
}

// KUA-209: in a graph with a merge step the approval only approves; the merge
// step merges, once.
func TestTheMergeStepMergesAfterTheShipApproval(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	var graph domain.WorkflowGraph
	require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
	graph.Nodes = append(graph.Nodes, domain.WorkflowNode{ID: "merge", Type: domain.WorkflowNodeMerge, Label: "Merge"})
	for i := range graph.Edges {
		if graph.Edges[i].ID == "ship-approved" {
			graph.Edges[i].Target = "merge"
		}
	}
	graph.Edges = append(graph.Edges, domain.WorkflowEdge{ID: "merge-done", Source: "merge", Target: "done", Condition: domain.WorkflowMerged})
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	h.w.version.Graph = raw
	merger := h.withGitHub()
	h.toShip()
	h.pulls.checks = passingChecks
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	require.Equal(t, "merge", h.current().NodeID, "the approval moves on to the merge step")
	require.Nil(t, h.w.steps[h.w.task.WorkflowSequence-1].MergeWait(), "the approval itself does not merge")
	require.NotNil(t, h.current().MergeWait())
	require.True(t, h.tick())
	require.Equal(t, []string{"acme/app#12@" + h.head() + "/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
	require.False(t, h.tick())
	require.Len(t, merger.merged, 1, "merged once")
}

// KUA-209: before a review the branch is brought up to date; a conflict goes
// back to the implementer as review feedback, within the reviewer's visits.
func TestTheReviewStepUpdatesTheBranchAndSendsConflictsBack(t *testing.T) {
	t.Run("behind is updated before the review", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		h.agentFinishes("Added a theme toggle")
		h.pulls.status.HeadSHA, h.pulls.status.Behind = h.head(), 4
		h.published()
		require.Equal(t, "review", h.current().NodeID)
		require.Equal(t, []string{"update:" + h.head()}, h.pulls.calls)
		require.NotNil(t, h.current().ReviewTaskID, "the review runs")
	})

	t.Run("a conflict goes back to the implementer, never endlessly", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		h.withGitHub()
		no := false
		for round := 1; round <= 3; round++ {
			h.agentFinishes("attempt")
			h.pulls.status.HeadSHA, h.pulls.status.Behind, h.pulls.status.Mergeable, h.pulls.status.MergeableState = h.head(), 2, &no, "dirty"
			h.published()
			require.Equal(t, "implement", h.current().NodeID, "round %d", round)
			require.Equal(t, "queued", h.w.task.State)
			require.Contains(t, h.w.task.Prompt, "conflicts with main")
			require.Contains(t, h.w.task.Prompt, "resolve the conflicts")
			review := h.w.steps[h.w.task.WorkflowSequence-1]
			require.Equal(t, "review", review.NodeID)
			require.Nil(t, review.ReviewTaskID, "no reviewer ran on a conflicting branch")
		}
		h.agentFinishes("attempt 4")
		h.published()
		require.Equal(t, "cancelling", h.w.task.State, "the reviewer's visit limit ends the loop")
		require.Contains(t, h.w.task.Reason, "Reviewer reached its limit of 3 visits")
		require.Empty(t, h.pulls.calls, "GitHub's update-branch is never asked to resolve conflicts")
	})
}
