package machine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type validationStoreFake struct {
	*managerStoreFake
	step             domain.DevMachineAgentRunStep
	validationChunks []domain.DevMachineLogChunk
	outputErr        error
	exit             *int
	results          []domain.CheckCommandResult
}

func (f *validationStoreFake) GetValidation(context.Context, domain.DevMachineOperation) (*domain.DevMachineAgentRunStep, error) {
	copy := f.step
	return &copy, nil
}
func (f *validationStoreFake) StartValidation(context.Context, domain.DevMachineOperation, string) error {
	f.step.Status = "running"
	return nil
}
func (f *validationStoreFake) CompleteValidation(_ context.Context, _ domain.DevMachineOperation, exit int, results []domain.CheckCommandResult) error {
	f.exit = &exit
	f.results = results
	f.step.Status = "succeeded"
	return nil
}
func (f *validationStoreFake) CreateValidationOutput(_ context.Context, _ domain.DevMachineOperation, c *domain.DevMachineLogChunk) error {
	f.validationChunks = append(f.validationChunks, *c)
	return f.outputErr
}

type validationRuntimeFake struct {
	*runtimeFake
	calls int
	exit  int
	// exits, when set, is each command's exit code by index.
	exits   []int
	indexes []int
	image   string
	err     error
}

func (f *validationRuntimeFake) RunValidation(_ context.Context, _ *domain.DevMachine, _ *domain.DevMachineAgentRun, _ *domain.DevMachineAgentRunStep, index int, _ *domain.DevMachineCheckout, image string, o *AgentObserver) (*AgentExecution, error) {
	f.calls++
	f.indexes = append(f.indexes, index)
	f.image = image
	exit := f.exit
	if index < len(f.exits) {
		exit = f.exits[index]
	}
	if f.err != nil {
		return nil, f.err
	}
	if err := o.Authorize(); err != nil {
		return nil, err
	}
	if err := o.Started("check"); err != nil {
		return nil, err
	}
	if err := o.Output("stdout", []byte("2026-10-27T10:00:00Z fixture-private-value must be hidden; tests passed\n")); err != nil {
		return nil, err
	}
	return &AgentExecution{ContainerID: "check", ExitCode: exit}, nil
}
func TestValidationManagerUsesActualExitAndRedactsWithoutModelParsing(t *testing.T) {
	for _, outputFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "actual_exit", true: "retain_recovery_container"}[outputFailure], func(t *testing.T) {
			taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
			machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
			key := make([]byte, 32)
			encrypted, err := cryptoutil.Encrypt(`{"FIXTURE_CREDENTIAL":"fixture-private-value"}`, key)
			require.NoError(t, err)
			store := &validationStoreFake{managerStoreFake: &managerStoreFake{machine: machine, agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, EncryptedOutputSecrets: &encrypted, MaxRuntimeSeconds: 60}, checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready"}, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}}, step: domain.DevMachineAgentRunStep{ID: uuid.New(), AgentRunID: runID, Status: "queued", CreatedAt: time.Now(), CommandArgv: []byte(`["test"]`)}}
			if outputFailure {
				store.outputErr = errors.New("persistence unavailable")
			}
			runtime := &validationRuntimeFake{runtimeFake: &runtimeFake{}, exit: 7}
			manager := &Manager{store: store, runtime: runtime, encryptionKey: key}
			err = manager.validateAgent(context.Background(), machine, &domain.DevMachineOperation{AgentRunID: &runID})
			if outputFailure {
				require.Error(t, err)
				require.Nil(t, store.exit)
				require.Zero(t, runtime.agentCancellations, "retain stopped container so retry replays, not reruns")
			} else {
				require.NoError(t, err)
				require.Equal(t, 7, *store.exit, "model text claiming tests passed must not override process failure")
			}
			require.Equal(t, "developer:fixture", runtime.image)
			require.NotEmpty(t, store.validationChunks)
			for _, chunk := range store.validationChunks {
				require.False(t, strings.Contains(chunk.Content, "fixture-private-value"))
			}
		})
	}
}

func TestValidationManagerFailsARejectedComposeStackWithItsReasonAndCleansUp(t *testing.T) {
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	key := make([]byte, 32)
	encrypted, err := cryptoutil.Encrypt(`{}`, key)
	require.NoError(t, err)
	store := &validationStoreFake{managerStoreFake: &managerStoreFake{machine: machine, agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, EncryptedOutputSecrets: &encrypted, MaxRuntimeSeconds: 60}, checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready"}, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}}, step: domain.DevMachineAgentRunStep{ID: uuid.New(), AgentRunID: runID, Status: "queued", CreatedAt: time.Now(), CommandArgv: []byte(`["test"]`), ComposeFile: "compose.yaml"}}
	runtime := &validationRuntimeFake{runtimeFake: &runtimeFake{}, err: &ComposeError{Reason: `service "app": "privileged" is not supported`}}
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key}
	err = manager.validateAgent(context.Background(), machine, &domain.DevMachineOperation{AgentRunID: &runID})
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal), "a configuration problem must not be retried")
	require.Equal(t, "compose_rejected", errorCode(err))
	require.Contains(t, terminal.message, "privileged")
	require.Nil(t, store.exit)
	require.Equal(t, 1, runtime.checkCleanups)
	require.Len(t, store.validationChunks, 1)
	require.Contains(t, store.validationChunks[0].Content, "privileged")
}

func TestValidationManagerRunsEveryAgentChosenCommandAndRecordsEachResult(t *testing.T) {
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	key := make([]byte, 32)
	encrypted, err := cryptoutil.Encrypt(`{}`, key)
	require.NoError(t, err)
	source := domain.CheckSourceAgent
	step := domain.DevMachineAgentRunStep{ID: uuid.New(), AgentRunID: runID, Status: "queued", CreatedAt: time.Now(), CommandSource: &source,
		CommandArgv: []byte(`["go","vet","./..."]`), Commands: []byte(`[["go","vet","./..."],["go","test","./internal/service/..."],["npm","run","check"]]`)}
	store := &validationStoreFake{managerStoreFake: &managerStoreFake{machine: machine, agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, EncryptedOutputSecrets: &encrypted, MaxRuntimeSeconds: 60}, checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready"}, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}}, step: step}
	runtime := &validationRuntimeFake{runtimeFake: &runtimeFake{}, exits: []int{0, 2, 0}}
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key}
	require.NoError(t, manager.validateAgent(context.Background(), machine, &domain.DevMachineOperation{AgentRunID: &runID}))
	require.Equal(t, []int{0, 1, 2}, runtime.indexes, "a failure does not hide the remaining commands' results")
	require.Equal(t, 2, *store.exit, "the first failing command decides the check")
	require.Equal(t, []domain.CheckCommandResult{{ExitCode: 0}, {ExitCode: 2}, {ExitCode: 0}}, store.results)
	var output strings.Builder
	for _, chunk := range store.validationChunks {
		output.WriteString(chunk.Content)
	}
	require.Contains(t, output.String(), "command 2 of 3 (go test ./internal/service/...) exited with code 2")
}
