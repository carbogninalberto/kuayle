package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// machineShareFixture is a workspace with one issue chat whose tasks all
// belong to the fixture user, the creator of the fixture machines.
type machineShareFixture struct {
	environmentRaceFixture
	repo *AgentTaskRepository
	ctx  context.Context
	chat uuid.UUID
}

func newMachineShareFixture(t *testing.T) machineShareFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	team, issue := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Share','SHR')`, team, fixture.workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'SHR-1','Share',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, fixture.workspaceID, team, fixture.userID)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: fixture.workspaceID, UserID: fixture.userID})
	chat, err := NewConversationRepository(db).OpenIssue(ctx, fixture.workspaceID, issue)
	require.NoError(t, err)
	return machineShareFixture{environmentRaceFixture: fixture, repo: NewAgentTaskRepository(db), ctx: ctx, chat: chat.ID}
}

func (f machineShareFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := f.db.Exec(query, args...)
	require.NoError(t, err)
}

// preparingTask creates a task and claims it into the preparing state.
func (f machineShareFixture) preparingTask(t *testing.T) domain.AgentTask {
	t.Helper()
	created, err := f.repo.Create(f.ctx, f.workspaceID, f.chat, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement issue", Provider: "codex"})
	require.NoError(t, err)
	f.exec(t, `UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, created.ID)
	claimed, err := f.repo.Claim(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, created.ID, claimed.ID)
	require.NoError(t, f.repo.Transition(f.ctx, *claimed, "preparing", "Resolve runtime"))
	claimed.State = "preparing"
	return *claimed
}

// workingTask puts another task of the creator to work on machine: running,
// with its attempt running there.
func (f machineShareFixture) workingTask(t *testing.T, machine uuid.UUID) uuid.UUID {
	t.Helper()
	task, err := f.repo.Create(f.ctx, f.workspaceID, f.chat, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Other work", Provider: "codex"})
	require.NoError(t, err)
	f.exec(t, `UPDATE agent_tasks SET machine_id=$2,state='running',next_poll_at=now()+interval '1 hour' WHERE id=$1`, task.ID, machine)
	f.exec(t, `INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at)
 VALUES($1,$2,$3,$4,$5,'codex','autonomous','Other work','["codex"]',60,'running',now())`, task.RunID, f.workspaceID, machine, task.ID, f.userID)
	return task.ID
}

func (f machineShareFixture) runningMachine(t *testing.T, activity string) uuid.UUID {
	t.Helper()
	id := f.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	f.exec(t, `UPDATE dev_machines SET last_activity_at=now()-$2::interval WHERE id=$1`, id, activity)
	return id
}

// A task runs on a busy machine of its creator, beside the work already there,
// rather than creating a new machine. An idle machine still comes first; among
// busy machines the least loaded is chosen; a machine that already runs as many
// tasks as the workspace allows (two by default), or that does not suit the
// task, is not used.
func TestReservationSharesABusyMachineBelowItsTaskLimit(t *testing.T) {
	f := newMachineShareFixture(t)
	claimed := f.preparingTask(t)
	repository := uuid.New()
	reserve := func() uuid.UUID {
		t.Helper()
		f.exec(t, `UPDATE agent_tasks SET machine_id=NULL WHERE id=$1`, claimed.ID)
		claimed.MachineID = nil
		id, _, err := f.repo.ReserveMachine(f.ctx, claimed, repository, &f.environmentID)
		require.NoError(t, err)
		return id
	}

	busy := f.runningMachine(t, "1 minute")
	f.workingTask(t, busy)
	idle := f.runningMachine(t, "2 hours")
	require.Equal(t, idle, reserve(), "an idle machine comes before a busy one, even a more recently active one")

	f.exec(t, `UPDATE dev_machines SET status='paused',desired_status='paused' WHERE id=$1`, idle)
	require.Equal(t, busy, reserve(), "a busy machine below the limit is shared instead of creating a machine")
	// Queued validation and publication of the task already there do not keep
	// this one away.
	for _, action := range []string{"validate_agent", "publish_agent"} {
		f.exec(t, `INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,$3,'pending',1,$4)`, f.workspaceID, busy, action, "share-"+action)
	}
	require.Equal(t, busy, reserve())

	f.workingTask(t, busy)
	require.Equal(t, claimed.ReservedMachineID, reserve(), "a machine at the limit is not shared: a new machine is created")

	f.exec(t, `UPDATE dev_machine_workspace_policies SET task_machine_max_tasks=3 WHERE workspace_id=$1`, f.workspaceID)
	require.Equal(t, busy, reserve(), "the workspace limit decides how many tasks share a machine")
	lighter := f.runningMachine(t, "3 hours")
	f.workingTask(t, lighter)
	require.Equal(t, lighter, reserve(), "the least loaded busy machine is chosen")

	// A busy machine that does not suit the task is never shared.
	f.exec(t, `INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'stop','pending',1,'share-stop')`, f.workspaceID, lighter)
	require.Equal(t, busy, reserve(), "a machine about to stop is not shared")
	f.exec(t, `UPDATE dev_machine_operations SET status='completed' WHERE machine_id=$1 AND idempotency_key='share-stop'`, lighter)
	f.exec(t, `UPDATE dev_machines SET environment_id=NULL WHERE id=$1`, lighter)
	require.Equal(t, busy, reserve(), "a machine of another environment is not shared")
	f.exec(t, `UPDATE dev_machines SET environment_id=$2 WHERE id=$1`, lighter, f.environmentID)
	manual := uuid.New()
	f.exec(t, `INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,$4,'codex','interactive','Manual','["codex"]',60,'running')`, manual, f.workspaceID, lighter, f.userID)
	require.Equal(t, busy, reserve(), "a machine running the user's own agent session is not shared")
	f.exec(t, `UPDATE dev_machine_agent_runs SET status='succeeded' WHERE id=$1`, manual)
	f.exec(t, `INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'run_check','pending',1,'share-check')`, f.workspaceID, lighter)
	require.Equal(t, busy, reserve(), "a machine running a manual check is not shared")
	f.exec(t, `UPDATE dev_machines SET status='paused',desired_status='paused' WHERE id=$1`, busy)
	require.Equal(t, claimed.ReservedMachineID, reserve(), "with no suitable machine below the limit a new one is created")
}

// The workspace reservation lock serializes the choice, so concurrent tasks
// never take more places on a machine than the limit allows.
func TestConcurrentReservationsDoNotExceedTheMachineTaskLimit(t *testing.T) {
	f := newMachineShareFixture(t)
	machine := f.runningMachine(t, "1 minute")
	tasks := make([]domain.AgentTask, 4)
	for i := range tasks {
		tasks[i] = f.preparingTask(t)
	}
	chosen := make(chan uuid.UUID, len(tasks))
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Go(func() {
			id, _, err := f.repo.ReserveMachine(f.ctx, task, uuid.New(), &f.environmentID)
			if err != nil {
				t.Error(err)
				return
			}
			chosen <- id
		})
	}
	wg.Wait()
	close(chosen)
	shared := 0
	for id := range chosen {
		if id == machine {
			shared++
		}
	}
	require.Equal(t, 2, shared, "two tasks share the machine; the others get new machines")
}

func taskRun(task domain.AgentTask, machine uuid.UUID) (*domain.DevMachineAgentRun, *domain.DevMachineOperation) {
	run := &domain.DevMachineAgentRun{ID: task.RunID, WorkspaceID: task.WorkspaceID, MachineID: machine, IssueID: &task.IssueID, RequestedByUserID: &task.CreatorID, ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, Prompt: "Implement issue", AcceptanceCriteria: []byte(`[]`), AllowedCommands: []byte(`[]`), ForbiddenPaths: []byte(`[]`), AllowedSecrets: []byte(`[]`), CommandArgv: []byte(`["codex","exec","fixture"]`), MaxRuntimeSeconds: 60}
	op := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine, Action: domain.DevMachineOpRunAgent, Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "run-agent:" + run.ID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 3}
	return run, op
}

// Attempts of different tasks run on one machine at the same time, up to the
// limit. The user's own agent sessions still never share a machine with task
// attempts.
func TestTaskAttemptsRunTogetherOnASharedMachine(t *testing.T) {
	f := newMachineShareFixture(t)
	machine := f.runningMachine(t, "1 minute")
	tasks := make([]domain.AgentTask, 3)
	for i := range tasks {
		tasks[i] = f.preparingTask(t)
		f.exec(t, `UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, tasks[i].ID, machine)
		tasks[i].MachineID = &machine
	}
	start := func(task domain.AgentTask) error {
		run, op := taskRun(task, machine)
		return f.repository.CreateAgentRun(domain.WithTaskExecution(f.ctx, task), run, op)
	}
	require.NoError(t, start(tasks[0]))
	require.NoError(t, start(tasks[1]), "a second task's attempt runs beside the first")
	var active int
	require.NoError(t, f.db.Get(&active, `SELECT count(*) FROM dev_machine_agent_runs WHERE machine_id=$1 AND status='queued'`, machine))
	require.Equal(t, 2, active)
	require.ErrorIs(t, start(tasks[2]), ErrMachineBusy, "a third attempt waits for a place")

	manual := &domain.DevMachineAgentRun{ID: uuid.New(), WorkspaceID: f.workspaceID, MachineID: machine, RequestedByUserID: &f.userID, ProviderID: "codex", Mode: "interactive", Status: domain.DevMachineAgentRunStatusQueued, Prompt: "Manual", AcceptanceCriteria: []byte(`[]`), AllowedCommands: []byte(`[]`), ForbiddenPaths: []byte(`[]`), AllowedSecrets: []byte(`[]`), CommandArgv: []byte(`["codex"]`), MaxRuntimeSeconds: 60}
	require.ErrorIs(t, f.repository.CreateAgentRun(context.Background(), manual, &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: f.workspaceID, MachineID: machine, Action: domain.DevMachineOpRunAgent, Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "manual-" + manual.ID.String(), MaxAttempts: 3}), ErrActiveAgentRun, "the user's own session waits for the tasks")

	// Once the first task awaits review its place is free.
	f.exec(t, `UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE id=$1`, tasks[0].RunID)
	f.exec(t, `UPDATE agent_tasks SET state='awaiting_review' WHERE id=$1`, tasks[0].ID)
	require.NoError(t, start(tasks[2]))

	// The user's own session holds its machine: a task attempt waits for it.
	own := f.runningMachine(t, "1 minute")
	f.exec(t, `INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,$4,'codex','interactive','Manual','["codex"]',60,'running')`, uuid.New(), f.workspaceID, own, f.userID)
	waiting := f.preparingTask(t)
	f.exec(t, `UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, waiting.ID, own)
	waiting.MachineID = &own
	run, op := taskRun(waiting, own)
	require.ErrorIs(t, f.repository.CreateAgentRun(domain.WithTaskExecution(f.ctx, waiting), run, op), ErrMachineBusy)
}

// A finished attempt is validated and published while another task's attempt
// still runs on the same machine.
func TestValidationRunsBesideAnotherTasksAttempt(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["true"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	var machine uuid.UUID
	require.NoError(t, db.Get(&machine, `SELECT machine_id FROM agent_tasks WHERE id=$1`, task.ID))
	other, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Other work", Provider: "codex"})
	require.NoError(t, err)
	shareOtherTask(t, db, *other, machine)

	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.NotNil(t, step)
	var queued int
	require.NoError(t, db.Get(&queued, `SELECT count(*) FROM dev_machine_operations WHERE agent_run_id=$1 AND action='validate_agent' AND status='pending'`, task.RunID))
	require.Equal(t, 1, queued)
}

func shareOtherTask(t *testing.T, db *sqlx.DB, task domain.AgentTask, machine uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`UPDATE agent_tasks SET machine_id=$2,state='running',next_poll_at=now()+interval '1 hour' WHERE id=$1`, task.ID, machine)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at) VALUES($1,$2,$3,$4,$5,'codex','autonomous','Other','["codex"]',60,'running',now())`, task.RunID, task.WorkspaceID, machine, task.ID, task.CreatorID)
	require.NoError(t, err)
}

// The manager works on several tasks of one machine at once: an attempt,
// checkout, validation or publication is leased beside another task's long
// attempt, while lifecycle work still waits for the machine to be free.
func TestOperationsOfSharedMachineTasksAreLeasedTogether(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	insert := func(action, status string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.Exec(`INSERT INTO dev_machine_operations(id,workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,$3,$4,'pending',1,$5)`, id, fixture.workspaceID, machine, action, "lease-"+id.String())
		require.NoError(t, err)
		if status == "leased" {
			_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='other-manager',lease_expires_at=now()+interval '1 minute',attempts=1 WHERE id=$1`, id)
			require.NoError(t, err)
		}
		return id
	}
	lease := func() []uuid.UUID {
		t.Helper()
		ops, err := fixture.repository.LeaseOperations(context.Background(), "share-fixture", 10, time.Minute)
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(ops))
		for _, op := range ops {
			ids = append(ids, op.ID)
		}
		return ids
	}
	insert("run_agent", "leased")
	for _, action := range []string{"checkout_issue", "run_agent", "validate_agent", "publish_agent"} {
		id := insert(action, "pending")
		require.Equal(t, []uuid.UUID{id}, lease(), action+" is leased beside another task's attempt")
	}
	insert("start_preview", "pending")
	require.Empty(t, lease(), "other machine work waits while attempts run")
}
