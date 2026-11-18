package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

const publishedHead = "0123456789abcdef0123456789abcdef01234567"

// KUA-215: a CI step reads only the suites of the commit its attempt
// published, records what it judged on the step, and a details read of the
// commit's failed check runs reaches the CI step waiting for it.
func TestWorkflowCIRecordsWhatItJudgedOnThePublishedHead(t *testing.T) {
	db, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	now := time.Now()
	for _, step := range []struct{ outcome, node string }{{domain.WorkflowPublished, "review"}, {domain.WorkflowReviewApproved, "ci"}} {
		decision := graph.Decide(claimed.Cursor(), step.outcome, now, nil)
		require.Equal(t, step.node, decision.To.ID)
		entry := WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · " + decision.Reason, WakeAt: &now}
		if step.node == "review" {
			entry = WorkflowEntry{Kind: WorkflowEnterReview, Reason: "Workflow · " + decision.Reason, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}}
		}
		moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: step.outcome, Moves: []WorkflowMove{{Decision: decision}}, Entry: entry})
		require.NoError(t, err)
		claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits, claimed.WorkflowStatus, claimed.State = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits, moved.WorkflowStatus, moved.State
	}
	ws := claimed.WorkspaceID

	// A suite of the published commit counts; one an older commit of the same
	// attempt reported does not.
	_, err := repo.RecordSignal(ctx, ws, "fixture/repo", 7, domain.AgentTaskSignalInput{Kind: "ci", ExternalID: "suite-now", Outcome: "failure", Name: "Tests", HeadSHA: publishedHead})
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,name,summary,url,action,request_id,head_sha) VALUES($1,$2,'ci','suite-old','failure','Lint','','','recorded',$3,'fedcba9876543210fedcba9876543210fedcba98')`, claimed.ID, claimed.RunID, uuid.New())
	require.NoError(t, err)
	ci, err := repo.WorkflowCI(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, publishedHead, ci.CommitSHA)
	require.Len(t, ci.Failed, 1)
	require.Equal(t, "suite-now", ci.Failed[0].ExternalID)

	// The step asks for the commit's failed check runs to be read.
	asked := now.Add(-time.Second)
	record := domain.WorkflowCIRecord{Repository: "fixture/repo", Head: publishedHead, Failed: nil, DetailsSince: &asked}
	wake := now.Add(15 * time.Second)
	stale := *claimed
	stale.LeaseToken = ptrUUID(uuid.New())
	require.ErrorIs(t, repo.RecordWorkflowCI(ctx, stale, record, &wake), ErrAgentTaskConflict, "only the worker holding the lease records")
	require.NoError(t, repo.RecordWorkflowCI(ctx, *claimed, record, &wake))
	step, err := repo.CurrentWorkflowStep(ctx, *claimed)
	require.NoError(t, err)
	stored := step.CIRecord()
	require.NotNil(t, stored)
	require.Equal(t, publishedHead, stored.Head)
	require.False(t, stored.DetailsRead)

	// A read of another repository's commit does not reach it; this one does,
	// and wakes the task.
	_, err = repo.RecordCIDetails(ctx, ws, "other/repo", publishedHead, "elsewhere")
	require.NoError(t, err)
	step, err = repo.CurrentWorkflowStep(ctx, *claimed)
	require.NoError(t, err)
	require.False(t, step.CIRecord().DetailsRead)
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_wake_at=NULL WHERE id=$1`, claimed.ID)
	require.NoError(t, err)
	_, err = repo.RecordCIDetails(ctx, ws, "Fixture/Repo", publishedHead, `"E2E" concluded failure in step "Run tests"`)
	require.NoError(t, err)
	step, err = repo.CurrentWorkflowStep(ctx, *claimed)
	require.NoError(t, err)
	stored = step.CIRecord()
	require.True(t, stored.DetailsRead)
	require.Equal(t, `"E2E" concluded failure in step "Run tests"`, stored.Details)
	require.Equal(t, "fixture/repo", stored.Repository, "the rest of the record is kept")
	var woken bool
	require.NoError(t, db.Get(&woken, `SELECT workflow_wake_at IS NOT NULL FROM agent_tasks WHERE id=$1`, claimed.ID))
	require.True(t, woken)

	// The step closes with what it judged; a later CI step on the same commit
	// finds it.
	stored.Failed = []string{"https://github.com/fixture/repo/actions/runs/9/job/1"}
	toShip := graph.Decide(claimed.Cursor(), domain.WorkflowChecksPassed, now, nil)
	moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: domain.WorkflowChecksPassed, CI: stored,
		Moves: []WorkflowMove{{Decision: toShip}}, Entry: WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · " + toShip.Reason, WakeAt: &now, NeedsPerson: true}})
	require.NoError(t, err)
	history, err := repo.WorkflowCIHistory(ctx, *moved, publishedHead)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "ci", history[0].NodeID)
	require.NotNil(t, history[0].FinishedAt)
	require.Equal(t, stored.Failed, history[0].CIRecord().Failed)
	none, err := repo.WorkflowCIHistory(ctx, *moved, "fedcba9876543210fedcba9876543210fedcba98")
	require.NoError(t, err)
	require.Empty(t, none)
}

// KUA-216: the merge approved at a ship approval before agent steps is
// deferred to the done end: the approval records it, it is found while only
// agent steps followed, the done end waits for it, and completes on it.
func TestAShipMergeDeferredPastAgentStepsCompletesOnTheDoneEnd(t *testing.T) {
	db, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	now := time.Now()
	for _, step := range []struct{ outcome, node string }{{domain.WorkflowPublished, "review"}, {domain.WorkflowReviewApproved, "ci"}, {domain.WorkflowChecksPassed, "ship"}} {
		decision := graph.Decide(claimed.Cursor(), step.outcome, now, nil)
		entry := WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · " + decision.Reason, WakeAt: &now, NeedsPerson: step.node == "ship"}
		if step.node == "review" {
			entry = WorkflowEntry{Kind: WorkflowEnterReview, Reason: "Workflow · " + decision.Reason, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}}
		}
		moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: step.outcome, Moves: []WorkflowMove{{Decision: decision}}, Entry: entry})
		require.NoError(t, err)
		claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits, claimed.WorkflowStatus, claimed.State = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits, moved.WorkflowStatus, moved.State
	}
	none, err := repo.DeferredShipMerge(ctx, *claimed)
	require.NoError(t, err)
	require.Nil(t, none)

	access, _ := domain.AccessFromContext(ctx)
	shipNode, _ := graph.Node("ship")
	agentNode := domain.WorkflowNode{ID: "agent-2", Type: domain.WorkflowNodeAgent, Label: "Agent step"}
	doneNode, _ := graph.Node("done")
	deferred := &domain.MergeWait{Approved: publishedHead, ActorID: &access.UserID, Deferred: true, Repository: "fixture/repo", PullRequest: 7, BaseBranch: "main", Since: now, Deadline: now.Add(2 * time.Hour)}
	moved, err := repo.DecideWorkflow(ctx, claimed.WorkspaceID, claimed.ID, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: domain.WorkflowApprovalApproved, StepMerge: deferred,
		Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: shipNode, To: agentNode, Reason: "Ready to ship → Agent step: approved"}}},
		Entry: WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · Ready to ship → Agent step: approved", WakeAt: &now}})
	require.NoError(t, err)
	claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits, claimed.WorkflowStatus, claimed.State = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits, moved.WorkflowStatus, moved.State
	found, err := repo.DeferredShipMerge(ctx, *claimed)
	require.NoError(t, err)
	require.NotNil(t, found, "only an agent step followed the approval")
	require.True(t, found.Deferred)
	require.Equal(t, 7, found.PullRequest)
	require.Equal(t, publishedHead, found.Approved)

	// Completing on a step that is not an end is refused.
	_, err = repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Hold: true, Outcome: domain.WorkflowEndDone, Entry: WorkflowEntry{Kind: WorkflowEnterComplete, Reason: "Workflow · Done"}})
	require.ErrorIs(t, err, ErrAgentTaskConflict)

	// The done end waits for the merge, open.
	started := *deferred
	started.Deferred, started.AfterAgents = false, true
	moved, err = repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: domain.WorkflowAgentFinished,
		Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: agentNode, To: doneNode, Reason: "Agent step → Done: agent finished"}}},
		Entry: WorkflowEntry{Kind: WorkflowEnterWait, Merge: &started, WakeAt: &now, Reason: "Workflow · Agent step → Done: agent finished; Kuayle merges pull request #7, as approved, once every check passes"}})
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", moved.State)
	require.Equal(t, domain.WorkflowStatusWaiting, *moved.WorkflowStatus)
	require.Equal(t, "done", *moved.WorkflowNode)
	end, err := repo.CurrentWorkflowStep(ctx, *moved)
	require.NoError(t, err)
	require.Nil(t, end.FinishedAt, "the end is open while it merges")
	require.Nil(t, end.Outcome)
	require.True(t, end.MergeWait().AfterAgents)
	none, err = repo.DeferredShipMerge(ctx, *moved)
	require.NoError(t, err)
	require.Nil(t, none, "the end carries the merge now")
	claimed.WorkflowSequence, claimed.WorkflowStatus, claimed.State = moved.WorkflowSequence, moved.WorkflowStatus, moved.State

	completed, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Hold: true, Outcome: domain.WorkflowEndDone, Detail: "Merged pull request #7 at 01234567",
		Entry: WorkflowEntry{Kind: WorkflowEnterComplete, Reason: "Workflow · Done: merged pull request #7 at 01234567"}})
	require.NoError(t, err)
	require.Equal(t, "completed", completed.State)
	require.Equal(t, domain.WorkflowStatusDone, *completed.WorkflowStatus)
	end, err = repo.CurrentWorkflowStep(ctx, *completed)
	require.NoError(t, err)
	require.NotNil(t, end.FinishedAt)
	require.Equal(t, domain.WorkflowEndDone, *end.Outcome)
	require.Equal(t, "Merged pull request #7 at 01234567", end.Detail)
	var reason string
	require.NoError(t, db.Get(&reason, `SELECT reason FROM agent_task_transitions WHERE task_id=$1 AND to_state='completed'`, claimed.ID))
	require.Contains(t, reason, "merged pull request #7")
}
