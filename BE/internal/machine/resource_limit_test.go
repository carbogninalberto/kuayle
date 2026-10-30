package machine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func runOutOfMemoryAgent(t *testing.T, execution *AgentExecution, task bool) *managerStoreFake {
	t.Helper()
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID,
		ProviderID: "opencode", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30,
	}
	if task {
		taskID := uuid.New()
		run.TaskID = &taskID
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", MemoryMB: 4096,
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		agentRun: run,
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "opencode", ImageRef: "kuayle/opencode:test", Enabled: true},
	}
	manager := NewManager(store, &runtimeFake{agentExecution: execution}, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), make([]byte, 32), "test")
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}))
	require.NotNil(t, store.completedRun)
	return store
}

// Production: Claude Code exited 137 with an empty error after the kernel
// killed it in the agent container's 35% share of a 4 GiB task machine.
func TestOutOfMemoryAgentRunExplainsTheLimit(t *testing.T) {
	limit := int64(4096) * 1024 * 1024 * 35 / 100
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 137, OOMKilled: true, MemoryLimitBytes: limit}, true)
	expected := "Out of memory: the agent container reached its 1.4 GiB memory limit. Resize the machine or raise the task machine memory."
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.NotNil(t, store.completedRun.ErrorMessage)
	require.Equal(t, expected, *store.completedRun.ErrorMessage)
	require.Equal(t, []string{"memory: " + expected}, store.resourceLimits)
	var logged []string
	for _, chunk := range store.outputChunks {
		logged = append(logged, chunk.Content)
	}
	require.Contains(t, strings.Join(logged, "\n"), "Kuayle: "+expected, "the run log says why it stopped")
}

func TestKilledAgentWithoutMemoryEventIsNotCalledOutOfMemory(t *testing.T) {
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 137, MemoryLimitBytes: 1 << 30}, false)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.NotNil(t, store.completedRun.ErrorMessage, "an empty error hides why the process stopped")
	require.Equal(t, "The agent process was killed (exit code 137) without an out-of-memory event; it may have been stopped by a signal.", *store.completedRun.ErrorMessage)
	require.False(t, domain.IsResourceLimitReason(*store.completedRun.ErrorMessage))
	require.Empty(t, store.resourceLimits)
}

func TestContainerMemoryEventIsRecordedOnTheMachine(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning}
	store := &managerStoreFake{machine: machine}
	manager := &Manager{store: store}
	hit := ResourceLimitHit{MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ContainerID: "terminal-1", Service: "terminal", LimitBytes: 4 << 30}
	manager.recordMemoryLimit(context.Background(), hit, time.Now())
	manager.recordMemoryLimit(context.Background(), hit, time.Now().Add(time.Second))
	expected := "Out of memory: the terminal container reached its 4 GiB memory limit. Resize the machine."
	require.Equal(t, []string{"memory: " + expected}, store.resourceLimits, "a burst of kills in one container is reported once")
	require.Len(t, store.events, 1)
	require.Equal(t, "resource.memory_limit", store.events[0].EventType)
	require.Contains(t, string(store.events[0].Payload), `"service":"terminal"`)

	other := hit
	other.WorkspaceID = uuid.New()
	other.ContainerID = "terminal-2"
	manager.recordMemoryLimit(context.Background(), other, time.Now())
	require.Len(t, store.resourceLimits, 1, "a container labelled for another workspace is ignored")
}

func TestDiskLimitStopIsRecordedOnTheMachine(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 2, DiskGB: 20}
	store := &managerStoreFake{machine: machine, runtimeMachines: []domain.DevMachine{*machine}}
	runtime := &runtimeFake{stats: map[uuid.UUID]ResourceUsage{machine.ID: {DiskBytes: 21 << 30}}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")
	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, []string{"disk: " + domain.DiskLimitMessage(20)}, store.resourceLimits)
}

type oomValidationRuntimeFake struct {
	*validationRuntimeFake
}

func (f *oomValidationRuntimeFake) RunValidation(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep, checkout *domain.DevMachineCheckout, image string, o *AgentObserver) (*AgentExecution, error) {
	execution, err := f.validationRuntimeFake.RunValidation(ctx, machine, run, step, checkout, image, o)
	if execution != nil {
		execution.OOMKilled, execution.MemoryLimitBytes = true, 2<<30
	}
	return execution, err
}

func TestOutOfMemoryCheckExplainsTheLimitInItsLog(t *testing.T) {
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	key := make([]byte, 32)
	encrypted, err := cryptoutil.Encrypt(`{}`, key)
	require.NoError(t, err)
	store := &validationStoreFake{managerStoreFake: &managerStoreFake{machine: machine, agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, EncryptedOutputSecrets: &encrypted, MaxRuntimeSeconds: 60}, checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready"}, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}}, step: domain.DevMachineAgentRunStep{ID: uuid.New(), AgentRunID: runID, Status: "queued", CreatedAt: time.Now(), CommandArgv: []byte(`["test"]`)}}
	runtime := &oomValidationRuntimeFake{validationRuntimeFake: &validationRuntimeFake{runtimeFake: &runtimeFake{}, exit: 137}}
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key}
	require.NoError(t, manager.validateAgent(context.Background(), machine, &domain.DevMachineOperation{AgentRunID: &runID}))
	require.Equal(t, 137, *store.exit)
	expected := "Out of memory: the check container reached its 2 GiB memory limit. Resize the machine or raise the task machine memory."
	var logged []string
	for _, chunk := range store.validationChunks {
		logged = append(logged, chunk.Content)
	}
	require.Contains(t, strings.Join(logged, ""), "Kuayle: "+expected)
	require.Equal(t, []string{"memory: " + expected}, store.resourceLimits)
}
