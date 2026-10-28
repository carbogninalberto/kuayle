package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func idleMachineIDs(machines []domain.DevMachine) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(machines))
	for _, machine := range machines {
		ids = append(ids, machine.ID)
	}
	return ids
}

// Machines that agent tasks use stay running while the creator's and the
// workspace's running machines are within the policy caps. Beyond a cap, the
// least recently active idle task machines are paused like any other idle
// machine; machines no task uses keep the idle pause.
func TestIdlePauseKeepsTaskMachinesRunningWithinTheCaps(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	ws, user := fixture.workspaceID, fixture.userID
	team, issue := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Idle','IDL')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'IDL-1','Idle',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	chat, err := NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	tasks := NewAgentTaskRepository(db)
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET idle_pause_minutes=30,max_machines_per_user=4,max_concurrent_machines=4 WHERE workspace_id=$1`, ws)
	require.NoError(t, err)

	// idleMachine is running and was last active the given minutes ago.
	idleMachine := func(minutes int, task bool) uuid.UUID {
		t.Helper()
		id := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
		_, err := db.Exec(`UPDATE dev_machines SET last_activity_at=now()-make_interval(mins => $2) WHERE id=$1`, id, minutes)
		require.NoError(t, err)
		if task {
			created, err := tasks.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE agent_tasks SET machine_id=$2,state='completed' WHERE id=$1`, created.ID, id)
			require.NoError(t, err)
		}
		return id
	}
	recentTask := idleMachine(60, true)
	olderTask := idleMachine(120, true)
	manual := idleMachine(90, false)
	idleMachine(5, false) // recently active: never idle

	idle, err := fixture.repository.ListIdleMachines(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{manual}, idleMachineIDs(idle), "within the caps only machines no task uses are paused")

	// The admin lowers the caps below the running machines: the least
	// recently active task machines beyond the cap are paused, oldest first.
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET max_machines_per_user=3 WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	idle, err = fixture.repository.ListIdleMachines(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{olderTask, manual}, idleMachineIDs(idle))
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET max_machines_per_user=20,max_concurrent_machines=1 WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	idle, err = fixture.repository.ListIdleMachines(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{olderTask, manual, recentTask}, idleMachineIDs(idle))

	// keep_running is still honored beyond the cap.
	_, err = db.Exec(`UPDATE dev_machines SET keep_running=true WHERE id=$1`, olderTask)
	require.NoError(t, err)
	idle, err = fixture.repository.ListIdleMachines(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{manual, recentTask}, idleMachineIDs(idle))
}

// Under host capacity pressure the least recently active idle machine, task
// machine or not, is chosen to stop, one at a time; machines in use, kept
// running, recently active or already changing state are never chosen.
func TestReclaimIdleMachineChoosesTheLeastRecentlyActiveIdleMachine(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	machine := func(status domain.DevMachineStatus, minutes int) uuid.UUID {
		t.Helper()
		id := fixture.insertMachine(t, status, status, false)
		_, err := db.Exec(`UPDATE dev_machines SET last_activity_at=now()-make_interval(mins => $2) WHERE id=$1`, id, minutes)
		require.NoError(t, err)
		return id
	}
	waiting := machine(domain.DevMachineStatusQueued, 0)
	machine(domain.DevMachineStatusRunning, 2) // recently active
	kept := machine(domain.DevMachineStatusRunning, 600)
	_, err := db.Exec(`UPDATE dev_machines SET keep_running=true WHERE id=$1`, kept)
	require.NoError(t, err)
	busy := machine(domain.DevMachineStatusRunning, 500)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,'codex','autonomous','Test','["codex"]',60,'running')`, uuid.New(), fixture.workspaceID, busy)
	require.NoError(t, err)
	paused := machine(domain.DevMachineStatusPaused, 300)
	running := machine(domain.DevMachineStatusRunning, 200)

	candidate, err := fixture.repository.ReclaimIdleMachine(context.Background(), waiting)
	require.NoError(t, err)
	require.NotNil(t, candidate)
	require.Equal(t, paused, candidate.ID)

	// While one reclaim stop is in flight no other machine is chosen.
	operation := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: fixture.workspaceID, MachineID: paused, Action: domain.DevMachineOpStop,
		Status: domain.DevMachineOpStatusPending, Generation: 2, IdempotencyKey: "host-capacity:2", MaxAttempts: 3}
	require.NoError(t, fixture.repository.SetDesiredAndEnqueue(context.Background(), fixture.workspaceID, paused, domain.DevMachineStatusStopped, operation))
	candidate, err = fixture.repository.ReclaimIdleMachine(context.Background(), waiting)
	require.NoError(t, err)
	require.Nil(t, candidate)

	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE id=$1`, operation.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET status='stopped' WHERE id=$1`, paused)
	require.NoError(t, err)
	candidate, err = fixture.repository.ReclaimIdleMachine(context.Background(), waiting)
	require.NoError(t, err)
	require.NotNil(t, candidate)
	require.Equal(t, running, candidate.ID)
	candidate, err = fixture.repository.ReclaimIdleMachine(context.Background(), running)
	require.NoError(t, err)
	require.Nil(t, candidate, "the machine waiting for capacity is never chosen")
}
