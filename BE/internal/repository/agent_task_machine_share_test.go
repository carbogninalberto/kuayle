package repository

import (
	"context"
	"os"
	"strings"
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

// taskLimit sets how many tasks share a machine in the fixture workspace.
func (f machineShareFixture) taskLimit(t *testing.T, limit int) {
	t.Helper()
	f.exec(t, `UPDATE dev_machine_workspace_policies SET task_machine_max_tasks=$2 WHERE workspace_id=$1`, f.workspaceID, limit)
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
// tasks as the workspace allows (two here), or that does not suit the task, is
// not used.
func TestReservationSharesABusyMachineBelowItsTaskLimit(t *testing.T) {
	f := newMachineShareFixture(t)
	f.taskLimit(t, 2)
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

// With the default limit ten tasks of a user share one running machine. The
// eleventh gets a machine of its own, which waits while the user's machine
// limit is reached; ten attempts run on the shared machine together and an
// eleventh attempt waits for a place.
func TestTenTasksShareOneMachineByDefault(t *testing.T) {
	f := newMachineShareFixture(t)
	var limit int
	require.NoError(t, f.db.Get(&limit, `SELECT task_machine_max_tasks FROM dev_machine_workspace_policies WHERE workspace_id=$1`, f.workspaceID))
	require.Equal(t, 10, limit, "a new workspace lets ten tasks share a machine")
	machine := f.runningMachine(t, "1 minute")
	for range 9 {
		f.workingTask(t, machine)
	}
	tenth := f.preparingTask(t)
	id, _, err := f.repo.ReserveMachine(f.ctx, tenth, uuid.New(), &f.environmentID)
	require.NoError(t, err)
	require.Equal(t, machine, id, "the tenth task shares the machine")

	eleventh := f.preparingTask(t)
	id, reserved, err := f.repo.ReserveMachine(f.ctx, eleventh, uuid.New(), &f.environmentID)
	require.NoError(t, err)
	require.Equal(t, eleventh.ReservedMachineID, id, "the eleventh task gets a new machine")
	require.Equal(t, eleventh.ReservedMachineID, reserved)

	f.exec(t, `UPDATE dev_machine_workspace_policies SET max_machines_per_user=1 WHERE workspace_id=$1`, f.workspaceID)
	routingKey := strings.ReplaceAll(reserved.String(), "-", "")[:16]
	err = f.repository.CreateBundle(context.Background(), &domain.DevMachine{
		ID: reserved, WorkspaceID: f.workspaceID, CreatedByUserID: &f.userID, RoutingKey: routingKey, Name: "task-" + routingKey,
		Status: domain.DevMachineStatusQueued, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepoProvider: "github",
		MachineSize: "custom", Profile: domain.DevMachineProfileHeadless, CPUMillis: 4000, MemoryMB: 8192, DiskGB: 20, PidsLimit: 512,
		ServicesConfig: []byte(`{}`), Labels: []byte(`{}`),
	}, nil, nil, nil, nil, nil, &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: reserved, WorkspaceID: f.workspaceID, Action: domain.DevMachineOpSpawn,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "spawn:1", RequestedByUserID: &f.userID, MaxAttempts: 5,
	})
	require.ErrorIs(t, err, ErrMachineQuota, "at the user's machine limit the eleventh task waits")

	// Ten attempts run on the machine together; an eleventh waits.
	start := func(task domain.AgentTask) error {
		task.MachineID = &machine
		f.exec(t, `UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, task.ID, machine)
		run, op := taskRun(task, machine)
		return f.repository.CreateAgentRun(domain.WithTaskExecution(f.ctx, task), run, op)
	}
	tenth.MachineID = &machine
	require.NoError(t, start(tenth), "the tenth attempt runs beside nine others")
	require.ErrorIs(t, start(eleventh), ErrMachineBusy, "an eleventh attempt waits for a place")
}

// The workspace reservation lock serializes the choice, so concurrent tasks
// never take more places on a machine than the limit allows.
func TestConcurrentReservationsDoNotExceedTheMachineTaskLimit(t *testing.T) {
	f := newMachineShareFixture(t)
	f.taskLimit(t, 2)
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
// limit (two here). The user's own agent sessions still never share a machine
// with task attempts.
func TestTaskAttemptsRunTogetherOnASharedMachine(t *testing.T) {
	f := newMachineShareFixture(t)
	f.taskLimit(t, 2)
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
// checkout, validation or publication of one task is leased beside another
// task's long attempt. The user's own agent runs and checkouts stay exclusive
// (they share the root workspace and its current checkout link), and other
// machine work waits.
func TestOperationsOfSharedMachineTasksAreLeasedTogether(t *testing.T) {
	f := newMachineShareFixture(t)
	machine := f.runningMachine(t, "1 minute")
	other := f.workingTask(t, machine)
	var issue uuid.UUID
	require.NoError(t, f.db.Get(&issue, `SELECT issue_id FROM agent_tasks WHERE id=$1`, other))
	installation, repository := uuid.New(), uuid.New()
	f.exec(t, `INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, f.workspaceID, f.userID)
	f.exec(t, `INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/share','main')`, repository, installation, f.workspaceID)
	checkout := func(scope string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		f.exec(t, `INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,scope_key)
 VALUES($1,$2,$3,$4,$5,'fixture/share','main',$6,$7,$8)`, id, f.workspaceID, machine, issue, repository, "kuayle/shr-1"+scope, "/workspace/tasks/shr-1"+scope, scope)
		return id
	}
	manualRun := func() uuid.UUID {
		t.Helper()
		id := uuid.New()
		f.exec(t, `INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,$4,'codex','interactive','Manual','["codex"]',60,'succeeded')`, id, f.workspaceID, machine, f.userID)
		return id
	}
	var otherRun uuid.UUID
	require.NoError(t, f.db.Get(&otherRun, `SELECT run_id FROM agent_tasks WHERE id=$1`, other))
	insert := func(action string, run, checkout *uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		f.exec(t, `INSERT INTO dev_machine_operations(id,workspace_id,machine_id,action,status,generation,idempotency_key,agent_run_id,checkout_id) VALUES($1,$2,$3,$4,'pending',1,$5,$6,$7)`, id, f.workspaceID, machine, action, "lease-"+id.String(), run, checkout)
		return id
	}
	lease := func() []uuid.UUID {
		t.Helper()
		ops, err := f.repository.LeaseOperations(context.Background(), "share-fixture", 10, time.Minute)
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(ops))
		for _, op := range ops {
			ids = append(ids, op.ID)
		}
		return ids
	}
	finish := func(ids ...uuid.UUID) {
		t.Helper()
		for _, id := range ids {
			f.exec(t, `UPDATE dev_machine_operations SET status='completed',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1`, id)
		}
	}
	attempt := insert("run_agent", &otherRun, nil)
	require.Equal(t, []uuid.UUID{attempt}, lease())

	// Task work of another task runs beside the attempt.
	taskCheckout := checkout("-task")
	f.exec(t, `UPDATE dev_machine_checkouts SET scope_key=$2 WHERE id=$1`, taskCheckout, uuid.NewString())
	for _, op := range []uuid.UUID{
		insert("checkout_issue", nil, &taskCheckout),
		insert("validate_agent", &otherRun, nil),
		insert("publish_agent", &otherRun, nil),
	} {
		require.Equal(t, []uuid.UUID{op}, lease(), "task work is leased beside another task's attempt")
		finish(op)
	}
	second := f.preparingTask(t)
	f.exec(t, `INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,$4,$5,'codex','autonomous','Second','["codex"]',60,'queued')`, second.RunID, f.workspaceID, machine, second.ID, f.userID)
	secondAttempt := insert("run_agent", &second.RunID, nil)
	require.Equal(t, []uuid.UUID{secondAttempt}, lease(), "two task attempts run together")
	finish(secondAttempt)

	// The user's own checkout and agent run do not lease beside task work.
	manualCheckout := checkout("")
	manual := insert("checkout_issue", nil, &manualCheckout)
	require.Empty(t, lease(), "a manual checkout waits for the task attempt")
	finish(manual)
	manualAgent := manualRun()
	manualAttempt := insert("run_agent", &manualAgent, nil)
	require.Empty(t, lease(), "a manual agent run waits for the task attempt")
	finish(manualAttempt)

	// Nor does task work lease beside the user's own work.
	finish(attempt)
	manualAttempt = insert("run_agent", &manualAgent, nil)
	require.Equal(t, []uuid.UUID{manualAttempt}, lease())
	taskCheckoutOp := insert("checkout_issue", nil, &taskCheckout)
	require.Empty(t, lease(), "a task checkout waits for the user's own agent run")
	finish(manualAttempt)
	require.Equal(t, []uuid.UUID{taskCheckoutOp}, lease())
	manual = insert("checkout_issue", nil, &manualCheckout)
	require.Empty(t, lease(), "a manual checkout waits for a task checkout")
	finish(taskCheckoutOp, manual)

	attempt = insert("run_agent", &otherRun, nil)
	require.Equal(t, []uuid.UUID{attempt}, lease())
	insert("start_preview", nil, nil)
	require.Empty(t, lease(), "other machine work waits while attempts run")
}
