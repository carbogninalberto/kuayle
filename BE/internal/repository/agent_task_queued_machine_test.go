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
	action, key := "spawn", "spawn:1"
	if status != domain.DevMachineStatusQueued {
		action, key = "start", "task-start:"+task.ID.String()+":1:0"
	}
	spawn := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machine_operations(id,machine_id,workspace_id,action,status,generation,idempotency_key,attempts,error_code,error_message,available_at)
 VALUES($1,$2,$3,$4,'pending',1,$5,0,'host_capacity_wait','waiting for host memory: this machine needs 12288 MB and running machines already use 4096 MB of the 20480 MB available to machines',now()+interval '30 seconds')`,
		spawn, machine, fixture.workspaceID, action, key)
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

func TestDiskWaitTaskIsReclaimedAfterItsMachineStartSettles(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	_, start := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
	const reason = "Host disk below the Dev Machine floor (35.7 GB free; floor 50 GB and 15%)"
	_, err := db.Exec(`UPDATE dev_machine_operations SET error_code='host_disk_low',error_message=$2 WHERE id=$1`, start, reason)
	require.NoError(t, err)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "waiting_for_disk", reason))
	require.NoError(t, repo.Release(ctx, *claimed))
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "waiting_for_disk", current.State)
	require.Equal(t, reason, current.Reason)
	waiting, err := repo.WaitingForHostDisk(ctx, *current)
	require.NoError(t, err)
	require.True(t, waiting, "the worker must leave the task waiting while the start is deferred")
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased' WHERE id=$1`, start)
	require.NoError(t, err)
	waiting, err = repo.WaitingForHostDisk(ctx, *current)
	require.NoError(t, err)
	require.True(t, waiting, "leasing the start for a retry does not release the task")

	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE id=$1`, start)
	require.NoError(t, err)
	claimed, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, "waiting_for_disk", claimed.State)
	waiting, err = repo.WaitingForHostDisk(ctx, *claimed)
	require.NoError(t, err)
	require.False(t, waiting)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing the dev machine"))
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "preparing", current.State)
	require.Equal(t, task.Generation, current.Generation)
	require.Equal(t, task.RunID, current.RunID)
	transitions, err := repo.Transitions(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "waiting_for_disk", transitions[len(transitions)-2].ToState)
	require.Equal(t, reason, transitions[len(transitions)-2].Reason)
}

func TestCancellingTaskInDiskWaitWithdrawsItsDeferredStart(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	_, start := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusStopped)
	_, err := db.Exec(`UPDATE dev_machine_operations SET error_code='host_disk_low' WHERE id=$1`, start)
	require.NoError(t, err)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "waiting_for_disk", "Host disk below the Dev Machine floor"))
	cancelling := cancelClaimed(t, repo, task)
	require.NoError(t, repo.CancelAttempt(ctx, cancelling))
	var status string
	require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_operations WHERE id=$1`, start))
	require.Equal(t, "cancelled", status)
}

func TestLegacyDiskRefusalRequeuesOnlyTheCurrentTaskMachineStart(t *testing.T) {
	db, _, ctx, task, fixture := taskRecoveryFixture(t)
	machine, start := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusFailed)
	_, err := db.Exec(`UPDATE dev_machine_operations SET status='failed',error_code='host_disk_low',attempts=1,completed_at=now() WHERE id=$1`, start)
	require.NoError(t, err)
	other := fixture.insertMachine(t, domain.DevMachineStatusFailed, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`INSERT INTO dev_machine_operations(machine_id,workspace_id,action,status,generation,idempotency_key,error_code)
	VALUES ($1,$2,'spawn','failed',1,'unrelated','host_disk_low'),($3,$2,'spawn','failed',0,'stale','host_disk_low')`, other, task.WorkspaceID, machine)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	require.NoError(t, store.RequeueHostDiskRefusals(ctx, "Host disk below the Dev Machine floor (35.7 GB free; floor 50 GB and 15%)"))
	op, err := store.GetMachineStartOperation(ctx, task.WorkspaceID, machine, 1)
	require.NoError(t, err)
	require.NotNil(t, op)
	require.Equal(t, start, op.ID)
	require.Equal(t, domain.DevMachineOpStatusPending, op.Status)
	require.Zero(t, op.Attempts)
	require.Nil(t, op.CompletedAt)
	require.Contains(t, *op.ErrorMessage, "35.7 GB free")
	var dropped int
	require.NoError(t, db.Get(&dropped, `SELECT COUNT(*) FROM dev_machine_operations WHERE status='failed' AND error_code='host_disk_low'`))
	require.Equal(t, 2, dropped)
}

func TestHostDiskWaitNotifiesOnlyWorkspaceAdminsOncePerBreach(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	machine, _ := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusQueued)
	secondMachine := fixture.insertMachine(t, domain.DevMachineStatusQueued, domain.DevMachineStatusRunning, false)
	admin, member := uuid.New(), uuid.New()
	for role, user := range map[string]uuid.UUID{"admin": admin, "member": member} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,$3,'test')`, user, user.String()+"@example.test", role)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, task.WorkspaceID, user, role)
		require.NoError(t, err)
	}
	// The additional admin has only workspace membership.
	store := NewNotificationRepository(db)
	breach := uuid.New()
	const reason = "Host disk below the Dev Machine floor (35.7 GB free; floor 50 GB and 15%)"
	require.NoError(t, store.NotifyHostDiskWait(ctx, task.WorkspaceID, secondMachine, breach, reason))
	var count int
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM notifications WHERE workspace_id=$1`, task.WorkspaceID))
	require.Zero(t, count, "a manual machine with no task waiting does not trigger an alert")
	require.NoError(t, store.NotifyHostDiskWait(ctx, task.WorkspaceID, machine, breach, reason))
	// A second refused task on another machine still alerts once.
	secondTask, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Other work", Provider: "codex"})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET state='preparing',machine_id=$2 WHERE id=$1`, secondTask.ID, secondMachine)
	require.NoError(t, err)
	require.NoError(t, store.NotifyHostDiskWait(ctx, task.WorkspaceID, secondMachine, breach, reason))
	for _, user := range []uuid.UUID{task.CreatorID, admin, member} {
		recipient := domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: user})
		notices, err := store.ListByUser(recipient, user, 10, 0)
		require.NoError(t, err)
		if user == member {
			require.Empty(t, notices)
			continue
		}
		require.Len(t, notices, 1)
		require.Equal(t, "host_disk_low", notices[0].Type)
		require.Nil(t, notices[0].IssueID)
		require.Contains(t, notices[0].Title, reason)
	}
	// A new breach can alert again. A demoted admin loses access to old alerts.
	_, err = db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, admin)
	require.NoError(t, err)
	require.NoError(t, store.NotifyHostDiskWait(ctx, task.WorkspaceID, secondMachine, uuid.New(), reason))
	notices, err := store.ListByUser(ctx, task.CreatorID, 10, 0)
	require.NoError(t, err)
	require.Len(t, notices, 2)
	notices, err = store.ListByUser(domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: admin}), admin, 10, 0)
	require.NoError(t, err)
	require.Empty(t, notices)
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

// A start the user requested on the task's stopped machine is the user's: it is
// not withdrawn, and the machine still starts.
func TestCancellingTaskKeepsAStartTheUserRequested(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	machine, start := queuedTaskMachine(t, db, task, fixture, domain.DevMachineStatusStopped)
	_, err := db.Exec(`UPDATE dev_machine_operations SET idempotency_key='start:2',requested_by_user_id=$2 WHERE id=$1`, start, task.CreatorID)
	require.NoError(t, err)
	claimed := cancelClaimed(t, repo, task)
	require.NoError(t, repo.CancelAttempt(ctx, claimed))
	var startStatus, desired string
	require.NoError(t, db.Get(&startStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, start))
	require.Equal(t, "pending", startStatus)
	require.NoError(t, db.Get(&desired, `SELECT desired_status FROM dev_machines WHERE id=$1`, machine))
	require.Equal(t, "running", desired)
}
