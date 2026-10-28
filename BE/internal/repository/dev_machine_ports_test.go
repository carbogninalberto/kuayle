package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func taskMachine(t *testing.T, db *sqlx.DB, task domain.AgentTask) domain.DevMachine {
	t.Helper()
	var machine domain.DevMachine
	require.NoError(t, db.Get(&machine, `SELECT m.* FROM dev_machines m JOIN agent_tasks t ON t.machine_id=m.id WHERE t.id=$1`, task.ID))
	return machine
}

func runningIDE(t *testing.T, store *DevMachineRepository, machine domain.DevMachine) *domain.DevMachineService {
	t.Helper()
	ide := &domain.DevMachineService{ID: uuid.New(), WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, ServiceType: "ide", ServiceKey: "ide", ContainerName: "ide", ImageRef: "fixture", InternalHost: "ide", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}
	require.NoError(t, store.CreateRuntimeService(context.Background(), ide))
	require.NoError(t, store.UpdateServiceRuntime(context.Background(), ide.ID, "container", "running", "healthy", nil))
	return ide
}

func appPort(machine domain.DevMachine, port int, source string, task *uuid.UUID) *domain.DevMachinePort {
	return &domain.DevMachinePort{WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, Port: port, Source: source, AgentTaskID: task, CreatedByUserID: *machine.CreatedByUserID}
}

// App ports route to the running developer service on their own port, never to
// a port of the machine's own services, and are limited per machine.
func TestAppPortsRouteToTheDeveloperServiceWithinTheirLimit(t *testing.T) {
	db, _, ctx, task := reviewableTask(t)
	store := NewDevMachineRepository(db)
	machine := taskMachine(t, db, task)
	route := func(port int) *domain.DevMachineService {
		t.Helper()
		_, service, err := store.GetRoute(ctx, machine.RoutingKey, domain.AppPortService(port))
		require.NoError(t, err)
		return service
	}
	require.Nil(t, route(5173), "nothing is exposed yet")
	ide := runningIDE(t, store, machine)
	require.NoError(t, store.CreatePort(ctx, appPort(machine, 5173, "user", nil)))
	served := route(5173)
	require.NotNil(t, served)
	require.Equal(t, ide.ID, served.ID, "tickets name the developer service")
	require.Equal(t, "port:5173", served.ServiceType)
	require.Equal(t, 5173, served.InternalPort)
	require.Equal(t, "ide", served.InternalHost)
	require.Nil(t, route(5174))
	_, ideRoute, err := store.GetRoute(ctx, machine.RoutingKey, "ide")
	require.NoError(t, err)
	require.Equal(t, 8080, ideRoute.InternalPort, "the IDE route is unchanged")

	_, err = db.Exec(`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,8080,'user',$3)`, machine.WorkspaceID, machine.ID, *machine.CreatedByUserID)
	require.NoError(t, err)
	require.Nil(t, route(8080), "a service port is never routed, even when stored")

	require.ErrorIs(t, store.CreatePort(ctx, appPort(machine, 5173, "agent", nil)), ErrPortExists)
	for port := 3000; port < 3008; port++ {
		require.NoError(t, store.CreatePort(ctx, appPort(machine, port, "agent", nil)))
	}
	require.ErrorIs(t, store.CreatePort(ctx, appPort(machine, 3008, "agent", nil)), ErrPortLimit)
	foreign := uuid.New()
	other := appPort(machine, 3009, "agent", &foreign)
	_, err = db.Exec(`DELETE FROM dev_machine_ports WHERE machine_id=$1 AND port=3007`, machine.ID)
	require.NoError(t, err)
	require.Error(t, store.CreatePort(ctx, other), "a port names only a task on this machine")

	removed, err := store.DeleteAgentPort(ctx, machine.ID, 5173)
	require.NoError(t, err)
	require.False(t, removed, "agents cannot remove the owner's ports")
	removed, err = store.DeleteAgentPort(ctx, machine.ID, 3000)
	require.NoError(t, err)
	require.True(t, removed)
}

// Removing a port revokes only the browser sessions of that port's host; the
// task identifier is shown only to the task's creator.
func TestDeletingAnAppPortRevokesOnlyItsHostSessions(t *testing.T) {
	db, _, ctx, task := reviewableTask(t)
	store := NewDevMachineRepository(db)
	machine := taskMachine(t, db, task)
	ide := runningIDE(t, store, machine)
	port := appPort(machine, 5173, "task", &task.ID)
	require.NoError(t, store.CreatePort(ctx, port))
	session := func(host string) uuid.UUID {
		id := uuid.New()
		_, err := db.Exec(`INSERT INTO dev_machine_access_sessions(id,workspace_id,machine_id,service_id,user_id,token_hash,bound_host,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()+interval '1 hour')`,
			id, machine.WorkspaceID, machine.ID, ide.ID, *machine.CreatedByUserID, strings.Repeat(strings.ReplaceAll(id.String(), "-", ""), 2), host)
		require.NoError(t, err)
		return id
	}
	portSession := session(machine.RoutingKey + "-5173.machines.example.test")
	otherPort := session(machine.RoutingKey + "-5174.machines.example.test")
	ideSession := session(machine.RoutingKey + ".machines.example.test")

	ports, err := store.ListPorts(ctx, machine.WorkspaceID, machine.ID)
	require.NoError(t, err)
	require.Len(t, ports, 1)
	require.Equal(t, "REC-1", *ports[0].TaskIdentifier)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: machine.WorkspaceID, UserID: uuid.New()})
	ports, err = store.ListPorts(stranger, machine.WorkspaceID, machine.ID)
	require.NoError(t, err)
	require.Nil(t, ports[0].TaskIdentifier)
	other, err := store.ListPorts(ctx, uuid.New(), machine.ID)
	require.NoError(t, err)
	require.Empty(t, other, "ports are workspace scoped")

	deleted, err := store.DeletePort(ctx, uuid.New(), machine.ID, port.ID, task.CreatorID)
	require.NoError(t, err)
	require.Nil(t, deleted, "another workspace cannot remove it")
	deleted, err = store.DeletePort(ctx, machine.WorkspaceID, machine.ID, port.ID, task.CreatorID)
	require.NoError(t, err)
	require.Equal(t, 5173, deleted.Port)
	revoked := func(id uuid.UUID) bool {
		var value bool
		require.NoError(t, db.Get(&value, `SELECT revoked_at IS NOT NULL FROM dev_machine_access_sessions WHERE id=$1`, id))
		return value
	}
	require.True(t, revoked(portSession))
	require.False(t, revoked(otherPort))
	require.False(t, revoked(ideSession))
	var stops int
	require.NoError(t, db.Get(&stops, `SELECT count(*) FROM dev_machine_operations WHERE action='stop_preview' AND agent_task_id=$1`, task.ID))
	require.Equal(t, 1, stops, "removing a task's port stops its preview")
}

func previewTask(t *testing.T, db *sqlx.DB, id uuid.UUID) domain.AgentTask {
	t.Helper()
	var task domain.AgentTask
	require.NoError(t, db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, id))
	return task
}

func previewOperations(t *testing.T, db *sqlx.DB, task uuid.UUID, action string) int {
	t.Helper()
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_operations WHERE action=$2::dev_machine_operation_action AND agent_task_id=$1`, task, action))
	return count
}

// A task in review queues its preview once per attempt; its creator can start
// and stop it, and the preview is part of every task payload.
func TestTaskPreviewsStartOncePerAttemptAndStopWithTheTask(t *testing.T) {
	db, repo, ctx, task, _ := publishingTask(t)
	store := NewDevMachineRepository(db)
	_, err := repo.RequestPreview(ctx, task.WorkspaceID, task.ID, true)
	require.ErrorIs(t, err, ErrPreviewUnavailable, "no preview command")
	_, err = db.Exec(`UPDATE agent_tasks SET preview_command='["npm","run","dev"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_scope_settings(id,workspace_id,issue_id,preview_command) VALUES(gen_random_uuid(),$1,$2,'["npm"]')`, task.WorkspaceID, task.IssueID)
	require.NoError(t, err, "the port is only preferred")

	current := previewTask(t, db, task.ID)
	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	current.State = "awaiting_review"
	require.NoError(t, taskPreviewTransition(ctx, tx, &current))
	require.Equal(t, "starting", *current.PreviewState)
	_, err = tx.Exec(`UPDATE agent_tasks SET preview_state='running' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	current.PreviewState = nil
	require.NoError(t, taskPreviewTransition(ctx, tx, &current))
	require.Nil(t, current.PreviewState, "the same attempt queues one preview and changes nothing")
	require.NoError(t, tx.Commit())
	require.Equal(t, 1, previewOperations(t, db, task.ID, "start_preview"))

	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.RequestPreview(stranger, task.WorkspaceID, task.ID, true)
	require.Error(t, err, "only the task's creator")
	_, err = repo.RequestPreview(ctx, task.WorkspaceID, task.ID, true)
	require.NoError(t, err)
	require.Equal(t, 2, previewOperations(t, db, task.ID, "start_preview"), "a running preview can be restarted")
	updated, err := repo.RequestPreview(ctx, task.WorkspaceID, task.ID, true)
	require.NoError(t, err)
	require.Equal(t, "starting", *updated.PreviewState)
	require.Equal(t, 2, previewOperations(t, db, task.ID, "start_preview"), "a starting preview is not queued again")

	machine := taskMachine(t, db, task)
	port := appPort(machine, 4100, "task", &task.ID)
	require.NoError(t, store.CreatePort(ctx, port))
	require.NoError(t, store.SetTaskPreview(ctx, task.ID, "running", nil, port))
	payload, err := json.Marshal(previewTask(t, db, task.ID))
	require.NoError(t, err)
	var view struct {
		Preview *domain.AgentTaskPreview `json:"preview"`
	}
	require.NoError(t, json.Unmarshal(payload, &view))
	require.Equal(t, &domain.AgentTaskPreview{State: "running", PortID: &port.ID, Port: &port.Port}, view.Preview)
	var events int
	require.NoError(t, db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.updated' AND payload->'preview'->>'state'='running'`, task.ConversationID))
	require.Equal(t, 1, events, "the chat learns about the running preview")

	_, err = repo.RequestPreview(ctx, task.WorkspaceID, task.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, previewOperations(t, db, task.ID, "stop_preview"))
	userPort := appPort(machine, 3000, "user", nil)
	require.NoError(t, store.CreatePort(ctx, userPort))
	require.NoError(t, store.StopMachinePreviews(ctx, machine.ID))
	stopped := previewTask(t, db, task.ID)
	require.Equal(t, "stopped", *stopped.PreviewState)
	require.Nil(t, stopped.PreviewPortID)
	ports, err := store.MachinePorts(ctx, machine.ID)
	require.NoError(t, err)
	require.Len(t, ports, 1, "a stopped machine keeps the owner's ports")
	require.Equal(t, 3000, ports[0].Port)
	_, err = repo.RequestPreview(ctx, task.WorkspaceID, task.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, previewOperations(t, db, task.ID, "stop_preview"), "a stopped preview needs no stop")

	// A running preview stops when its task finishes.
	require.NoError(t, store.CreatePort(ctx, port))
	require.NoError(t, store.SetTaskPreview(ctx, task.ID, "running", nil, port))
	_, err = db.Exec(`DELETE FROM dev_machine_ports WHERE id=$1`, port.ID)
	require.NoError(t, err)
	gone := previewTask(t, db, task.ID)
	require.Nil(t, gone.PreviewPortID, "removing the port clears it from the task")
	require.Nil(t, gone.PreviewAppPort)
	current = previewTask(t, db, task.ID)
	tx, err = db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	current.State = "completed"
	require.NoError(t, taskPreviewTransition(ctx, tx, &current))
	require.NoError(t, tx.Commit())
	require.Equal(t, 2, previewOperations(t, db, task.ID, "stop_preview"))
	require.NoError(t, store.StopTaskPreview(ctx, task.ID))
	require.Equal(t, "stopped", *previewTask(t, db, task.ID).PreviewState)
}

// A worktree path maps to the task whose checkout it is on that machine only.
func TestAgentRequestsMapTheirWorktreeToItsTask(t *testing.T) {
	db, _, ctx, task := reviewableTask(t)
	store := NewDevMachineRepository(db)
	machine := taskMachine(t, db, task)
	found, err := store.TaskForWorktree(ctx, machine.ID, "/workspace/tasks/rec-1")
	require.NoError(t, err)
	require.Equal(t, task.ID, *found)
	found, err = store.TaskForWorktree(ctx, uuid.New(), "/workspace/tasks/rec-1")
	require.NoError(t, err)
	require.Nil(t, found)
	require.NoError(t, store.SetListeningPorts(ctx, machine.ID, []int{3000, 5173}))
	machines, err := store.ListPortScanMachines(ctx, 10)
	require.NoError(t, err)
	var scanned *domain.DevMachine
	for i := range machines {
		if machines[i].ID == machine.ID {
			scanned = &machines[i]
		}
	}
	require.NotNil(t, scanned)
	require.Equal(t, []int64{3000, 5173}, []int64(scanned.ListeningPorts))
	require.NotNil(t, scanned.ListeningPortsAt)
}
