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

// idleTaskWithCheckout leaves a task awaiting review after a finished attempt,
// with its ready checkout on a running machine and the attempt's redaction
// snapshot, as a real attempt does.
func idleTaskWithCheckout(t *testing.T) (*sqlx.DB, *AgentTaskRepository, context.Context, domain.AgentTask, uuid.UUID) {
	t.Helper()
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	installation, repositoryID := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/repo','main')`, repositoryID, installation, task.WorkspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/rec-1','/workspace/tasks/rec-1','ready',$6)`, task.CheckoutID, task.WorkspaceID, *claimed.MachineID, task.IssueID, repositoryID, task.ID.String())
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0,checkout_id=$2,encrypted_output_secrets='sealed' WHERE id=$1`, task.RunID, task.CheckoutID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET state='awaiting_review',reason='Pull request opened; waiting for human review',lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, task.ID)
	require.NoError(t, err)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	return db, repo, ctx, *current, *claimed.MachineID
}

func leaseManualCheck(t *testing.T, db *sqlx.DB, ctx context.Context, task domain.AgentTask, step *domain.DevMachineAgentRunStep) domain.DevMachineOperation {
	t.Helper()
	store := NewDevMachineRepository(db)
	ops, err := store.LeaseOperations(ctx, "manual-check-fixture", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	op := ops[0]
	require.Equal(t, domain.DevMachineOpRunCheck, op.Action)
	require.NoError(t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "check-" + step.ID.String(), ContainerName: "check", ImageRef: "fixture", InternalHost: "check", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
	return op
}

func TestManualCheckRunsOnTheLatestAttemptWithoutChangingTheTask(t *testing.T) {
	db, repo, ctx, task, machine := idleTaskWithCheckout(t)
	_, err := repo.RunCheck(ctx, task.WorkspaceID, task.ID, nil)
	require.ErrorIs(t, err, ErrNoCheckCommand, "no command is configured yet")
	// A check configured after the attempt is the task's check now.
	_, err = db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,issue_id,test_command,compose_file) VALUES($1,$2,'["npm","test"]','compose.yaml')`, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	step, err := repo.RunCheck(ctx, task.WorkspaceID, task.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "manual_check", step.StepType)
	require.Equal(t, task.RunID, step.AgentRunID)
	require.Equal(t, domain.DevMachineAgentRunStepStatusQueued, step.Status)
	require.Equal(t, 100, step.Sequence, "manual checks never take the validation or publication sequence")
	require.JSONEq(t, `["npm","test"]`, string(step.CommandArgv))
	require.Equal(t, "compose.yaml", step.ComposeFile)
	var op domain.DevMachineOperation
	require.NoError(t, db.Get(&op, `SELECT * FROM dev_machine_operations WHERE agent_run_id=$1 AND action='run_check'`, task.RunID))
	require.Equal(t, "task-check:"+step.ID.String(), op.IdempotencyKey)
	require.Equal(t, machine, op.MachineID)
	require.Equal(t, task.ID, *op.AgentTaskID)

	_, err = repo.RunCheck(ctx, task.WorkspaceID, task.ID, nil)
	require.ErrorIs(t, err, ErrMachineBusy, "one check at a time")
	competing := &domain.DevMachineAgentRun{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine}
	require.ErrorIs(t, NewDevMachineRepository(db).CreateAgentRun(context.Background(), competing, &domain.DevMachineOperation{Generation: op.Generation}), ErrMachineBusy, "an attempt waits for the check")

	store := NewDevMachineRepository(db)
	leased := leaseManualCheck(t, db, ctx, task, step)
	require.ErrorIs(t, store.CompleteValidation(ctx, leased, 0, nil), ErrAgentTaskConflict, "a result needs a real start")
	require.NoError(t, store.StartValidation(ctx, leased, "container"))
	chunk := &domain.DevMachineLogChunk{WorkspaceID: task.WorkspaceID, MachineID: machine, AgentRunID: &task.RunID, ServiceID: &step.ID, Stream: "stdout", Sequence: 1, Content: "3 tests failed", SourceKey: "source"}
	require.NoError(t, store.CreateValidationOutput(ctx, leased, chunk))
	require.NoError(t, store.CompleteValidation(ctx, leased, 3, nil))
	require.NoError(t, store.CompleteOperation(ctx, leased.ID, *leased.LeaseOwner))

	checks, err := repo.ManualChecks(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Len(t, checks, 1)
	require.Equal(t, domain.DevMachineAgentRunStepStatusFailed, checks[0].Status)
	require.Equal(t, 3, *checks[0].ExitCode)
	require.Equal(t, "The check failed", *checks[0].Summary)
	validation, err := repo.Validation(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Nil(t, validation, "a manual check is not the attempt's independent validation")

	after, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", after.State)
	require.Equal(t, task.Generation, after.Generation)
	require.Equal(t, task.RunID, after.RunID)
	var runs, transitions, events int
	require.NoError(t, db.Get(&runs, `SELECT count(*) FROM dev_machine_agent_runs WHERE task_id=$1`, task.ID))
	require.Equal(t, 1, runs, "no agent attempt starts")
	require.NoError(t, db.Get(&transitions, `SELECT count(*) FROM agent_task_transitions WHERE task_id=$1 AND to_state='awaiting_review'`, task.ID))
	require.Zero(t, transitions)
	require.NoError(t, db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.tests_updated'`, task.ConversationID))
	require.Equal(t, 3, events, "queued, running and finished; a refused check emits nothing")

	// A one-off command runs as given once the machine is free.
	custom, err := repo.RunCheck(ctx, task.WorkspaceID, task.ID, []string{"sh", "-c", "npm test -- auth"})
	require.NoError(t, err)
	require.JSONEq(t, `["sh","-c","npm test -- auth"]`, string(custom.CommandArgv))
	require.Equal(t, 101, custom.Sequence)
	_, err = repo.RunCheck(ctx, task.WorkspaceID, task.ID, []string{})
	require.ErrorIs(t, err, ErrConversationInvalid)

	stranger := domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.RunCheck(stranger, task.WorkspaceID, task.ID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.ManualChecks(stranger, task.WorkspaceID, task.ID, task.RunID)
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestManualCheckRefusesWhileTheAgentWorks(t *testing.T) {
	for _, scenario := range []string{"attempt", "validating", "other_task_attempt", "paused_machine", "no_checkout", "queued"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, ctx, task, machine := idleTaskWithCheckout(t)
			want := ErrMachineBusy
			var err error
			switch scenario {
			case "attempt":
				_, err = db.Exec(`UPDATE agent_tasks SET state='running' WHERE id=$1`, task.ID)
			case "validating":
				_, err = db.Exec(`UPDATE agent_tasks SET state='validating' WHERE id=$1`, task.ID)
			case "other_task_attempt":
				_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,'codex','autonomous','Other','["codex"]',60,'running')`, task.WorkspaceID, machine)
			case "paused_machine":
				want = ErrCheckUnavailable
				_, err = db.Exec(`UPDATE dev_machines SET status='paused',desired_status='paused' WHERE id=$1`, machine)
			case "no_checkout":
				want = ErrCheckUnavailable
				_, err = db.Exec(`UPDATE dev_machine_checkouts SET status='failed' WHERE id=$1`, task.CheckoutID)
			case "queued":
				want = ErrCheckUnavailable
				_, err = db.Exec(`UPDATE agent_tasks SET state='queued' WHERE id=$1`, task.ID)
			}
			require.NoError(t, err)
			_, err = repo.RunCheck(ctx, task.WorkspaceID, task.ID, []string{"true"})
			require.ErrorIs(t, err, want)
			var count int
			require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND step_type='manual_check'`, task.RunID))
			require.Zero(t, count)
		})
	}
}

func TestManualCheckRejectsRevokedAuthorityAndRecordsFailedOperations(t *testing.T) {
	for _, scenario := range []string{"retried", "cancelling", "membership", "expired_lease", "machine_generation", "operation_failed"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, ctx, task, _ := idleTaskWithCheckout(t)
			step, err := repo.RunCheck(ctx, task.WorkspaceID, task.ID, []string{"true"})
			require.NoError(t, err)
			op := leaseManualCheck(t, db, ctx, task, step)
			store := NewDevMachineRepository(db)
			require.NoError(t, store.StartValidation(ctx, op, "container"))
			switch scenario {
			case "retried":
				_, err = db.Exec(`UPDATE agent_tasks SET run_id=gen_random_uuid(),state='queued' WHERE id=$1`, task.ID)
			case "cancelling":
				_, err = db.Exec(`UPDATE agent_tasks SET state='cancelling' WHERE id=$1`, task.ID)
			case "membership":
				_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
			case "expired_lease":
				_, err = db.Exec(`UPDATE dev_machine_operations SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, op.ID)
			case "machine_generation":
				_, err = db.Exec(`UPDATE dev_machines SET generation=generation+1 WHERE id=$1`, op.MachineID)
			case "operation_failed":
				var retry bool
				retry, err = store.FailOperation(ctx, op.ID, *op.LeaseOwner, "check_stale", "Machine changed during the check", false)
				require.False(t, retry)
			}
			require.NoError(t, err)
			_, err = store.GetValidation(ctx, op)
			require.Error(t, err)
			require.Error(t, store.CompleteValidation(ctx, op, 0, nil))
			require.Error(t, store.CreateValidationOutput(ctx, op, &domain.DevMachineLogChunk{WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceID: &step.ID, Stream: "stdout", Sequence: 1, Content: "must not persist"}))
			var status string
			require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_agent_run_steps WHERE id=$1`, step.ID))
			require.Equal(t, "running", status)
			if scenario == "operation_failed" {
				require.NoError(t, store.FailManualCheck(context.Background(), op, "The check could not finish"))
				var summary string
				require.NoError(t, db.QueryRow(`SELECT status,summary FROM dev_machine_agent_run_steps WHERE id=$1`, step.ID).Scan(&status, &summary))
				require.Equal(t, "failed", status)
				require.Equal(t, "The check could not finish", summary)
			} else {
				require.ErrorIs(t, store.FailManualCheck(context.Background(), op, "x"), ErrAgentTaskConflict, "only a failed operation fails its step")
			}
		})
	}
}

func TestManualCheckStepsDoNotBreakCheckCommandDecoding(t *testing.T) {
	db, repo, ctx, task, _ := idleTaskWithCheckout(t)
	_, err := db.Exec(`UPDATE agent_tasks SET test_command='["go","test","./..."]',validation_command='["go","test","./..."]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	// A command requested for the task wins over the configured one.
	_, err = db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,issue_id,test_command) VALUES($1,$2,'["npm","test"]')`, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	step, err := repo.RunCheck(ctx, task.WorkspaceID, task.ID, nil)
	require.NoError(t, err)
	var argv []string
	require.NoError(t, json.Unmarshal(step.CommandArgv, &argv))
	require.Equal(t, []string{"go", "test", "./..."}, argv)
}

func TestAgentWorkReportsTheLatestManualCheckAndKeepsItsOutputPrivate(t *testing.T) {
	db, repo, ctx, task, machine := idleTaskWithCheckout(t)
	work, err := repo.IssueAgentWork(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Len(t, work, 1)
	require.Nil(t, work[0].ManualCheck)
	step, err := repo.RunCheck(ctx, task.WorkspaceID, task.ID, []string{"npm", "test"})
	require.NoError(t, err)
	op := leaseManualCheck(t, db, ctx, task, step)
	store := NewDevMachineRepository(db)
	require.NoError(t, store.StartValidation(ctx, op, "container"))
	for i, line := range []string{"PASS auth\n", "FAIL search: expected 2 results\n"} {
		require.NoError(t, store.CreateValidationOutput(ctx, op, &domain.DevMachineLogChunk{WorkspaceID: task.WorkspaceID, MachineID: machine, AgentRunID: &task.RunID, ServiceID: &step.ID, Stream: "stdout", Sequence: int64(i + 1), Content: line, SourceKey: "source"}))
	}
	require.NoError(t, store.CompleteValidation(ctx, op, 1, nil))
	type check struct {
		Status     string   `json:"status"`
		ExitCode   *int     `json:"exit_code"`
		Command    []string `json:"command"`
		OutputTail *string  `json:"output_tail"`
	}
	read := func(ctx context.Context) check {
		work, err := repo.IssueAgentWork(ctx, task.WorkspaceID, task.IssueID)
		require.NoError(t, err)
		require.Len(t, work, 1)
		require.NotNil(t, work[0].ManualCheck)
		var result check
		require.NoError(t, json.Unmarshal(*work[0].ManualCheck, &result))
		return result
	}
	own := read(ctx)
	require.Equal(t, "failed", own.Status)
	require.Equal(t, 1, *own.ExitCode)
	require.Equal(t, []string{"npm", "test"}, own.Command)
	require.Equal(t, "PASS auth\nFAIL search: expected 2 results\n", *own.OutputTail)
	member := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Teammate','hash')`, member, member.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, task.WorkspaceID, member)
	require.NoError(t, err)
	teammate := read(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: member}))
	require.Equal(t, "failed", teammate.Status, "issue viewers see the result")
	require.Nil(t, teammate.OutputTail, "task output stays with its creator")
}
