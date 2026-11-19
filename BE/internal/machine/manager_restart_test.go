package machine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// A stopping manager stops following an agent attempt without stopping it: the
// container keeps running for the next manager to re-adopt.
func TestAgentDetachStopsFollowingWithoutStoppingTheContainer(t *testing.T) {
	var stops, kills atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/logs"):
			writeDockerFrame(w, stdcopy.Stdout, "working\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/wait"):
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/stop"):
			stops.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/kill"):
			kills.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	detach := make(chan struct{})
	var once sync.Once
	started := time.Now()
	_, err = runtime.resumeAgent(ctx, container.InspectResponse{ID: "test", State: &container.State{Running: true}}, false, nil, &AgentObserver{
		Detach: detach,
		Output: func(string, []byte) error {
			once.Do(func() { close(detach) })
			return nil
		},
	})
	require.ErrorIs(t, err, ErrAgentDetached)
	require.Less(t, time.Since(started), time.Second, "detaching does not wait for the container")
	require.Zero(t, stops.Load(), "detaching never stops the container")
	require.Zero(t, kills.Load(), "detaching never kills the container")
}

// testAgentRunFixture is a machine and run an agent operation can start.
func testAgentRunFixture(startedAt *time.Time) (*domain.DevMachine, *domain.DevMachineAgentRun) {
	machine := &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture-agent",
		DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume"),
	}
	run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, CommandArgv: json.RawMessage(`["codex"]`), StartedAt: startedAt}
	return machine, run
}

// fakeAgentDaemon answers the Docker calls RunAgent makes before it creates the
// attempt's container. inspect lets one test choose what ContainerInspect
// returns; creates counts container creations.
func fakeAgentDaemon(inspect func(*http.Request) (string, int), creates *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"sha256:x","Config":{"Cmd":[],"Env":[]}}`))
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			creates.Add(1)
			http.Error(w, "create failed", http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/volumes/create"):
			var body struct {
				Name   string
				Labels map[string]string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"Name": body.Name, "Labels": body.Labels})
		case strings.HasSuffix(r.URL.Path, "/json"):
			body, status := inspect(r)
			if status != http.StatusOK {
				http.Error(w, "not found", status)
				return
			}
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

// A run that started without a container was removed while no manager followed
// it: the runtime says so instead of re-creating a container for it.
func TestRunAgentReportsAContainerThatIsGone(t *testing.T) {
	now := time.Now()
	machine, run := testAgentRunFixture(&now)
	var creates atomic.Int32
	server := fakeAgentDaemon(func(*http.Request) (string, int) {
		return "", http.StatusNotFound
	}, &creates)
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	_, err = runtime.RunAgent(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "fixture"}, nil, nil, nil)
	require.ErrorIs(t, err, ErrAgentContainerGone)
	require.Zero(t, creates.Load(), "a run whose container is gone is never re-created")
}

// A run that never started (no StartedAt) is still created when no container
// exists yet; the 404 alone is not a lost attempt.
func TestRunAgentCreatesARunThatNeverStarted(t *testing.T) {
	machine, run := testAgentRunFixture(nil)
	var creates atomic.Int32
	server := fakeAgentDaemon(func(*http.Request) (string, int) {
		return "", http.StatusNotFound
	}, &creates)
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	_, err = runtime.RunAgent(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "fixture"}, nil, nil, nil)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAgentContainerGone)
	require.Positive(t, creates.Load(), "an attempt that never started is created")
}

// A manager that finds an already exited container marks the execution as one
// it did not observe, so the run is classified as a manager restart.
func TestRunAgentFindsAnExitedContainerUnobserved(t *testing.T) {
	now := time.Now()
	machine, run := testAgentRunFixture(&now)
	var creates atomic.Int32
	server := fakeAgentDaemon(func(r *http.Request) (string, int) {
		if !strings.Contains(r.URL.Path, "/containers/") {
			return "", http.StatusNotFound
		}
		body, _ := json.Marshal(map[string]any{
			"Id":     "existing",
			"State":  map[string]any{"Status": "exited", "Running": false, "ExitCode": 143},
			"Config": map[string]any{"Image": "fixture", "Labels": map[string]string{"com.kuayle.agent-run-id": run.ID.String()}},
		})
		return string(body), http.StatusOK
	}, &creates)
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	execution, err := runtime.RunAgent(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "fixture"}, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.True(t, execution.ExitedUnobserved)
	require.Equal(t, 143, execution.ExitCode)
	require.Zero(t, creates.Load())
}

// restartRunFixture is a run whose container an operation re-adopts, set up
// like a run that survived a machine manager restart.
func restartRunFixture(t *testing.T) (*managerStoreFake, *runtimeFake, *Manager, uuid.UUID) {
	t.Helper()
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	key := cryptoutil.DeriveKey("manager-restart-test")
	current, _ := cryptoutil.Encrypt("fixture-current-provider-key", key)
	snapshot, _ := cryptoutil.Encrypt(`{"KUAYLE_MODEL_API_KEY":"fixture-original-provider-key"}`, key)
	run := &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusRunning, EncryptedOutputSecrets: &snapshot, MaxRuntimeSeconds: 30}
	store := &managerStoreFake{
		machine:     &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1},
		agentRun:    run,
		provider:    &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"},
		modelServer: &domain.AgentModelServer{EncryptedAPIKey: current, DefaultModel: "deepseek-flash"},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")
	return store, runtime, manager, runID
}

func restartOperation(store *managerStoreFake, runID uuid.UUID) *domain.DevMachineOperation {
	return &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID,
		AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}
}

// A detached attempt is handed to the next manager: no attempt is counted and
// the container is never stopped.
func TestRunAgentDetachesWithoutCompletingOrStoppingTheRun(t *testing.T) {
	store, runtime, manager, runID := restartRunFixture(t)
	runtime.onRunAgent = func(context.Context, *AgentObserver) (*AgentExecution, error) {
		return nil, ErrAgentDetached
	}
	err := manager.runAgent(context.Background(), store.machine, restartOperation(store, runID))
	require.ErrorIs(t, err, errManagerShutdown)
	require.Nil(t, store.completedRun)
	require.Zero(t, runtime.agentCancellations)
}

// A run whose container was removed is recorded as a platform failure of the
// restart, never left starting forever.
func TestRunAgentRecordsAGoneContainerAsAManagerRestart(t *testing.T) {
	store, runtime, manager, runID := restartRunFixture(t)
	runtime.onRunAgent = func(context.Context, *AgentObserver) (*AgentExecution, error) {
		return nil, ErrAgentContainerGone
	}
	err := manager.runAgent(context.Background(), store.machine, restartOperation(store, runID))
	require.NoError(t, err)
	require.NotNil(t, store.completedRun)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.NotNil(t, store.completedRun.FailureClass)
	require.Equal(t, string(domain.FailurePlatform), *store.completedRun.FailureClass)
	require.NotNil(t, store.completedRun.FailureRule)
	require.Equal(t, domain.FailureRuleManagerRestart, *store.completedRun.FailureRule)
}

// An attempt that ended while no manager followed it says so, and the generic
// killed message no longer hides the manager restart behind it.
func TestRunAgentClassifiesAnUnobservedExitAsAManagerRestart(t *testing.T) {
	t.Run("unobserved", func(t *testing.T) {
		store, runtime, manager, runID := restartRunFixture(t)
		runtime.onRunAgent = func(context.Context, *AgentObserver) (*AgentExecution, error) {
			return &AgentExecution{ContainerID: "existing", ExitCode: 143, ExitedUnobserved: true}, nil
		}
		require.NoError(t, manager.runAgent(context.Background(), store.machine, restartOperation(store, runID)))
		require.NotNil(t, store.completedRun)
		require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
		require.Equal(t, string(domain.FailurePlatform), *store.completedRun.FailureClass)
		require.Equal(t, domain.FailureRuleManagerRestart, *store.completedRun.FailureRule)
		require.NotNil(t, store.completedRun.ErrorMessage)
		require.Contains(t, *store.completedRun.ErrorMessage, "machine manager restarted")
		require.NotContains(t, *store.completedRun.ErrorMessage, "exit code 137")
	})
	t.Run("observed", func(t *testing.T) {
		store, runtime, manager, runID := restartRunFixture(t)
		runtime.onRunAgent = func(context.Context, *AgentObserver) (*AgentExecution, error) {
			return &AgentExecution{ContainerID: "existing", ExitCode: 137, ExitedUnobserved: false}, nil
		}
		require.NoError(t, manager.runAgent(context.Background(), store.machine, restartOperation(store, runID)))
		require.NotNil(t, store.completedRun)
		require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
		require.Equal(t, "killed", *store.completedRun.FailureRule)
		require.NotNil(t, store.completedRun.ErrorMessage)
		require.Contains(t, *store.completedRun.ErrorMessage, "exit code 137")
	})
}

// A stopping manager hands the operation it was following back to the next
// manager instead of failing it.
func TestProcessLeasedOperationDefersOnManagerShutdown(t *testing.T) {
	store, runtime, manager, runID := restartRunFixture(t)
	runtime.onRunAgent = func(ctx context.Context, _ *AgentObserver) (*AgentExecution, error) {
		<-ctx.Done()
		return nil, ErrAgentDetached
	}
	operation := restartOperation(store, runID)
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errManagerShutdown)
	manager.processLeasedOperation(ctx, *operation)
	deferred := store.deferredOperations()
	require.Len(t, deferred, 1)
	require.Equal(t, operation.ID, deferred[0].ID)
	require.Equal(t, "manager_restart", deferred[0].Code)
	require.Empty(t, store.failedOperationCode, "a restart never fails the operation")
	require.Nil(t, store.completedRun)
}

// checkRestartFixture is a queued check step with everything it needs to run,
// including a compose file whose cleanup would show up in the fake. The key
// decrypts the fixture's output snapshot.
func checkRestartFixture(t *testing.T) (*validationStoreFake, *validationRuntimeFake, *domain.DevMachine, *domain.DevMachineOperation, []byte) {
	t.Helper()
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	key := make([]byte, 32)
	encrypted, err := cryptoutil.Encrypt(`{}`, key)
	require.NoError(t, err)
	store := &validationStoreFake{
		managerStoreFake: &managerStoreFake{
			machine:  machine,
			agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, EncryptedOutputSecrets: &encrypted, MaxRuntimeSeconds: 60},
			checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready"},
			services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}},
		},
		step: domain.DevMachineAgentRunStep{ID: uuid.New(), StepType: manualCheckStepType, AgentRunID: runID, Status: "queued", CreatedAt: time.Now(), CommandArgv: []byte(`["test"]`), ComposeFile: "compose.yaml"},
	}
	runtime := &validationRuntimeFake{runtimeFake: &runtimeFake{}}
	operation := &domain.DevMachineOperation{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunCheck}
	return store, runtime, machine, operation, key
}

// A check that keeps running when the manager stops is handed to the next
// manager: its container and compose services stay, nothing is recorded and
// it is not cancelled.
func TestCheckIsHandedBackWhenTheManagerStops(t *testing.T) {
	store, runtime, machine, operation, key := checkRestartFixture(t)
	var observed *AgentObserver
	runtime.onRun = func(observer *AgentObserver) (*AgentExecution, error) {
		observed = observer
		return &AgentExecution{ContainerID: "check", ExitCode: -1}, ErrAgentDetached
	}
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key, detach: make(chan struct{})}
	err := manager.runManualCheck(context.Background(), machine, operation)
	require.ErrorIs(t, err, errManagerShutdown)
	require.NotNil(t, observed)
	require.NotNil(t, observed.Detach, "the check detaches like an agent attempt")
	require.Equal(t, (<-chan struct{})(manager.detach), observed.Detach)
	require.Nil(t, store.exit, "the next manager records the result")
	require.Zero(t, runtime.checkCleanups, "compose services stay for the next manager")
	require.Zero(t, runtime.agentCancellations)
	require.Empty(t, runtime.removedChecks)
}

// A stopping manager starts no check container at all: the next manager runs
// the command, or re-adopts the one already running.
func TestStoppingManagerStartsNoCheck(t *testing.T) {
	store, runtime, machine, operation, key := checkRestartFixture(t)
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key, detach: make(chan struct{})}
	manager.beginShutdown()
	require.ErrorIs(t, manager.runManualCheck(context.Background(), machine, operation), errManagerShutdown)
	require.Zero(t, runtime.calls, "no check container is started")
}

// A check handed back through a leased operation is deferred, never failed.
func TestHandedBackCheckIsDeferredNotFailed(t *testing.T) {
	store, runtime, _, operation, key := checkRestartFixture(t)
	runtime.onRun = func(*AgentObserver) (*AgentExecution, error) {
		return &AgentExecution{ContainerID: "check", ExitCode: -1}, ErrAgentDetached
	}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	manager.processLeasedOperation(context.Background(), *operation)
	deferred := store.deferredOperations()
	require.Len(t, deferred, 1)
	require.Equal(t, operation.ID, deferred[0].ID)
	require.Equal(t, "manager_restart", deferred[0].Code)
	require.Empty(t, store.failedOperationCode, "a restart never fails the operation")
	require.Nil(t, store.exit)
	require.Zero(t, runtime.checkCleanups)
}

// A stopping manager lets agent attempts keep running and returns promptly.
func TestManagerRunDetachesAgentRunsOnShutdown(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		drain time.Duration
		hook  func(context.Context, *AgentObserver) (*AgentExecution, error)
	}{
		{
			name: "detach",
			hook: func(_ context.Context, observer *AgentObserver) (*AgentExecution, error) {
				<-observer.Detach
				return nil, ErrAgentDetached
			},
		},
		{
			name:  "drain_timeout",
			drain: 50 * time.Millisecond,
			hook: func(ctx context.Context, _ *AgentObserver) (*AgentExecution, error) {
				<-ctx.Done()
				return nil, ErrAgentDetached
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, runtime, manager, runID := restartRunFixture(t)
			operation := restartOperation(store, runID)
			store.leased = []domain.DevMachineOperation{*operation}
			entered := make(chan struct{})
			var once sync.Once
			runtime.onRunAgent = func(ctx context.Context, observer *AgentObserver) (*AgentExecution, error) {
				once.Do(func() { close(entered) })
				return scenario.hook(ctx, observer)
			}
			manager.pollInterval = 10 * time.Millisecond
			if scenario.drain > 0 {
				manager.SetShutdownDrain(scenario.drain)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- manager.Run(ctx) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("the leased agent operation never started")
			}
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not return after the shutdown drain")
			}
			deferred := store.deferredOperations()
			require.Len(t, deferred, 1)
			require.Equal(t, operation.ID, deferred[0].ID)
			require.Equal(t, "manager_restart", deferred[0].Code)
			require.Empty(t, store.failedOperationCode, "a restart never fails the operation")
			require.Nil(t, store.completedRun)
		})
	}
}

func TestPreUpgradeValidationOnlyCleansUpItsRun(t *testing.T) {
	store, runtime, machine, operation, key := checkRestartFixture(t)
	run := store.agentRun
	run.MachineID, run.WorkspaceID, run.Status = machine.ID, machine.WorkspaceID, domain.DevMachineAgentRunStatusSucceeded
	operation.Action = domain.DevMachineOpValidateAgent
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key}
	require.NoError(t, manager.processOperation(t.Context(), operation))
	require.Zero(t, runtime.calls)
	require.Equal(t, 1, runtime.agentCancellations)
}
