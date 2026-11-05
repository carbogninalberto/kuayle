package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// queuedTaskMachine gives the task its own machine, still waiting to start:
// its spawn was deferred for host capacity, which consumes no attempts.
func queuedTaskMachine(t *testing.T, db *sqlx.DB, task domain.AgentTask, fixture environmentRaceFixture, status domain.DevMachineStatus) (uuid.UUID, uuid.UUID) {
	t.Helper()
	machine := fixture.insertMachine(t, status, domain.DevMachineStatusRunning, false)
	_, err := db.Exec(`UPDATE agent_tasks SET state='preparing',machine_id=$2,reserved_machine_id=$2 WHERE id=$1`, task.ID, machine)
	require.NoError(t, err)
	action := "spawn"
	if status != domain.DevMachineStatusQueued {
		action = "start"
	}
	spawn := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machine_operations(id,machine_id,workspace_id,action,status,generation,idempotency_key,attempts,error_code,error_message,available_at)
 VALUES($1,$2,$3,$4,'pending',1,$5,0,'host_capacity_wait','waiting for host memory: 12288 MB requested of 4096 MB available to machines',now()+interval '30 seconds')`,
		spawn, machine, fixture.workspaceID, action, action+":1")
	require.NoError(t, err)
	return machine, spawn
}

func taskContext(task domain.AgentTask) context.Context {
	return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: task.CreatorID})
}

func cancelClaimed(t *testing.T, repo *AgentTaskRepository, task domain.AgentTask) domain.AgentTask {
	t.Helper()
	ctx := taskContext(task)
	require.NoError(t, repo.RequestCancel(ctx, task.WorkspaceID, task.ID))
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, "cancelling", claimed.State)
	return *claimed
}

// Cancelling a task whose own machine waits for host capacity withdraws that
// start and retires the machine, instead of waiting for memory that may never
// free up and then booting a machine for a cancelled task.
func TestCancellingTaskWithdrawsItsQueuedMachine(t *testing.T) {
	db, repo, _, task, fixture := taskRecoveryFixture(t)
	machine, spawn := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
	claimed := cancelClaimed(t, repo, task)

	require.NoError(t, repo.CancelAttempt(taskContext(task), claimed))
	var spawnStatus string
	require.NoError(t, db.Get(&spawnStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, spawn))
	require.Equal(t, "cancelled", spawnStatus, "the machine must not boot for a cancelled task")
	var desired string
	var deleting bool
	require.NoError(t, db.QueryRow(`SELECT desired_status,delete_requested_at IS NOT NULL FROM dev_machines WHERE id=$1`, machine).Scan(&desired, &deleting))
	require.Equal(t, "destroyed", desired)
	require.True(t, deleting, "the machine is retired through the ordinary permanent deletion")

	// The teardown is cleanup, not capacity-gated work; cancellation waits for it.
	require.ErrorIs(t, repo.FinishCancellation(taskContext(task), claimed, "Runtime cancellation confirmed"), ErrAgentTaskConflict)
	_, err := db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE machine_id=$1 AND action='teardown'`, machine)
	require.NoError(t, err)
	require.NoError(t, repo.FinishCancellation(taskContext(task), claimed, "Runtime cancellation confirmed"))
	current, err := repo.Get(taskContext(task), task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", current.State)

	// Repeating the cleanup step is harmless.
	require.ErrorIs(t, repo.CancelAttempt(taskContext(task), claimed), ErrAgentTaskConflict)
}

// Revoked issue access cancels through the same cleanup step.
func TestRevokedTaskWithdrawsItsQueuedMachine(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	_, spawn := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	require.NoError(t, repo.Revoke(ctx, *claimed))
	cleanup, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	require.NoError(t, repo.CancelAttempt(ctx, *cleanup))
	var spawnStatus string
	require.NoError(t, db.Get(&spawnStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, spawn))
	require.Equal(t, "cancelled", spawnStatus)
}

// A start that the manager already picked up, or that already made an
// attempt, may have created runtime resources: cancellation still waits.
func TestCancellingTaskWaitsForAStartAlreadyUnderway(t *testing.T) {
	for name, change := range map[string]string{
		"leased":    `UPDATE dev_machine_operations SET status='leased',attempts=1,lease_owner='manager',lease_expires_at=now()+interval '2 minutes' WHERE id=$1`,
		"attempted": `UPDATE dev_machine_operations SET attempts=1 WHERE id=$1`,
	} {
		t.Run(name, func(t *testing.T) {
			db, repo, _, task, fixture := taskRecoveryFixture(t)
			machine, spawn := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
			_, err := db.Exec(change, spawn)
			require.NoError(t, err)
			claimed := cancelClaimed(t, repo, task)
			require.NoError(t, repo.CancelAttempt(taskContext(task), claimed))
			var spawnStatus string
			require.NoError(t, db.Get(&spawnStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, spawn))
			require.NotEqual(t, "cancelled", spawnStatus)
			var deleting bool
			require.NoError(t, db.Get(&deleting, `SELECT delete_requested_at IS NOT NULL FROM dev_machines WHERE id=$1`, machine))
			require.False(t, deleting)
			require.ErrorIs(t, repo.FinishCancellation(taskContext(task), claimed, "Runtime cancellation confirmed"), ErrAgentTaskConflict)
		})
	}
}

// A machine another task also waits for stays queued for that task.
func TestCancellingTaskKeepsAQueuedMachineAnotherTaskUses(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	machine, spawn := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
	other, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Other work", Provider: "codex"})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET state='preparing',machine_id=$2 WHERE id=$1`, other.ID, machine)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now()+interval '1 hour' WHERE id=$1`, other.ID)
	require.NoError(t, err)
	claimed := cancelClaimed(t, repo, task)
	require.NoError(t, repo.CancelAttempt(ctx, claimed))
	var spawnStatus string
	require.NoError(t, db.Get(&spawnStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, spawn))
	require.Equal(t, "pending", spawnStatus)
	var deleting bool
	require.NoError(t, db.Get(&deleting, `SELECT delete_requested_at IS NOT NULL FROM dev_machines WHERE id=$1`, machine))
	require.False(t, deleting)
}

// A machine the task did not create is never retired, even while queued.
func TestCancellingTaskKeepsAQueuedMachineItDidNotCreate(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	machine, spawn := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
	_, err := db.Exec(`UPDATE agent_tasks SET reserved_machine_id=gen_random_uuid() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	claimed := cancelClaimed(t, repo, task)
	require.NoError(t, repo.CancelAttempt(ctx, claimed))
	var spawnStatus string
	require.NoError(t, db.Get(&spawnStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, spawn))
	require.Equal(t, "pending", spawnStatus)
	var deleting bool
	require.NoError(t, db.Get(&deleting, `SELECT delete_requested_at IS NOT NULL FROM dev_machines WHERE id=$1`, machine))
	require.False(t, deleting)
	require.NoError(t, repo.FinishCancellation(ctx, claimed, "Runtime cancellation confirmed"))
}

// The task's own machine that already ran (stopped, now restarting for a
// retry) keeps its workspace: only the queued start is withdrawn.
func TestCancellingTaskWithdrawsTheRestartOfItsStoppedMachine(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	machine, start := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusStopped)
	_, err := db.Exec(`UPDATE dev_machines SET docker_network_name='net',workspace_volume_name='vol' WHERE id=$1`, machine)
	require.NoError(t, err)
	claimed := cancelClaimed(t, repo, task)
	require.NoError(t, repo.CancelAttempt(ctx, claimed))
	var startStatus string
	require.NoError(t, db.Get(&startStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, start))
	require.Equal(t, "cancelled", startStatus)
	var status, desired string
	var deleting bool
	require.NoError(t, db.QueryRow(`SELECT status,desired_status,delete_requested_at IS NOT NULL FROM dev_machines WHERE id=$1`, machine).Scan(&status, &desired, &deleting))
	require.Equal(t, "stopped", status)
	require.Equal(t, "stopped", desired)
	require.False(t, deleting)
	require.NoError(t, repo.FinishCancellation(ctx, claimed, "Runtime cancellation confirmed"))
}
