package machine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// runAgentForTrace starts one unattended run of an OpenCode task and returns
// the store with what the manager recorded when it finished.
func runAgentForTrace(t *testing.T, execution *AgentExecution, configure func(*domain.DevMachineAgentRun, *managerStoreFake, *runtimeFake)) *managerStoreFake {
	t.Helper()
	machineID, workspaceID, runID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID, TaskID: &taskID,
		ProviderID: "opencode", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
		CommandArgv: []byte(`["opencode"]`), Model: "model-a", Thinking: "high",
		MaxRuntimeSeconds: 600, NativeSessionID: ptr("ses_previous"),
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			CPUMillis: 4000, MemoryMB: 8192, PidsLimit: 512,
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		agentRun: run,
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "opencode", ImageRef: "kuayle/opencode:test", Enabled: true},
	}
	runtime := &runtimeFake{agentExecution: execution}
	if configure != nil {
		configure(run, store, runtime)
	}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), make([]byte, 32), "test")
	manager.agentCLIVersions = map[string]string{"opencode-ai": "1.2.3"}
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}))
	require.NotNil(t, store.completedRun)
	return store
}

func runEnvironment(t *testing.T, run *domain.DevMachineAgentRun) domain.AgentRunEnvironment {
	t.Helper()
	require.NotEmpty(t, run.Environment, "a run records what it ran on")
	var environment domain.AgentRunEnvironment
	require.NoError(t, json.Unmarshal(run.Environment, &environment))
	return environment
}

// A failed run names its class and the real cause: exit code, signal and the
// end of standard error, not only "Execution failed".
func TestFailedRunRecordsItsClassAndCause(t *testing.T) {
	store := runAgentForTrace(t, &AgentExecution{
		ContainerID: "agent", ExitCode: 1, Stderr: "Error: the configuration file is not valid JSON\n",
	}, nil)
	run := store.completedRun
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, run.Status)
	require.NotNil(t, run.FailureClass)
	require.Equal(t, string(domain.FailureAgent), *run.FailureClass)
	require.NotNil(t, run.FailureRule)
	require.Equal(t, "harness_exit_with_output", *run.FailureRule)
	require.NotNil(t, run.FailureCause)
	require.Contains(t, *run.FailureCause, "exit code 1")
	require.Contains(t, *run.FailureCause, "Error: the configuration file is not valid JSON")
	require.NotNil(t, run.ErrorMessage, "no failed run is stored without a cause")
	require.Contains(t, *run.ErrorMessage, "Error: the configuration file is not valid JSON")
}

// A crash at the container's process limit is the platform's failure, and the
// cause says which limit.
func TestPlatformPIDLimitIsRecordedAsTheCause(t *testing.T) {
	store := runAgentForTrace(t, &AgentExecution{
		ContainerID: "agent", ExitCode: 139, PidsLimit: 1024, PidsPeak: 1024,
	}, nil)
	run := store.completedRun
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, run.Status)
	require.NotNil(t, run.FailureClass)
	require.Equal(t, string(domain.FailurePlatform), *run.FailureClass)
	require.NotNil(t, run.FailureRule)
	require.Equal(t, "pid_limit", *run.FailureRule)
	require.NotNil(t, run.FailureCause)
	require.Contains(t, *run.FailureCause, "limit of 1024 processes and threads")
	require.Contains(t, *run.FailureCause, "SIGSEGV")
}

// A run records its harness, versions, image, model, limits and the session
// it resumed, and its output cuts.
func TestRunRecordsWhatItRanOnAndWhatWasCut(t *testing.T) {
	store := runAgentForTrace(t, &AgentExecution{
		ContainerID: "agent-container", ExitCode: 1,
		PidsLimit: 1024, PidsPeak: 900, MemoryLimitBytes: 4 << 30, CPUMillis: 2000,
	}, func(_ *domain.DevMachineAgentRun, _ *managerStoreFake, runtime *runtimeFake) {
		runtime.onAgentOutput = func(output AgentOutput) error {
			plain := strings.Repeat("x", 40<<10)
			for _, line := range []string{"first", plain, "second"} {
				if err := output("stdout", []byte("2026-11-11T10:00:00.000000001Z "+line+"\n")); err != nil {
					return err
				}
			}
			return nil
		}
	})
	run := store.completedRun
	environment := runEnvironment(t, run)
	require.Equal(t, "opencode", environment.Harness)
	require.Equal(t, "opencode", environment.HarnessCommand)
	require.Equal(t, "1.2.3", environment.HarnessVersion)
	require.Equal(t, "shared agent CLI refresh", environment.HarnessVersionFrom)
	require.Equal(t, "kuayle/opencode:test", environment.ImageRef)
	require.Equal(t, "model-a", environment.Model)
	require.Equal(t, "high", environment.Effort)
	require.Equal(t, "ses_previous", environment.ResumedSession)
	require.EqualValues(t, 600, environment.TimeLimitSeconds)
	require.EqualValues(t, 2000, environment.CPUMillis)
	require.EqualValues(t, 4<<30, environment.MemoryBytes)
	require.EqualValues(t, 1024, environment.PidsLimit)
	require.EqualValues(t, 900, environment.PidsPeak)
	require.Equal(t, "agent-container", environment.ContainerID)
	require.NotEmpty(t, store.recordedEnvironment)

	var cuts []domain.OutputCut
	require.NoError(t, json.Unmarshal(run.OutputCuts, &cuts))
	cut := cutOf(cuts, "stdout", domain.OutputCutLine)
	require.NotNil(t, cut)
	require.Equal(t, 1, cut.Count)
	require.Positive(t, cut.Bytes)
}

func TestPidsSampleKeepsThePeak(t *testing.T) {
	var sample pidsSample
	sample.observe(12)
	sample.observe(1024)
	sample.observe(80)
	require.EqualValues(t, 1024, sample.peak.Load())
}

type identityRuntimeFake struct {
	*runtimeFake
}

func (f *identityRuntimeFake) ImageIdentity(context.Context, string) (string, string, error) {
	return "sha256:image-id", "sha256:manifest-digest", nil
}

// The image reference resolves to the local image and its digest without
// starting anything; an unknown harness version stays empty, never guessed.
func TestRunEnvironmentResolvesTheImageAndNeverGuessesAVersion(t *testing.T) {
	machine := &domain.DevMachine{CPUMillis: 2000, MemoryMB: 4096, PidsLimit: 512}
	run := &domain.DevMachineAgentRun{ProviderID: "crush", CommandArgv: []byte(`["crush","run"]`), MaxRuntimeSeconds: 60}
	manager := &Manager{runtime: &identityRuntimeFake{runtimeFake: &runtimeFake{}}}
	environment := manager.runEnvironment(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "kuayle/crush:test"}, nil)
	require.Equal(t, "sha256:image-id", environment.ImageID)
	require.Equal(t, "sha256:manifest-digest", environment.ImageDigest)
	require.Equal(t, "crush", environment.HarnessCommand)
	require.Empty(t, environment.HarnessVersion)
	require.Empty(t, environment.HarnessVersionFrom)
}

func TestCancelledRunRecordsWhoStoppedIt(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning}
	runID := uuid.New()
	store := &managerStoreFake{
		machine: machine,
		agentRun: &domain.DevMachineAgentRun{
			ID: runID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
			ProviderID: "opencode", Status: domain.DevMachineAgentRunStatusRunning,
		},
	}
	manager := &Manager{store: store, runtime: &runtimeFake{}}
	manager.cancelStaleAgentRun(context.Background(), runID)
	require.Equal(t, domain.DevMachineAgentRunStatusCancelled, store.completedRun.Status)
	require.NotNil(t, store.completedRun.FailureClass)
	require.Equal(t, string(domain.FailurePlatform), *store.completedRun.FailureClass)
	require.Equal(t, "cancelled_by_platform", *store.completedRun.FailureRule)

	policyStore := &managerStoreFake{
		machine: machine,
		agentRun: &domain.DevMachineAgentRun{
			ID: runID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
			ProviderID: "opencode", Status: domain.DevMachineAgentRunStatusRunning,
		},
	}
	manager.store = policyStore
	require.NoError(t, manager.cancelUnauthorizedRun(context.Background(), policyStore.agentRun))
	require.NotNil(t, policyStore.completedRun.FailureClass)
	require.Equal(t, string(domain.FailurePolicy), *policyStore.completedRun.FailureClass)
	require.Equal(t, "cancelled_by_policy", *policyStore.completedRun.FailureRule)
}

func TestRunThatCouldNotStartRecordsTheManagerError(t *testing.T) {
	store := &managerStoreFake{
		machine: &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning},
		agentRun: &domain.DevMachineAgentRun{
			ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New(),
			ProviderID: "opencode", Status: domain.DevMachineAgentRunStatusStarting,
		},
	}
	manager := &Manager{store: store}
	manager.failAgentRun(context.Background(), store.agentRun.ID, &terminalOperationError{code: "checkout_not_ready", message: "agent checkout is unavailable"})
	require.NotNil(t, store.completedRun.FailureClass)
	require.Equal(t, string(domain.FailurePlatform), *store.completedRun.FailureClass)
	require.NotNil(t, store.completedRun.FailureCause)
	require.Contains(t, *store.completedRun.FailureCause, "checkout was not ready")

	signIn := &managerStoreFake{
		machine: &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning},
		agentRun: &domain.DevMachineAgentRun{
			ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New(),
			ProviderID: "claude-code", Status: domain.DevMachineAgentRunStatusStarting,
		},
	}
	manager.store = signIn
	manager.failAgentRun(context.Background(), signIn.agentRun.ID, &terminalOperationError{code: "sign_in_required", message: "Claude Code needs you to sign in"})
	require.NotNil(t, signIn.completedRun.FailureClass)
	require.Equal(t, string(domain.FailureProvider), *signIn.completedRun.FailureClass)
	require.Equal(t, "sign_in_required", *signIn.completedRun.FailureRule)
}
