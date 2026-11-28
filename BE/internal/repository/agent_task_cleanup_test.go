package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTaskResourceCleanupEligibility(t *testing.T) {
	db, tasks, actorCtx, task, fixture := taskRecoveryFixture(t)
	ctx := context.Background()
	store := NewDevMachineRepository(db)
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning)
	linkGitHubPullRequest(t, db, task.WorkspaceID, task.CreatorID, task.IssueID, 226, "open")
	_, err := db.Exec(`UPDATE dev_machines SET workspace_volume_name='fixture' WHERE id=$1`, machine)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,scope_key,status)
 SELECT $1,$2,$3,$4,id,full_name,'main',$5,$6,$7,'ready' FROM github_repos WHERE workspace_id=$2`,
		task.CheckoutID, task.WorkspaceID, machine, task.IssueID, "kuayle/rec-1-"+task.ID.String(), "/workspace/tasks/rec-1-"+task.ID.String(), task.ID.String())
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, task.ID, machine)
	require.NoError(t, err)

	now := time.Now()
	setTask := func(state string, age time.Duration) {
		t.Helper()
		_, err := db.Exec(`UPDATE agent_tasks SET state=$2,updated_at=$3 WHERE id=$1`, task.ID, state, now.Add(-age))
		require.NoError(t, err)
	}
	setPR := func(state string) {
		t.Helper()
		_, err := db.Exec(`UPDATE github_pull_requests SET state=$1 WHERE workspace_id=$2 AND number=226`, state, task.WorkspaceID)
		require.NoError(t, err)
	}
	queue := func(at time.Time, want int64) {
		t.Helper()
		count, err := store.QueueTaskResourceCleanup(ctx, at, 100)
		require.NoError(t, err)
		require.Equal(t, want, count)
	}
	setTask("completed", time.Minute)
	queue(now, 0)
	setTask("cancelled", time.Minute)
	queue(now, 0)
	setTask("running", 48*time.Hour)
	queue(now, 0)
	setTask("failed", 24*time.Hour)
	setPR("closed")
	queue(now, 0) // A PR merely linked to the issue is insufficient.

	// The failed iteration may have published nothing. Its earlier runs'
	// published PR refs still identify the task's branch.
	oldRun := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,checkout_id,task_id,requested_by_user_id,provider_id,mode,status,prompt,command_argv,max_runtime_seconds)
 VALUES($1,$2,$3,$4,$5,$6,'codex','autonomous','failed','old attempt','[]',60)`, oldRun, task.WorkspaceID, machine, task.CheckoutID, task.ID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,issue_id,agent_run_id,ref_type,repository_full_name,ref_name,pull_request_number,source)
 VALUES($1,$2,$3,$4,'pull_request','acme/app',$5,226,'published')`, task.WorkspaceID, machine, task.IssueID, oldRun, "kuayle/rec-1-"+task.ID.String())
	require.NoError(t, err)
	setPR("open")
	queue(now, 0)
	setPR("merged")
	session := &domain.DevMachineTerminalSession{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine, CheckoutID: &task.CheckoutID,
		UserID: task.CreatorID, Name: "Fixture", RuntimeSessionName: "task-fixture", Status: "active"}
	require.NoError(t, store.CreateTerminalSession(ctx, session))
	queue(now, 0)
	_, err = db.Exec(`UPDATE dev_machine_terminal_sessions SET status='closed' WHERE id=$1`, session.ID)
	require.NoError(t, err)
	queue(now, 1)
	queue(now, 0)
	operations, err := store.LeaseOperations(ctx, "cleanup-test", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, operations, 1)
	op := operations[0]
	require.Equal(t, domain.DevMachineOpCleanupTask, op.Action)

	setPR("open")
	checkout, runs, err := store.BeginTaskResourceCleanup(ctx, op, now)
	require.NoError(t, err)
	require.Nil(t, checkout, "a reopened PR restores the retry window")
	require.Empty(t, runs)
	checkout, runs, err = store.BeginTaskResourceCleanup(ctx, op, now.Add(8*24*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, checkout)
	require.Equal(t, task.CheckoutID, checkout.ID)
	require.Equal(t, "retired", checkout.Status)
	require.Equal(t, []uuid.UUID{oldRun}, runs)
	session.ID, session.RuntimeSessionName = uuid.New(), "task-fixture-retired"
	require.ErrorIs(t, store.CreateTerminalSession(ctx, session), sql.ErrNoRows)
	require.NoError(t, store.CompleteOperation(ctx, op.ID, "cleanup-test"))
	queue(now.Add(8*24*time.Hour), 0)

	_, err = db.Exec(`UPDATE agent_tasks SET generation=generation+1 WHERE id=$1`, task.ID)
	require.NoError(t, err)
	setTask("completed", time.Hour)
	queue(now, 1)
	setTask("failed", time.Hour)
	setPR("closed")
	current, err := tasks.Get(actorCtx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	retry, err := tasks.Retry(actorCtx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{
		RequestID: uuid.New(), Generation: current.Generation, Prompt: "Try again",
	})
	require.NoError(t, err)
	require.Equal(t, "queued", retry.State)
	operations, err = store.LeaseOperations(ctx, "cleanup-test", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, operations, 1)
	checkout, _, err = store.BeginTaskResourceCleanup(ctx, operations[0], now)
	require.NoError(t, err)
	require.Nil(t, checkout, "a queued follow-up fences stale cleanup")
}
