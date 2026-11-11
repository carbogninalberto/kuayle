package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// publishingWorkflowTask is a task on the review loop workflow whose
// implementing step finished and whose pull request is being published.
func publishingWorkflowTask(t *testing.T) (*sqlx.DB, *AgentTaskRepository, context.Context, domain.WorkflowGraph, *domain.AgentTask) {
	t.Helper()
	db, repo, ctx, legacy, fixture := taskRecoveryFixture(t)
	_, err := db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	ws := legacy.WorkspaceID
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, fixture.userID, "Review loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(ctx, ws, legacy.IssueID, &chosen)
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, *legacy.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code", Workflow: version})
	require.NoError(t, err)
	claimed := prepareTaskStart(t, db, repo, ctx, *task, fixture)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0 WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	toPublish := graph.Decide(task.Cursor(), domain.WorkflowAgentFinished, time.Now(), nil)
	moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: 2, Outcome: domain.WorkflowAgentFinished, Moves: []WorkflowMove{{Decision: toPublish}}, Entry: WorkflowEntry{Kind: WorkflowEnterPublish, Reason: "Workflow · " + toPublish.Reason}})
	require.NoError(t, err)
	require.Equal(t, "publishing", moved.State)
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source) VALUES
 ($1,$2,$3,'pull_request','fixture/repo','kuayle/wf',NULL,7,'https://github.com/fixture/repo/pull/7','published'),
 ($1,$2,$3,'commit','fixture/repo','kuayle/wf','0123456789abcdef0123456789abcdef01234567',NULL,NULL,'published')`, ws, *claimed.MachineID, task.RunID)
	require.NoError(t, err)
	claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits
	return db, repo, ctx, graph, claimed
}

// claimTaskNotices claims the pending notices and returns this task's: other
// tests share the database.
func claimTaskNotices(t *testing.T, repo *AgentTaskRepository, ctx context.Context, task uuid.UUID) []domain.AgentTaskNotice {
	t.Helper()
	notices, err := repo.ClaimTaskNotices(ctx, 1000)
	require.NoError(t, err)
	var own []domain.AgentTaskNotice
	for _, notice := range notices {
		if notice.TaskID == task {
			own = append(own, notice)
		}
	}
	return own
}

// A workflow tells the creator that a review is needed when it reaches a step
// that waits for a person, also when the task already waited (for its agent
// review, for CI) and so records no transition; those waits need nobody.
func TestWorkflowNotifiesWhenAStepWaitsForAPerson(t *testing.T) {
	_, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	now := time.Now()
	apply := func(outcome, wantNode string, entry WorkflowEntry) *domain.AgentTask {
		t.Helper()
		decision := graph.Decide(claimed.Cursor(), outcome, now, nil)
		require.Equal(t, wantNode, decision.To.ID)
		entry.Reason = "Workflow · " + decision.Reason
		moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: outcome, Moves: []WorkflowMove{{Decision: decision}}, Entry: entry})
		require.NoError(t, err)
		require.Equal(t, "awaiting_review", moved.State)
		claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits
		return moved
	}

	apply(domain.WorkflowPublished, "review", WorkflowEntry{Kind: WorkflowEnterReview, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}})
	require.Empty(t, claimTaskNotices(t, repo, ctx, claimed.ID), "an agent reviews; nobody is needed yet")

	apply(domain.WorkflowReviewApproved, "ci", WorkflowEntry{Kind: WorkflowEnterWait, WakeAt: &now})
	require.Empty(t, claimTaskNotices(t, repo, ctx, claimed.ID), "CI is pending; nobody is needed yet")

	// The task already is awaiting_review, so this records no transition.
	moved := apply(domain.WorkflowChecksPassed, "ship", WorkflowEntry{Kind: WorkflowEnterWait, WakeAt: &now, NeedsPerson: true})
	notices := claimTaskNotices(t, repo, ctx, claimed.ID)
	require.Len(t, notices, 1)
	require.Equal(t, "awaiting_review", notices[0].State)
	require.Equal(t, domain.NotificationReviewNeeded, domain.AgentTaskNotificationType(notices[0].State))
	require.Equal(t, moved.CreatorID, notices[0].CreatorID)
	require.Empty(t, claimTaskNotices(t, repo, ctx, claimed.ID), "the wait is announced once")
}

// Entering the CI wait from publishing is a transition into awaiting_review,
// but only CI is pending: it is not announced as ready for review.
func TestWorkflowCIWaitDoesNotAskForReview(t *testing.T) {
	_, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	publish, _ := graph.Node("publish")
	ci, _ := graph.Node("ci")
	now := time.Now()
	moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: domain.WorkflowPublished,
		Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: publish, To: ci, Reason: "Open pull request → CI"}}},
		Entry: WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · Open pull request → CI; waiting for CI on the pull request", WakeAt: &now}})
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", moved.State)
	require.Empty(t, claimTaskNotices(t, repo, ctx, claimed.ID))
}
