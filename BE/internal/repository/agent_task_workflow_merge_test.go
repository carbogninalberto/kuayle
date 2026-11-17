package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// KUA-209: approving a ship approval records the merge on the step, as the
// person, with the step's audit; the runner's progress is fenced by its lease
// and the step's sequence; a failed merge tells the creator again; a merge
// step entered after the approval carries the merge.
func TestWorkflowMergesAreRecordedOnTheStepTheyCarryOut(t *testing.T) {
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
	claimTaskNotices(t, repo, ctx, claimed.ID)

	target, err := repo.WorkflowPullRequest(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, "fixture/repo", target.Repository)
	require.Equal(t, 7, target.PullRequestNumber)
	require.Equal(t, "0123456789abcdef0123456789abcdef01234567", target.CommitSHA)

	access, _ := domain.AccessFromContext(ctx)
	wait := &domain.MergeWait{Approved: target.CommitSHA, ActorID: &access.UserID, Since: now, Deadline: now.Add(2 * time.Hour)}
	_, err = repo.DecideWorkflowMerge(ctx, claimed.WorkspaceID, claimed.ID, WorkflowMerge{Sequence: claimed.WorkflowSequence - 1, Merge: wait, Reason: "Workflow · Ready to ship: approved", Wake: &now})
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a decision on a step the task left is refused")
	decided, err := repo.DecideWorkflowMerge(ctx, claimed.WorkspaceID, claimed.ID, WorkflowMerge{Sequence: claimed.WorkflowSequence, Merge: wait, Reason: "Workflow · Ready to ship: approved; Kuayle is merging pull request #7", Wake: &now,
		Notes: []domain.NotebookNote{{Event: "A person chose \"approve\" at Ready to ship; Kuayle merges the pull request"}}})
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", decided.State, "approving merges first; the task is not done yet")
	require.Equal(t, "Workflow · Ready to ship: approved; Kuayle is merging pull request #7", decided.Reason)
	require.NotNil(t, decided.WorkflowWakeAt)
	steps, err := repo.WorkflowSteps(ctx, claimed.WorkspaceID, claimed.ID)
	require.NoError(t, err)
	ship := steps[len(steps)-1]
	require.Equal(t, "ship", ship.NodeID)
	require.Nil(t, ship.FinishedAt, "the approval stays open while it merges")
	require.Equal(t, access.UserID, *ship.DecidedBy)
	require.Equal(t, target.CommitSHA, ship.MergeWait().Approved)
	var audits int
	require.NoError(t, db.Get(&audits, `SELECT count(*) FROM workflow_audit_events WHERE entity_id=$1 AND action='decision'`, claimed.ID))
	require.Equal(t, 1, audits)

	// The runner's progress: a failure asks a person again.
	wait.Head, wait.Failure = target.CommitSHA, "Pull request #7 was not merged: 1 check(s) failed"
	recorded, err := repo.RecordWorkflowMerge(ctx, *claimed, WorkflowMerge{Sequence: claimed.WorkflowSequence, Merge: wait, Reason: "Workflow · Ready to ship: approved, but pull request #7 was not merged", NeedsPerson: true})
	require.NoError(t, err)
	require.Nil(t, recorded.WorkflowWakeAt)
	require.Len(t, claimTaskNotices(t, repo, ctx, claimed.ID), 1, "the creator is told a person is needed again")
	steps, err = repo.WorkflowSteps(ctx, claimed.WorkspaceID, claimed.ID)
	require.NoError(t, err)
	require.Contains(t, steps[len(steps)-1].MergeWait().Failure, "1 check(s) failed")
	stale := *claimed
	stale.LeaseToken = ptrUUID(uuid.New())
	_, err = repo.RecordWorkflowMerge(ctx, stale, WorkflowMerge{Sequence: claimed.WorkflowSequence, Merge: wait, Reason: "x"})
	require.ErrorIs(t, err, ErrAgentTaskConflict, "only the worker holding the lease records progress")

	// A merge step entered after the approval carries the merge.
	shipNode, _ := graph.Node("ship")
	mergeNode := domain.WorkflowNode{ID: "merge", Type: domain.WorkflowNodeMerge, Label: "Merge"}
	carried := &domain.MergeWait{Approved: target.CommitSHA, ActorID: &access.UserID, Since: now, Deadline: now.Add(2 * time.Hour)}
	moved, err := repo.DecideWorkflow(ctx, claimed.WorkspaceID, claimed.ID, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: domain.WorkflowApprovalApproved,
		Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: shipNode, To: mergeNode, Reason: "Ready to ship → Merge: approved"}}},
		Entry: WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · Ready to ship → Merge: approved", WakeAt: &now, NeedsPerson: true, Merge: carried}})
	require.NoError(t, err)
	require.Equal(t, "merge", *moved.WorkflowNode)
	steps, err = repo.WorkflowSteps(ctx, claimed.WorkspaceID, claimed.ID)
	require.NoError(t, err)
	last := steps[len(steps)-1]
	require.Equal(t, "merge", last.NodeID)
	require.NotNil(t, last.MergeWait())
	require.Equal(t, access.UserID, *last.MergeWait().ActorID)
	raw, err := json.Marshal(last)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"merge":{`, "the step's merge is part of the task's workflow view")
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
