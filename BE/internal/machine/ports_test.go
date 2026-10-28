package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// portFakeState holds the app port data of managerStoreFake.
type portFakeState struct {
	ports          []domain.DevMachinePort
	listening      []int
	target         *repository.TaskPreviewTarget
	previewStates  []string
	previewErrors  []string
	previewPort    *domain.DevMachinePort
	stoppedTasks   []uuid.UUID
	stoppedMachine int
	worktreeTask   map[string]uuid.UUID
}

func (f *managerStoreFake) MachinePorts(context.Context, uuid.UUID) ([]domain.DevMachinePort, error) {
	return append([]domain.DevMachinePort(nil), f.appPorts.ports...), nil
}
func (f *managerStoreFake) CreatePort(_ context.Context, port *domain.DevMachinePort) error {
	for _, existing := range f.appPorts.ports {
		if existing.Port == port.Port {
			return repository.ErrPortExists
		}
	}
	if len(f.appPorts.ports) >= domain.DevMachinePortLimit {
		return repository.ErrPortLimit
	}
	port.ID = uuid.New()
	f.appPorts.ports = append(f.appPorts.ports, *port)
	return nil
}
func (f *managerStoreFake) DeleteAgentPort(_ context.Context, _ uuid.UUID, number int) (bool, error) {
	for i, port := range f.appPorts.ports {
		if port.Port == number && port.Source == domain.DevMachinePortSourceAgent {
			f.appPorts.ports = append(f.appPorts.ports[:i], f.appPorts.ports[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (f *managerStoreFake) SetListeningPorts(_ context.Context, _ uuid.UUID, ports []int) error {
	f.appPorts.listening = ports
	return nil
}
func (f *managerStoreFake) ListPortScanMachines(context.Context, int) ([]domain.DevMachine, error) {
	if f.machine == nil {
		return nil, nil
	}
	return []domain.DevMachine{*f.machine}, nil
}
func (f *managerStoreFake) TaskForWorktree(_ context.Context, _ uuid.UUID, path string) (*uuid.UUID, error) {
	if id, ok := f.appPorts.worktreeTask[path]; ok {
		return &id, nil
	}
	return nil, nil
}
func (f *managerStoreFake) PreviewTarget(context.Context, uuid.UUID) (*repository.TaskPreviewTarget, error) {
	return f.appPorts.target, nil
}
func (f *managerStoreFake) SetTaskPreview(_ context.Context, _ uuid.UUID, state string, message *string, port *domain.DevMachinePort) error {
	f.appPorts.previewStates = append(f.appPorts.previewStates, state)
	if message != nil {
		f.appPorts.previewErrors = append(f.appPorts.previewErrors, *message)
	}
	f.appPorts.previewPort = port
	return nil
}
func (f *managerStoreFake) StopTaskPreview(_ context.Context, taskID uuid.UUID) error {
	f.appPorts.stoppedTasks = append(f.appPorts.stoppedTasks, taskID)
	kept := f.appPorts.ports[:0]
	for _, port := range f.appPorts.ports {
		if port.Source != domain.DevMachinePortSourceTask || port.AgentTaskID == nil || *port.AgentTaskID != taskID {
			kept = append(kept, port)
		}
	}
	f.appPorts.ports = kept
	return nil
}
func (f *managerStoreFake) StopMachinePreviews(context.Context, uuid.UUID) error {
	f.appPorts.stoppedMachine++
	return nil
}

type portRuntimeFake struct {
	*runtimeFake
	scan     PortScan
	scanErr  error
	states   [][]byte
	started  []startedPreview
	stopped  []uuid.UUID
	startErr error
}

type startedPreview struct {
	task uuid.UUID
	argv []string
	port int
	cwd  string
}

func (f *portRuntimeFake) ScanPorts(context.Context, []domain.DevMachineService) (PortScan, error) {
	scan := f.scan
	f.scan.Requests = nil
	return scan, f.scanErr
}
func (f *portRuntimeFake) WritePortState(_ context.Context, _ []domain.DevMachineService, state []byte) error {
	f.states = append(f.states, state)
	return nil
}
func (f *portRuntimeFake) StartPreview(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, checkout *domain.DevMachineCheckout, task uuid.UUID, argv []string, port int) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, startedPreview{task, argv, port, checkout.WorkspacePath})
	return nil
}
func (f *portRuntimeFake) StopPreview(_ context.Context, _ []domain.DevMachineService, task uuid.UUID) error {
	f.stopped = append(f.stopped, task)
	return nil
}

func portMachine() (*domain.DevMachine, []domain.DevMachineService) {
	creator := uuid.New()
	container := "developer"
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), CreatedByUserID: &creator, RoutingKey: "0123456789abcdef0123",
		Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)}
	services := []domain.DevMachineService{
		{ID: uuid.New(), MachineID: machine.ID, ServiceType: "ide", Status: "running", InternalPort: 8080, ContainerID: &container},
		{ID: uuid.New(), MachineID: machine.ID, ServiceType: "terminal", Status: "running", InternalPort: 7681, ContainerID: &container},
	}
	return machine, services
}

func newPortManager() (*Manager, *managerStoreFake, *portRuntimeFake) {
	machine, services := portMachine()
	store := &managerStoreFake{machine: machine, services: services}
	runtime := &portRuntimeFake{runtimeFake: &runtimeFake{}}
	manager := &Manager{store: store, runtime: runtime}
	manager.SetMachineDomain("machines.example.test")
	return manager, store, runtime
}

func request(name, body string) PortRequest { return PortRequest{Name: name, Body: []byte(body)} }

func TestParsePortScanReadsOnlyListeningSockets(t *testing.T) {
	output := strings.Join([]string{
		"   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1 1 0000000000000000 100 0 0 10 0",
		"   1: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 2 1 0000000000000000 100 0 0 10 0",
		"   2: 0100007F:0BB8 0100007F:A1B2 01 00000000:00000000 00:00000000 00000000  1000        0 3 1 0000000000000000 100 0 0 10 0",
		"   0: 00000000000000000000000000000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 4",
		"   3: 0B00007F:89FD 00000000:0000 0A 00000000:00000000 00:00000000 00000000  0 0 5",
		"   4: 00000000000000000000000001000000:1389 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 6",
		"   5: 0000000000000000FFFF00000100007F:1771 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 7",
		"   6: 00000000:1389 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 8",
		"   9: garbage line",
		"R expose-3000.json " + "eyJwb3J0IjozMDAwfQ==",
		"R unexpose-5173 ",
		"R expose-4000.json !!!not-base64",
	}, "\n")
	scan := parsePortScan(output)
	// Loopback listeners (127.0.0.0/8, ::1, ::ffff:127.x) cannot be reached
	// through the gateway: 3000 and 6001 are dropped, 5001 also listens on
	// all addresses.
	require.Equal(t, []int{5001, 5173, 8080}, scan.Listening, "reachable LISTEN sockets, deduplicated and sorted")
	require.Equal(t, []PortRequest{request("expose-3000.json", `{"port":3000}`), request("unexpose-5173", "")}, scan.Requests)
}

func TestPortRequestsAreValidatedStrictly(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       portAction
		err        string
	}{
		{"expose-3000.json", `{"port":3000,"label":"web app"}`, portAction{expose: true, port: 3000, label: strPtr("web app")}, ""},
		{"expose-3000.json", `{"port":3000,"cwd":"/workspace/tasks/eng-1/web"}`, portAction{expose: true, port: 3000, worktree: "/workspace/tasks/eng-1"}, ""},
		{"expose-3000.json", `{"port":3000,"cwd":"/home/kuayle"}`, portAction{expose: true, port: 3000}, ""},
		{"unexpose-3000", ``, portAction{port: 3000}, ""},
		{"expose-3000.json", `{"port":3001}`, portAction{}, "does not match"},
		{"expose-0300.json", `{"port":300}`, portAction{}, "invalid request"},
		{"expose-80.json", `{"port":80}`, portAction{}, "invalid request"},
		{"expose-3000.json", `{"port":3000,"label":"<script>"}`, portAction{}, "label"},
		{"expose-3000.json", `{"port":3000,"extra":1}`, portAction{}, "invalid JSON"},
		{"expose-3000.json", `{"port":3000}{"port":3000}`, portAction{}, "invalid JSON"},
		{"expose-3000.json", strings.Repeat(" ", 2000) + `{"port":3000}`, portAction{}, "too large"},
		{"rm-rf", ``, portAction{}, "invalid request"},
	} {
		action, err := parsePortRequest(request(test.name, test.body))
		if test.err != "" {
			require.ErrorContains(t, err, test.err, test.name+" "+test.body)
			continue
		}
		require.NoError(t, err, test.name)
		require.Equal(t, test.want, action, test.name)
	}
}

func strPtr(value string) *string { return &value }

func portState(t *testing.T, runtime *portRuntimeFake) agentPortState {
	t.Helper()
	require.NotEmpty(t, runtime.states)
	var state agentPortState
	require.NoError(t, json.Unmarshal(runtime.states[len(runtime.states)-1], &state))
	return state
}

// Agent requests become agent ports owned by the machine creator, never reach
// service ports or exceed the limit, and the agent sees the result.
func TestPortScanAppliesAgentRequestsAndPublishesTheState(t *testing.T) {
	manager, store, runtime := newPortManager()
	task := uuid.New()
	store.appPorts.worktreeTask = map[string]uuid.UUID{"/workspace/tasks/eng-1": task}
	store.appPorts.ports = []domain.DevMachinePort{{ID: uuid.New(), Port: 5173, Source: "user"}}
	runtime.scan = PortScan{Listening: []int{3000, 7681, 8080, 5173}, Requests: []PortRequest{
		request("expose-3000.json", `{"port":3000,"label":"web","cwd":"/workspace/tasks/eng-1"}`),
		request("expose-8080.json", `{"port":8080}`),
		request("unexpose-5173", ``),
		request("expose-4000.json", `{"port":4001}`),
	}}
	ctx := context.Background()
	require.NoError(t, manager.syncMachinePorts(ctx, store.machine, store.services))
	require.Equal(t, []int{3000, 5173}, store.appPorts.listening, "service ports are not reported")
	require.Len(t, store.appPorts.ports, 2)
	agent := store.appPorts.ports[1]
	require.Equal(t, 3000, agent.Port)
	require.Equal(t, "agent", agent.Source)
	require.Equal(t, "web", *agent.Label)
	require.Equal(t, task, *agent.AgentTaskID)
	require.Equal(t, *store.machine.CreatedByUserID, agent.CreatedByUserID, "the gateway only admits the machine creator")

	state := portState(t, runtime)
	require.Equal(t, []agentPort{
		{Port: 3000, Label: "web", URL: "https://0123456789abcdef0123-3000.machines.example.test", Source: "agent"},
		{Port: 5173, URL: "https://0123456789abcdef0123-5173.machines.example.test", Source: "user"},
	}, sortedPorts(state.Ports))
	require.Equal(t, []int{3000, 5173}, state.Listening)
	require.Equal(t, []int{7681, 8080}, state.Reserved, "kuayle-ports hides the machine's own services")
	reasons := map[int]string{}
	for _, refusal := range state.Refused {
		reasons[refusal.Port] = refusal.Reason
	}
	require.Contains(t, reasons[8080], "service")
	require.Contains(t, reasons[5173], "only ports exposed by agents")
	require.Contains(t, reasons[4000], "does not match")

	// The limit is enforced, and an unchanged state is not rewritten at once.
	for port := 6000; len(store.appPorts.ports) < domain.DevMachinePortLimit; port++ {
		store.appPorts.ports = append(store.appPorts.ports, domain.DevMachinePort{ID: uuid.New(), Port: port, Source: "user"})
	}
	runtime.scan.Requests = []PortRequest{request("expose-3001.json", `{"port":3001}`)}
	require.NoError(t, manager.syncMachinePorts(ctx, store.machine, store.services))
	require.Len(t, store.appPorts.ports, domain.DevMachinePortLimit)
	require.Contains(t, portState(t, runtime).Refused[0].Reason, "at most 10")
	require.NoError(t, manager.syncMachinePorts(ctx, store.machine, store.services))
	writes := len(runtime.states)
	require.NoError(t, manager.syncMachinePorts(ctx, store.machine, store.services))
	require.Len(t, runtime.states, writes, "nothing changed")

	// Requests are capped per scan.
	runtime.scan.Requests = nil
	for i := 0; i < 50; i++ {
		runtime.scan.Requests = append(runtime.scan.Requests, request(fmt.Sprintf("unexpose-%d", 7000+i), ""))
	}
	require.NoError(t, manager.syncMachinePorts(ctx, store.machine, store.services))
	require.Len(t, portState(t, runtime).Refused, maxPortRequestsPerScan)
}

func sortedPorts(ports []agentPort) []agentPort {
	for i := range ports {
		for j := i + 1; j < len(ports); j++ {
			if ports[j].Port < ports[i].Port {
				ports[i], ports[j] = ports[j], ports[i]
			}
		}
	}
	return ports
}

func previewTarget(machine *domain.DevMachine, preferred *int) *repository.TaskPreviewTarget {
	starting := domain.AgentTaskPreviewStarting
	return &repository.TaskPreviewTarget{Identifier: "ENG-1", Task: domain.AgentTask{
		ID: uuid.New(), WorkspaceID: machine.WorkspaceID, MachineID: &machine.ID, CheckoutID: uuid.New(), CreatorID: *machine.CreatedByUserID,
		PreviewCommand: json.RawMessage(`["npm","run","dev"]`), PreviewPort: preferred, PreviewState: &starting,
	}}
}

// Each task gets its own process and port: the preferred port when free, else
// the first free port from 4100, and never a service or exposed port.
func TestTaskPreviewsGetTheirOwnProcessAndPort(t *testing.T) {
	manager, store, runtime := newPortManager()
	ctx := context.Background()
	preferred := 5173
	store.appPorts.target = previewTarget(store.machine, &preferred)
	store.checkout = &domain.DevMachineCheckout{ID: store.appPorts.target.Task.CheckoutID, MachineID: store.machine.ID, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1"}
	op := &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}
	require.NoError(t, manager.startPreview(ctx, store.machine, op))
	require.Equal(t, []startedPreview{{store.appPorts.target.Task.ID, []string{"npm", "run", "dev"}, 5173, "/workspace/tasks/eng-1"}}, runtime.started)
	require.Equal(t, []string{"running"}, store.appPorts.previewStates)
	require.Equal(t, 5173, store.appPorts.previewPort.Port)
	require.Equal(t, "task", store.appPorts.previewPort.Source)
	require.Equal(t, "ENG-1", *store.appPorts.previewPort.Label)
	require.Equal(t, store.appPorts.target.Task.ID, *store.appPorts.previewPort.AgentTaskID)
	require.Empty(t, runtime.stopped, "starting never stops other tasks' previews")

	// A second task whose preferred port is taken gets the first free port.
	runtime.scan = PortScan{Listening: []int{4100}}
	first := store.appPorts.target.Task.ID
	store.appPorts.target = previewTarget(store.machine, &preferred)
	store.appPorts.target.Task.CheckoutID = store.checkout.ID
	op = &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}
	require.NoError(t, manager.startPreview(ctx, store.machine, op))
	require.Equal(t, 4101, runtime.started[1].port, "5173 is exposed and 4100 is listening")
	require.NotEqual(t, first, runtime.started[1].task)

	// Restarting reuses the task's own port.
	running := domain.AgentTaskPreviewStarting
	store.appPorts.target.Task.PreviewState = &running
	existing := *store.appPorts.previewPort
	store.appPorts.target.Port = &existing
	require.NoError(t, manager.startPreview(ctx, store.machine, op))
	require.Equal(t, 4101, runtime.started[2].port)

	// A preferred service port is never used.
	runtime.scan = PortScan{}
	service := 8080
	store.appPorts.target = previewTarget(store.machine, &service)
	store.appPorts.target.Task.CheckoutID = store.checkout.ID
	op = &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}
	require.NoError(t, manager.startPreview(ctx, store.machine, op))
	require.Equal(t, 4100, runtime.started[3].port, "the 4100 listener stopped meanwhile")
}

func TestTaskPreviewFailuresAreRecorded(t *testing.T) {
	ctx := context.Background()
	t.Run("not starting", func(t *testing.T) {
		manager, store, runtime := newPortManager()
		store.appPorts.target = previewTarget(store.machine, nil)
		stopped := domain.AgentTaskPreviewStopped
		store.appPorts.target.Task.PreviewState = &stopped
		require.NoError(t, manager.startPreview(ctx, store.machine, &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}))
		require.Empty(t, runtime.started, "a stopped request needs no preview")
		require.Empty(t, store.appPorts.previewStates)
	})
	t.Run("worktree missing", func(t *testing.T) {
		manager, store, runtime := newPortManager()
		store.appPorts.target = previewTarget(store.machine, nil)
		require.NoError(t, manager.startPreview(ctx, store.machine, &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}))
		require.Empty(t, runtime.started)
		require.Equal(t, []string{"failed"}, store.appPorts.previewStates)
		require.Contains(t, store.appPorts.previewErrors[0], "worktree")
	})
	t.Run("port limit", func(t *testing.T) {
		manager, store, runtime := newPortManager()
		store.appPorts.target = previewTarget(store.machine, nil)
		store.checkout = &domain.DevMachineCheckout{ID: store.appPorts.target.Task.CheckoutID, MachineID: store.machine.ID, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1"}
		for port := 3000; port < 3010; port++ {
			store.appPorts.ports = append(store.appPorts.ports, domain.DevMachinePort{ID: uuid.New(), Port: port, Source: "user"})
		}
		require.NoError(t, manager.startPreview(ctx, store.machine, &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}))
		require.Empty(t, runtime.started)
		require.Equal(t, []string{"failed"}, store.appPorts.previewStates)
		require.Contains(t, store.appPorts.previewErrors[0], "10 app ports")
	})
	t.Run("process fails", func(t *testing.T) {
		manager, store, runtime := newPortManager()
		store.appPorts.target = previewTarget(store.machine, nil)
		store.checkout = &domain.DevMachineCheckout{ID: store.appPorts.target.Task.CheckoutID, MachineID: store.machine.ID, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1"}
		runtime.startErr = errors.New("exec failed: npm: not found")
		require.NoError(t, manager.startPreview(ctx, store.machine, &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}))
		require.Equal(t, []string{"failed"}, store.appPorts.previewStates)
		require.Equal(t, []uuid.UUID{store.appPorts.target.Task.ID}, store.appPorts.stoppedTasks, "its port is removed again")
		require.Empty(t, store.appPorts.ports)
	})
}

func TestStopPreviewKillsOnlyThatTask(t *testing.T) {
	manager, store, runtime := newPortManager()
	task := uuid.New()
	require.NoError(t, manager.stopPreview(context.Background(), store.machine, &domain.DevMachineOperation{AgentTaskID: &task}))
	require.Equal(t, []uuid.UUID{task}, runtime.stopped)
	require.Equal(t, []uuid.UUID{task}, store.appPorts.stoppedTasks)

	store.machine.Status = domain.DevMachineStatusStopped
	other := uuid.New()
	require.NoError(t, manager.stopPreview(context.Background(), store.machine, &domain.DevMachineOperation{AgentTaskID: &other}))
	require.Len(t, runtime.stopped, 1, "a stopped machine has no process to kill")
	require.Equal(t, other, store.appPorts.stoppedTasks[1])
}
