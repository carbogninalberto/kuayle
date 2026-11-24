package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// KUA-265: a waiting step stores what it did on its pull request's branch,
// with the facts it read, in its ci, fenced by the worker lease; an approved
// merge's refusal stores them next to the merge; a step that closes keeps
// them; the task's workflow view returns them.
func TestWorkflowStepsStoreTheBranchFactsTheyActedOn(t *testing.T) {
	db, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	now := time.Now()
	for _, step := range []struct{ outcome, node string }{{domain.WorkflowPublished, "review"}, {domain.WorkflowReviewApproved, "ci"}, {domain.WorkflowChecksPassed, "ship"}} {
		decision := graph.Decide(claimed.Cursor(), step.outcome, now, nil)
		require.Equal(t, step.node, decision.To.ID)
		entry := WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · " + decision.Reason, WakeAt: &now, NeedsPerson: step.node == "ship"}
		if step.node == "review" {
			entry = WorkflowEntry{Kind: WorkflowEnterReview, Reason: "Workflow · " + decision.Reason, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}}
		}
		moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: step.outcome, Moves: []WorkflowMove{{Decision: decision}}, Entry: entry})
		require.NoError(t, err)
		claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits, claimed.WorkflowStatus, claimed.State = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits, moved.WorkflowStatus, moved.State
	}
	current := func() domain.WorkflowStep {
		t.Helper()
		step, err := repo.CurrentWorkflowStep(ctx, *claimed)
		require.NoError(t, err)
		return *step
	}

	// The ship approval waits and brings its branch up to date.
	updated := domain.WorkflowBranchRecord{Action: domain.WorkflowBranchUpdated, At: now, PullRequest: 7, Head: publishedHead, Base: "main", Behind: 2,
		Conflict: "no", MigrationCollision: "no", Updates: 1, UpdatedTo: "fedcba9876543210fedcba9876543210fedcba98"}
	wake := now.Add(time.Minute)
	stale := *claimed
	stale.LeaseToken = ptrUUID(uuid.New())
	require.ErrorIs(t, repo.RecordWorkflowBranch(ctx, stale, updated, &wake), ErrAgentTaskConflict, "only the worker holding the lease records")
	moved := *claimed
	moved.WorkflowSequence--
	require.ErrorIs(t, repo.RecordWorkflowBranch(ctx, moved, updated, &wake), ErrAgentTaskConflict, "nor on a step the task left")
	require.NoError(t, repo.RecordWorkflowBranch(ctx, *claimed, updated, &wake))
	ship := current()
	require.Equal(t, "ship", ship.NodeID)
	require.True(t, updated.Same(ship.CIRecord().Branch))
	require.WithinDuration(t, now, ship.CIRecord().Branch.At, time.Microsecond)
	require.Nil(t, ship.MergeWait(), "recording it starts no merge")
	var woken time.Time
	require.NoError(t, db.Get(&woken, `SELECT workflow_wake_at FROM agent_tasks WHERE id=$1`, claimed.ID))
	require.WithinDuration(t, wake, woken, time.Millisecond)

	// Approved, the merge is refused: the facts it read sit next to it.
	access, _ := domain.AccessFromContext(ctx)
	wait := &domain.MergeWait{Approved: publishedHead, Head: publishedHead, ActorID: &access.UserID, Since: now, Deadline: now.Add(2 * time.Hour)}
	_, err := repo.DecideWorkflowMerge(ctx, claimed.WorkspaceID, claimed.ID, WorkflowMerge{Sequence: claimed.WorkflowSequence, Merge: wait, Reason: "Workflow · Ready to ship: approved", Wake: &now})
	require.NoError(t, err)
	require.True(t, updated.Same(current().CIRecord().Branch), "approving keeps what the step did")
	wait.Failure = "Pull request #7 was not merged: 1 check(s) failed on 0123456: Tests"
	refused := domain.WorkflowBranchRecord{Action: domain.WorkflowBranchMergeRefused, At: now, PullRequest: 7, Head: publishedHead, Base: "main", Behind: 0,
		Conflict: "no", MigrationCollision: "no", CI: "no", Refusal: wait.Failure}
	_, err = repo.RecordWorkflowMerge(ctx, *claimed, WorkflowMerge{Sequence: claimed.WorkflowSequence, Merge: wait, Reason: "Workflow · Ready to ship: approved, but not merged", NeedsPerson: true, Branch: &refused})
	require.NoError(t, err)
	ship = current()
	require.True(t, refused.Same(ship.CIRecord().Branch))
	require.Equal(t, wait.Failure, ship.MergeWait().Failure)
	_, err = repo.RecordWorkflowMerge(ctx, *claimed, WorkflowMerge{Sequence: claimed.WorkflowSequence, Merge: wait, Reason: "Workflow · Ready to ship: approved; merging"})
	require.NoError(t, err)
	require.True(t, refused.Same(current().CIRecord().Branch), "a pass without a refusal keeps it")

	// The approval closes (denied here): the step keeps what it did last.
	denied := domain.WorkflowBranchRecord{Action: domain.WorkflowBranchConflicted, At: now, PullRequest: 7, Head: publishedHead, Base: "main", Behind: 3, Conflict: "yes", MigrationCollision: "no"}
	decision := graph.Decide(claimed.Cursor(), domain.WorkflowApprovalDenied, now, nil)
	_, err = repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: domain.WorkflowApprovalDenied, CI: &domain.WorkflowCIRecord{Branch: &denied},
		Moves: []WorkflowMove{{Decision: decision, Stop: true}}, Entry: WorkflowEntry{Kind: WorkflowEnterFail, Reason: "Workflow · denied"}})
	require.NoError(t, err)
	steps, err := repo.WorkflowSteps(ctx, claimed.WorkspaceID, claimed.ID)
	require.NoError(t, err)
	closed := steps[len(steps)-1]
	require.Equal(t, "ship", closed.NodeID)
	require.NotNil(t, closed.FinishedAt)
	require.True(t, denied.Same(closed.CIRecord().Branch))
	raw, err := json.Marshal(closed)
	require.NoError(t, err)
	var view struct {
		CI struct {
			Branch struct {
				Action string `json:"action"`
				Behind int    `json:"behind"`
			} `json:"branch"`
		} `json:"ci"`
	}
	require.NoError(t, json.Unmarshal(raw, &view))
	require.Equal(t, domain.WorkflowBranchConflicted, view.CI.Branch.Action, "the step's JSON carries it to the task's workflow view")
	require.Equal(t, 3, view.CI.Branch.Behind)
}
