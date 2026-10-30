package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAgentTasksDurableTransitionsAndFencing(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	owner, user, outsider, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, user, outsider} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Task test','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Task test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{owner, user, outsider} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, id)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Private task','TSK',true)`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id) VALUES($1,$2,$3,1,'TSK-1','Task issue',$4,'todo',(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	other := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: outsider})
	chats := NewConversationRepository(db)
	c, err := chats.OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	input := domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement and verify the issue", Provider: "codex", Model: "deepseek-flash", Thinking: "high"}
	_, err = repo.Create(other, ws, c.ID, input)
	require.ErrorIs(t, err, ErrConversationNotFound)
	// Concurrent duplicate clicks commit one task and one initial event.
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 2)
	for range 2 {
		wg.Go(func() {
			task, e := repo.Create(ctx, ws, c.ID, input)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- task.ID
		})
	}
	wg.Wait()
	close(ids)
	var id uuid.UUID
	for got := range ids {
		if id != uuid.Nil {
			require.Equal(t, id, got)
		}
		id = got
	}
	require.NotEqual(t, uuid.Nil, id)
	changed := input
	changed.Prompt = "different action"
	_, err = repo.Create(ctx, ws, c.ID, changed)
	require.ErrorIs(t, err, ErrConversationConflict)
	task, err := repo.Get(ctx, ws, id)
	require.NoError(t, err)
	require.Equal(t, "queued", task.State)
	var issueStatus string
	require.NoError(t, db.Get(&issueStatus, `SELECT status FROM issues WHERE id=$1`, issue))
	require.Equal(t, "todo", issueStatus)
	_, err = repo.Get(other, ws, id)
	require.ErrorIs(t, err, ErrConversationNotFound)
	attempt, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.Equal(t, id, attempt.ID)
	next, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.Nil(t, next)
	require.ErrorIs(t, repo.Transition(ctx, *attempt, "completed", "invalid shortcut"), ErrAgentTaskConflict)
	require.NoError(t, repo.Transition(ctx, *attempt, "preparing", "Resolving repository and environment"))
	// A crash releases the lease only by expiry. The successor fences the old worker.
	_, err = db.Exec(`UPDATE agent_tasks SET lease_expires_at=now()-interval '1 second',next_poll_at=now() WHERE id=$1`, id)
	require.NoError(t, err)
	successor, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, successor)
	require.NotEqual(t, attempt.LeaseToken, successor.LeaseToken)
	runtimeRepo := NewDevMachineRepository(db)
	stale := domain.WithTaskExecution(ctx, *attempt)
	deniedMachine := &domain.DevMachine{ID: attempt.ReservedMachineID, WorkspaceID: ws, IssueID: &issue, CreatedByUserID: &user}
	require.ErrorIs(t, runtimeRepo.CreateBundle(stale, deniedMachine, nil, nil, nil, nil, nil, &domain.DevMachineOperation{}), ErrAgentTaskConflict)
	fenced := domain.WithTaskExecution(ctx, *successor)
	deniedMachine.ID = uuid.New()
	require.ErrorIs(t, runtimeRepo.CreateBundle(fenced, deniedMachine, nil, nil, nil, nil, nil, &domain.DevMachineOperation{}), ErrAgentTaskConflict)
	var machines int
	require.NoError(t, db.Get(&machines, `SELECT count(*) FROM dev_machines WHERE workspace_id=$1`, ws))
	require.Zero(t, machines)

	require.ErrorIs(t, repo.Transition(ctx, *attempt, "running", "stale worker"), ErrAgentTaskConflict)
	require.NoError(t, repo.Transition(ctx, *successor, "running", "Native harness started"))
	require.NoError(t, repo.RequestCancel(ctx, ws, id))
	require.NoError(t, repo.RequestCancel(ctx, ws, id))
	require.ErrorIs(t, repo.Transition(ctx, *successor, "validating", "late result"), ErrAgentTaskConflict)
	cancelled, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cancelled)
	require.Equal(t, "cancelling", cancelled.State)
	require.NoError(t, repo.Transition(ctx, *cancelled, "cancelled", "Runtime cancellation confirmed"))
	history, err := repo.Transitions(ctx, ws, id)
	require.NoError(t, err)
	require.Len(t, history, 5)
	for i := 1; i < len(history); i++ {
		require.Greater(t, history[i].Sequence, history[i-1].Sequence)
		require.False(t, history[i].CreatedAt.Before(history[i-1].CreatedAt))
	}
	events, err := chats.Events(ctx, ws, c.ID, 0)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, "task.state_changed", events[4].Type)
	// Revoked access still permits the worker to claim cleanup, never new execution.
	input.RequestID = uuid.New()
	revoked, err := repo.Create(ctx, ws, c.ID, input)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, user)
	require.NoError(t, err)
	_, err = repo.Get(ctx, ws, revoked.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	cleanup, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.Equal(t, revoked.ID, cleanup.ID)
	require.ErrorIs(t, repo.Transition(ctx, *cleanup, "preparing", "must not run"), ErrConversationNotFound)
	movedTeam := uuid.New()
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Moved','MOV')`, movedTeam, ws)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE teams SET is_private=false WHERE id=$1`, team)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET team_id=$2 WHERE id=$1`, issue, movedTeam)
	require.NoError(t, err)
	require.NoError(t, repo.Revoke(context.Background(), *cleanup))
	_, err = db.Exec(`DELETE FROM issues WHERE id=$1`, issue)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, revoked.ID)
	require.NoError(t, err)
	orphan, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, orphan)
	require.Nil(t, orphan.ConversationID)
	require.Equal(t, "cancelling", orphan.State)
	require.NoError(t, repo.FinishCancellation(context.Background(), *orphan, "No live attempt remains"))
	deadline, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	next, err = repo.Claim(deadline)
	require.NoError(t, err)
	require.Nil(t, next)
}

func TestAgentTasksReserveMachinesAndFenceActualDispatch(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	ws, user := fixture.workspaceID, fixture.userID
	team, issue := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task','RUN')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'RUN-1','Dispatch',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	c, err := NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_providers(machine_id,provider_id,display_name,image_ref) VALUES($1,'codex','Codex','test.invalid/codex:fixture')`, machine)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	tasks := make([]domain.AgentTask, 2)
	for i := range tasks {
		_, err = repo.Create(ctx, ws, c.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement issue", Provider: "codex"})
		require.NoError(t, err)
		claimed, e := repo.Claim(ctx)
		require.NoError(t, e)
		require.NotNil(t, claimed)
		require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Resolve runtime"))
		tasks[i] = *claimed
	}
	// The workspace reservation prevents both tasks choosing the same idle runtime.
	chosen := make(chan uuid.UUID, 2)
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Go(func() {
			id, _, e := repo.ReserveMachine(ctx, task, uuid.New(), &fixture.environmentID)
			if e != nil {
				t.Error(e)
				return
			}
			chosen <- id
		})
	}
	wg.Wait()
	close(chosen)
	reused := 0
	for id := range chosen {
		if id == machine {
			reused++
		}
	}
	require.Equal(t, 1, reused)
	var task domain.AgentTask
	require.NoError(t, db.Get(&task, `SELECT * FROM agent_tasks WHERE machine_id=$1`, machine))
	dispatch := domain.WithTaskExecution(ctx, task)
	run := &domain.DevMachineAgentRun{ID: task.RunID, WorkspaceID: ws, MachineID: machine, IssueID: &issue, RequestedByUserID: &user, ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, Prompt: "Implement issue", AcceptanceCriteria: []byte(`[]`), AllowedCommands: []byte(`[]`), ForbiddenPaths: []byte(`[]`), AllowedSecrets: []byte(`[]`), CommandArgv: []byte(`["codex","exec","fixture"]`), MaxRuntimeSeconds: 60}
	op := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: ws, MachineID: machine, Action: domain.DevMachineOpRunAgent, Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "run-agent:" + run.ID.String(), RequestedByUserID: &user, MaxAttempts: 3}
	require.NoError(t, fixture.repository.CreateAgentRun(dispatch, run, op))
	_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, user)
	require.NoError(t, err)
	require.Error(t, fixture.repository.UpdateAgentRunStarted(context.Background(), run.ID), "a queued task must recheck permission when the native manager starts it")
	var nativeStatus string
	require.NoError(t, db.Get(&nativeStatus, `SELECT status FROM dev_machine_agent_runs WHERE id=$1`, run.ID))
	require.Equal(t, "queued", nativeStatus)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, user)
	require.NoError(t, err)
	require.NoError(t, fixture.repository.UpdateAgentRunStarted(context.Background(), run.ID))

	service := &domain.DevMachineService{ID: uuid.New(), WorkspaceID: ws, MachineID: machine, AgentRunID: &run.ID, ServiceType: "agent", ServiceKey: "agent-" + run.ID.String(), ContainerName: "test-agent", ImageRef: "test", InternalHost: "agent", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}
	require.NoError(t, fixture.repository.CreateRuntimeService(ctx, service))
	chunk := &domain.DevMachineLogChunk{WorkspaceID: ws, MachineID: machine, AgentRunID: &run.ID, ServiceID: &service.ID, Stream: "stdout", Sequence: 1, Content: "working\n"}
	events := []domain.DevMachineEvent{{EventType: "tool.started", Payload: []byte(`{"name":"test"}`)}}
	require.NoError(t, fixture.repository.CreateAgentOutput(ctx, chunk, events))
	require.NoError(t, fixture.repository.CreateAgentOutput(ctx, chunk, events))
	changedChunk := *chunk
	changedChunk.Content = "different replay"
	require.ErrorIs(t, fixture.repository.CreateAgentOutput(ctx, &changedChunk, events), ErrAgentOutputConflict)
	rotatedChunk := *chunk
	rotatedChunk.SourceKey = "2026-10-26T20:00:01.000000001Z"
	require.ErrorIs(t, fixture.repository.CreateAgentOutput(ctx, &rotatedChunk, events), ErrAgentOutputConflict, "identical text from a rotated prefix is not an old record")
	var activityCount int
	require.NoError(t, db.Get(&activityCount, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.activity'`, c.ID))
	require.Equal(t, 1, activityCount, "native output must be in the conversation before execution completes, exactly once")
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_events WHERE agent_run_id=$1 AND event_type='tool.started'`, run.ID))
	require.Equal(t, 1, count)
	chunk.Sequence = 2
	require.Error(t, fixture.repository.CreateAgentOutput(ctx, chunk, []domain.DevMachineEvent{{EventType: "broken", Payload: []byte(`not-json`)}}))
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_log_chunks WHERE agent_run_id=$1`, run.ID))
	require.Equal(t, 1, count, "event failure rolls back the log deduplication key too")
	require.NoError(t, fixture.repository.CreateAgentOutput(ctx, chunk, events))
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_events WHERE agent_run_id=$1 AND event_type='tool.started'`, run.ID))
	require.Equal(t, 2, count)

	require.NoError(t, repo.RequestCancel(ctx, ws, task.ID))
	chunk.Sequence = 3
	require.ErrorIs(t, fixture.repository.CreateAgentOutput(ctx, chunk, events), ErrAgentTaskUnauthorized, "cancelled task cannot persist late native activity")
	require.ErrorIs(t, fixture.repository.CreateAgentRun(dispatch, run, op), ErrAgentTaskConflict)
	cancelled, e := repo.Claim(ctx)
	require.NoError(t, e)
	require.NotNil(t, cancelled)
	require.Equal(t, task.ID, cancelled.ID)
	require.ErrorIs(t, repo.FinishCancellation(ctx, *cancelled, "Cleanup finished"), ErrAgentTaskConflict)
	// The native run's cancelled flag alone does not prove its container has stopped.
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='cancelled' WHERE id=$1`, run.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.FinishCancellation(ctx, *cancelled, "Cleanup finished"), ErrAgentTaskConflict)
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE id=$1`, op.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.FinishCancellation(ctx, *cancelled, "Runtime cleanup confirmed"), ErrAgentTaskConflict, "a terminal flag is not proof of cancellation cleanup")
	require.NoError(t, repo.CancelAttempt(ctx, *cancelled))
	require.NoError(t, repo.CancelAttempt(ctx, *cancelled))
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='failed',attempts=5 WHERE agent_run_id=$1 AND action='cancel_agent'`, run.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.CancelAttempt(ctx, *cancelled), ErrAgentTaskCleanupFailed)
	require.NoError(t, repo.RequestCancel(ctx, ws, task.ID))
	require.NoError(t, repo.CancelAttempt(ctx, *cancelled))

	require.ErrorIs(t, repo.FinishCancellation(ctx, *cancelled, "Runtime cleanup confirmed"), ErrAgentTaskConflict)
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE agent_run_id=$1 AND action='cancel_agent'`, run.ID)
	require.NoError(t, err)
	require.NoError(t, repo.FinishCancellation(ctx, *cancelled, "Runtime cleanup confirmed"))
	var runs, operations int
	require.NoError(t, db.Get(&runs, `SELECT count(*) FROM dev_machine_agent_runs WHERE id=$1`, run.ID))
	require.Equal(t, 1, runs)
	require.NoError(t, db.Get(&operations, `SELECT count(*) FROM dev_machine_operations WHERE agent_run_id=$1`, run.ID))
	require.Equal(t, 2, operations)
}

func TestAgentRunSecretSnapshotIsImmutableAndRejectsUnsafeRecovery(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	runID := uuid.New()
	_, err := db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds) VALUES($1,$2,$3,'codex','autonomous','Test','["codex"]',60)`, runID, fixture.workspaceID, machine)
	require.NoError(t, err)
	ctx := context.Background()
	repo := fixture.repository
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, candidate := range []string{"opaque-first-ciphertext", "opaque-second-ciphertext"} {
		wg.Go(func() {
			sealed, err := repo.SealAgentRunSecrets(ctx, runID, candidate)
			if err != nil {
				t.Error(err)
				return
			}
			results <- sealed
		})
	}
	wg.Wait()
	close(results)
	var original string
	for value := range results {
		if original == "" {
			original = value
		}
		require.Equal(t, original, value)
	}
	require.NotEmpty(t, original)
	snapshots, err := repo.ListAgentRunSecretSnapshots(ctx, fixture.workspaceID, machine)
	require.NoError(t, err)
	require.Equal(t, []string{original}, snapshots)
	snapshots, err = repo.ListAgentRunSecretSnapshots(ctx, uuid.New(), machine)
	require.NoError(t, err)
	require.Empty(t, snapshots)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='running' WHERE id=$1`, runID)
	require.NoError(t, err)
	recovered, err := repo.SealAgentRunSecrets(ctx, runID, "opaque-rotated-ciphertext")
	require.NoError(t, err)
	require.Equal(t, original, recovered)
	// An active legacy process could be using any prior credential. Never guess.
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET encrypted_output_secrets=NULL WHERE id=$1`, runID)
	require.NoError(t, err)
	_, err = repo.SealAgentRunSecrets(ctx, runID, "opaque-new-ciphertext")
	require.ErrorIs(t, err, ErrAgentRunSecretsUnavailable)
	var missing bool
	require.NoError(t, db.Get(&missing, `SELECT encrypted_output_secrets IS NULL FROM dev_machine_agent_runs WHERE id=$1`, runID))
	require.True(t, missing)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='cancelled' WHERE id=$1`, runID)
	require.NoError(t, err)
	_, err = repo.SealAgentRunSecrets(ctx, runID, "opaque-new-ciphertext")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestReservationReusesAnIdleMachineWithADifferentAgent(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	ws, user := fixture.workspaceID, fixture.userID
	team, issue := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task','RUN')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'RUN-1','Dispatch',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	c, err := NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_providers(machine_id,provider_id,display_name,image_ref) VALUES($1,'codex','Codex','test.invalid/codex:fixture')`, machine)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	_, err = repo.Create(ctx, ws, c.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement issue", Provider: "claude-code"})
	require.NoError(t, err)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Resolve runtime"))
	claimed.State = "preparing"
	// Reserved before the machine became reusable: a placeholder that was never created.
	_, err = db.Exec(`UPDATE dev_machines SET status='paused' WHERE id=$1`, machine)
	require.NoError(t, err)
	placeholder, _, err := repo.ReserveMachine(ctx, *claimed, uuid.New(), &fixture.environmentID)
	require.NoError(t, err)
	require.Equal(t, claimed.ReservedMachineID, placeholder)
	claimed.MachineID = &placeholder
	_, err = db.Exec(`UPDATE dev_machines SET status='running' WHERE id=$1`, machine)
	require.NoError(t, err)
	id, _, err := repo.ReserveMachine(ctx, *claimed, uuid.New(), &fixture.environmentID)
	require.NoError(t, err)
	require.Equal(t, machine, id, "an idle machine is reused even when it lacks this task's agent")

	require.NoError(t, repo.NoteWaiting(ctx, *claimed, "Waiting for a free dev machine"))
	var reason string
	require.NoError(t, db.Get(&reason, `SELECT reason FROM agent_tasks WHERE id=$1`, claimed.ID))
	require.Equal(t, "Waiting for a free dev machine", reason)
}

// Tasks reuse a running, idle machine of their creator before creating a new
// one, whatever repository it served before. Other users' machines, another
// environment's, paused or stopped ones (their user chose that), busy ones and
// ones the workspace policy no longer allows are never chosen. A machine
// already used for the same issue comes first, then the most recently active.
func TestReservationReusesTheCreatorsRunningIdleMachine(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	ws, user := fixture.workspaceID, fixture.userID
	other := uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, other)
	require.NoError(t, err)
	team, issue, otherIssue := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task','RUN')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	for number, id := range []uuid.UUID{issue, otherIssue} {
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,$4,$5,'Dispatch',$6,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, id, ws, team, number+1, fmt.Sprintf("RUN-%d", number+1), user)
		require.NoError(t, err)
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	chats := NewConversationRepository(db)
	c, err := chats.OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	otherChat, err := chats.OpenIssue(ctx, ws, otherIssue)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	_, err = repo.Create(ctx, ws, c.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement issue", Provider: "codex"})
	require.NoError(t, err)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Resolve runtime"))
	claimed.State = "preparing"
	repository := uuid.New()
	reserve := func() uuid.UUID {
		t.Helper()
		_, err := db.Exec(`UPDATE agent_tasks SET machine_id=NULL WHERE id=$1`, claimed.ID)
		require.NoError(t, err)
		claimed.MachineID = nil
		id, _, err := repo.ReserveMachine(ctx, *claimed, repository, &fixture.environmentID)
		require.NoError(t, err)
		return id
	}
	running := func(activity string) uuid.UUID {
		t.Helper()
		id := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
		_, err := db.Exec(`UPDATE dev_machines SET last_activity_at=now()-$2::interval WHERE id=$1`, id, activity)
		require.NoError(t, err)
		return id
	}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}

	othersRunning := running("1 minute")
	exec(`UPDATE dev_machines SET created_by_user_id=$2 WHERE id=$1`, othersRunning, other)
	otherEnvironment := running("1 minute")
	exec(`UPDATE dev_machines SET environment_id=NULL WHERE id=$1`, otherEnvironment)
	deleting := running("1 minute")
	exec(`UPDATE dev_machines SET delete_requested_at=now() WHERE id=$1`, deleting)
	builder := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, true)
	fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, false)
	fixture.insertMachine(t, domain.DevMachineStatusPaused, domain.DevMachineStatusPaused, false)
	starting := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusRunning, false)
	require.Equal(t, claimed.ReservedMachineID, reserve(), "no running machine of the creator fits, so a new one is created")
	for _, id := range []uuid.UUID{builder, starting} {
		exec(`UPDATE dev_machines SET status='destroyed',desired_status='destroyed' WHERE id=$1`, id)
	}

	// The machine served another repository before: one machine serves several.
	older := running("3 hours")
	installation, previousRepository := uuid.New(), uuid.New()
	exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, ws, user)
	exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/previous','main')`, previousRepository, installation, ws)
	exec(`UPDATE dev_machines SET repository_affinity_id=$2 WHERE id=$1`, older, previousRepository)
	require.Equal(t, older, reserve())
	recent := running("1 hour")
	require.Equal(t, recent, reserve(), "the most recently active machine is preferred")

	// A machine already used for this issue (an earlier, finished task) wins
	// over a more recently active one.
	earlier, err := repo.Create(ctx, ws, c.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Earlier attempt", Provider: "codex"})
	require.NoError(t, err)
	exec(`UPDATE agent_tasks SET machine_id=$2,state='completed' WHERE id=$1`, earlier.ID, older)
	require.Equal(t, older, reserve(), "the issue's previous machine is preferred")
	exec(`UPDATE agent_tasks SET machine_id=NULL WHERE id=$1`, earlier.ID)

	// Busy machines are skipped: an active run, another active task, or a
	// queued validation, publication, check or tool run.
	busyRun := uuid.New()
	exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,'codex','autonomous','Busy','["codex"]',60,'running')`, busyRun, ws, recent)
	require.Equal(t, older, reserve(), "a machine with a running agent is busy")
	exec(`UPDATE dev_machine_agent_runs SET status='succeeded' WHERE id=$1`, busyRun)
	require.Equal(t, recent, reserve())
	for _, action := range []string{"validate_agent", "publish_agent", "run_check", "run_tool"} {
		exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,$3,'pending',1,$4)`, ws, recent, action, "busy-"+action)
		require.Equal(t, older, reserve(), action+" holds the machine")
		exec(`UPDATE dev_machine_operations SET status='completed' WHERE machine_id=$1 AND idempotency_key=$2`, recent, "busy-"+action)
	}
	require.Equal(t, recent, reserve())
	holder, err := repo.Create(ctx, ws, otherChat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Other work", Provider: "codex"})
	require.NoError(t, err)
	exec(`UPDATE agent_tasks SET machine_id=$2,state='running' WHERE id=$1`, holder.ID, recent)
	require.Equal(t, older, reserve(), "another task's machine is not shared")
	// Once that task awaits review its machine is free again; its checkout
	// stays separate from this task's.
	exec(`UPDATE agent_tasks SET state='awaiting_review' WHERE id=$1`, holder.ID)
	require.Equal(t, recent, reserve())

	// The workspace policy must still allow the machine.
	exec(`UPDATE dev_machines SET sudo_enabled=true WHERE id=$1`, recent)
	require.Equal(t, older, reserve(), "a sudo machine is not reused once sudo is disallowed")
	exec(`UPDATE dev_machine_workspace_policies SET allow_sudo=true WHERE workspace_id=$1`, ws)
	require.Equal(t, recent, reserve())
	exec(`INSERT INTO dev_machine_services(workspace_id,machine_id,service_key,service_type,container_name,image_ref,internal_host,internal_port) VALUES($1,$2,'docker','docker','fixture-docker','docker:fixture','docker',2376)`, ws, recent)
	require.Equal(t, older, reserve(), "a Docker machine is not reused once Docker is disallowed")
	exec(`UPDATE dev_machine_workspace_policies SET allow_docker=true WHERE workspace_id=$1`, ws)
	require.Equal(t, recent, reserve())
	exec(`UPDATE dev_machine_workspace_policies SET enabled=false WHERE workspace_id=$1`, ws)
	require.Equal(t, claimed.ReservedMachineID, reserve(), "nothing is reused while Dev Machines are disabled")
	exec(`UPDATE dev_machine_workspace_policies SET enabled=true WHERE workspace_id=$1`, ws)
	paused := fixture.insertMachine(t, domain.DevMachineStatusPaused, domain.DevMachineStatusPaused, false)
	stopped := older
	exec(`UPDATE dev_machines SET status='stopped',desired_status='stopped' WHERE id=$1`, stopped)

	// A task whose machine was destroyed (records are kept as history) gets a
	// fresh reservation instead of waiting on the destroyed machine forever.
	_, err = db.Exec(`UPDATE dev_machines SET status='destroyed',desired_status='destroyed' WHERE id IN ($1,$2,$3,$4)`, paused, stopped, recent, otherEnvironment)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET machine_id=$2,reserved_machine_id=$2 WHERE id=$1`, claimed.ID, paused)
	require.NoError(t, err)
	claimed.MachineID, claimed.ReservedMachineID = &paused, paused
	fresh, _, err := repo.ReserveMachine(ctx, *claimed, repository, &fixture.environmentID)
	require.NoError(t, err)
	require.NotEqual(t, paused, fresh)
	var reserved uuid.UUID
	require.NoError(t, db.Get(&reserved, `SELECT reserved_machine_id FROM agent_tasks WHERE id=$1`, claimed.ID))
	require.Equal(t, fresh, reserved, "a new machine will be created under a new reservation")
}
