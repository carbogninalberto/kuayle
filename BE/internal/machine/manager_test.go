package machine

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

type managerStoreFake struct {
	machine                   *domain.DevMachine
	services                  []domain.DevMachineService
	states                    []domain.DevMachineStatus
	events                    []domain.DevMachineEvent
	idle                      []domain.DevMachine
	permanentDeleteMachines   []domain.DevMachine
	permanentDeleteRequests   int
	purgedMachineIDs          []uuid.UUID
	desired                   []domain.DevMachineStatus
	actions                   []domain.DevMachineOperationAction
	checkout                  *domain.DevMachineCheckout
	checkoutStatus            string
	checkoutError             *string
	environment               *domain.DevMachineEnvironment
	deleteRequestedEnvs       []domain.DevMachineEnvironment
	deletedEnvironmentIDs     []uuid.UUID
	installationFullNameCalls []string
	agentRun                  *domain.DevMachineAgentRun
	provider                  *domain.DevMachineAgentProvider
	completedRun              *domain.DevMachineAgentRun
}

func (f *managerStoreFake) LeaseOperations(context.Context, string, int, time.Duration) ([]domain.DevMachineOperation, error) {
	return nil, nil
}
func (f *managerStoreFake) RenewOperationLease(context.Context, uuid.UUID, string, time.Duration) error {
	return nil
}
func (f *managerStoreFake) CompleteOperation(context.Context, uuid.UUID, string) error { return nil }
func (f *managerStoreFake) FailOperation(context.Context, uuid.UUID, string, string, string) (bool, error) {
	return false, nil
}
func (f *managerStoreFake) GetMachineInternal(context.Context, uuid.UUID) (*domain.DevMachine, error) {
	return f.machine, nil
}
func (f *managerStoreFake) GetProvider(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineAgentProvider, error) {
	return f.provider, nil
}
func (f *managerStoreFake) GetAgentRunInternal(context.Context, uuid.UUID) (*domain.DevMachineAgentRun, error) {
	return f.agentRun, nil
}
func (f *managerStoreFake) ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error) {
	return f.services, nil
}
func (f *managerStoreFake) ListEnvVarsInternal(context.Context, uuid.UUID, *string, string) ([]domain.DevMachineEnvVar, error) {
	return nil, nil
}
func (f *managerStoreFake) SetMachineState(_ context.Context, _ uuid.UUID, status domain.DevMachineStatus, network, volume, _, _ *string) error {
	f.machine.Status = status
	if network != nil {
		f.machine.DockerNetworkName = network
	}
	if volume != nil {
		f.machine.WorkspaceVolumeName = volume
	}
	f.states = append(f.states, status)
	return nil
}
func (f *managerStoreFake) SetMachineStateForOperation(_ context.Context, _ uuid.UUID, generation int64, allowStale bool, status domain.DevMachineStatus, network, volume, _, _ *string) (bool, error) {
	if !allowStale && f.machine.Generation != generation {
		return false, nil
	}
	_ = f.SetMachineState(context.Background(), f.machine.ID, status, network, volume, nil, nil)
	return true, nil
}
func (f *managerStoreFake) UpdateServiceRuntime(_ context.Context, serviceID uuid.UUID, containerID, status, health string, _ *string) error {
	for i := range f.services {
		if f.services[i].ID == serviceID {
			f.services[i].ContainerID = &containerID
			f.services[i].Status = status
			f.services[i].HealthStatus = health
		}
	}
	return nil
}
func (f *managerStoreFake) CreateRuntimeService(context.Context, *domain.DevMachineService) error {
	return nil
}
func (f *managerStoreFake) UpdateAgentRunStarted(context.Context, uuid.UUID) error { return nil }
func (f *managerStoreFake) CompleteAgentRun(_ context.Context, run *domain.DevMachineAgentRun) error {
	copy := *run
	f.completedRun = &copy
	return nil
}
func (f *managerStoreFake) CreateEvent(_ context.Context, event *domain.DevMachineEvent) error {
	f.events = append(f.events, *event)
	return nil
}
func (f *managerStoreFake) CreateLogChunk(context.Context, *domain.DevMachineLogChunk) error {
	return nil
}
func (f *managerStoreFake) RevokeMachineAccess(context.Context, uuid.UUID) error { return nil }
func (f *managerStoreFake) ListExpiredMachines(context.Context, int) ([]domain.DevMachine, error) {
	return nil, nil
}
func (f *managerStoreFake) ListTimedOutAgentRuns(context.Context, int) ([]domain.DevMachineAgentRun, error) {
	return nil, nil
}
func (f *managerStoreFake) ListRuntimeMachines(context.Context, int, int) ([]domain.DevMachine, error) {
	return nil, nil
}

func (f *managerStoreFake) SetDesiredAndEnqueue(_ context.Context, _ uuid.UUID, _ uuid.UUID, desired domain.DevMachineStatus, operation *domain.DevMachineOperation) error {
	f.desired = append(f.desired, desired)
	f.actions = append(f.actions, operation.Action)
	return nil
}

func (f *managerStoreFake) RequestPermanentDelete(_ context.Context, workspaceID, machineID uuid.UUID, _ *uuid.UUID) (*domain.DevMachineOperation, error) {
	f.permanentDeleteRequests++
	var machine *domain.DevMachine
	if f.machine != nil && f.machine.ID == machineID && f.machine.WorkspaceID == workspaceID {
		machine = f.machine
	} else {
		for i := range f.permanentDeleteMachines {
			if f.permanentDeleteMachines[i].ID == machineID && f.permanentDeleteMachines[i].WorkspaceID == workspaceID {
				machine = &f.permanentDeleteMachines[i]
				break
			}
		}
	}
	if machine == nil {
		return nil, nil
	}
	if machine.DeleteRequestedAt == nil {
		now := time.Now().UTC()
		machine.DeleteRequestedAt = &now
	}
	if domain.DevMachineSafelyPurgeable(machine) {
		return nil, nil
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpTeardown, Status: domain.DevMachineOpStatusPending,
		Generation: machine.Generation + 1, IdempotencyKey: "permanent-delete:test", MaxAttempts: 10,
	}
	f.desired = append(f.desired, domain.DevMachineStatusDestroyed)
	f.actions = append(f.actions, operation.Action)
	machine.DesiredStatus = domain.DevMachineStatusDestroyed
	machine.Generation = operation.Generation
	return operation, nil
}

func (f *managerStoreFake) ListPermanentDeleteRequests(context.Context, int) ([]domain.DevMachine, error) {
	return f.permanentDeleteMachines, nil
}

func (f *managerStoreFake) PurgePermanentDeleteRequest(_ context.Context, _ uuid.UUID, machineID uuid.UUID) error {
	f.purgedMachineIDs = append(f.purgedMachineIDs, machineID)
	return nil
}

func (f *managerStoreFake) CreateResourceSample(context.Context, *domain.DevMachineResourceSample) error {
	return nil
}
func (f *managerStoreFake) UpdateVolumeUsage(context.Context, uuid.UUID, int64) error    { return nil }
func (f *managerStoreFake) CreateGitRef(context.Context, *domain.DevMachineGitRef) error { return nil }
func (f *managerStoreFake) GetGitHubInstallationID(_ context.Context, _ uuid.UUID, fullName string) (int64, error) {
	f.installationFullNameCalls = append(f.installationFullNameCalls, fullName)
	return 0, nil
}
func (f *managerStoreFake) GetGitHubAppConfig(context.Context, uuid.UUID) (*domain.GitHubAppConfig, error) {
	return nil, nil
}
func (f *managerStoreFake) GetCheckoutInternal(context.Context, uuid.UUID) (*domain.DevMachineCheckout, error) {
	return f.checkout, nil
}
func (f *managerStoreFake) UpdateCheckoutState(_ context.Context, _ uuid.UUID, status string, lastError *string) error {
	f.checkoutStatus = status
	f.checkoutError = lastError
	if f.checkout != nil {
		f.checkout.Status = status
		f.checkout.LastError = lastError
	}
	return nil
}
func (f *managerStoreFake) GetEnvironment(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineEnvironment, error) {
	return f.environment, nil
}
func (f *managerStoreFake) UpdateEnvironmentState(_ context.Context, _ uuid.UUID, status, imageRef string, digest *string) error {
	if f.environment != nil {
		f.environment.Status = status
		if imageRef != "" {
			f.environment.ImageRef = imageRef
		}
		f.environment.ImageDigest = digest
	}
	return nil
}
func (f *managerStoreFake) ListDeleteRequestedEnvironments(context.Context, int) ([]domain.DevMachineEnvironment, error) {
	return f.deleteRequestedEnvs, nil
}
func (f *managerStoreFake) DeleteEnvironment(_ context.Context, _ uuid.UUID, environmentID uuid.UUID) error {
	f.deletedEnvironmentIDs = append(f.deletedEnvironmentIDs, environmentID)
	return nil
}
func (f *managerStoreFake) ListIdleMachines(context.Context, int) ([]domain.DevMachine, error) {
	return f.idle, nil
}

type runtimeFake struct {
	spawned, paused, stopped, tornDown int
	deletedEnvironmentIDs              []uuid.UUID
	onSpawn                            func()
	agentExecution                     *AgentExecution
}

func (f *runtimeFake) Spawn(_ context.Context, _ *domain.DevMachine, services []domain.DevMachineService, _ map[string]map[string]string) (string, string, map[string]string, error) {
	f.spawned++
	if f.onSpawn != nil {
		f.onSpawn()
	}
	containers := make(map[string]string)
	for _, service := range services {
		containers[service.ServiceKey] = "container-" + service.ServiceKey
	}
	return "machine-network", "workspace-volume", containers, nil
}
func (f *runtimeFake) Start(context.Context, *domain.DevMachine, []domain.DevMachineService, map[string]map[string]string) error {
	return nil
}
func (f *runtimeFake) Pause(context.Context, *domain.DevMachine, []domain.DevMachineService) error {
	f.paused++
	return nil
}
func (f *runtimeFake) Stop(context.Context, *domain.DevMachine, []domain.DevMachineService) error {
	f.stopped++
	return nil
}
func (f *runtimeFake) Teardown(context.Context, *domain.DevMachine, []domain.DevMachineService) error {
	f.tornDown++
	return nil
}
func (f *runtimeFake) RunAgent(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentProvider, *domain.DevMachineCheckout, map[string]string) (*AgentExecution, error) {
	return f.agentExecution, nil
}
func (f *runtimeFake) CancelAgent(context.Context, *domain.DevMachineAgentRun) error { return nil }
func (f *runtimeFake) Inspect(context.Context, *domain.DevMachine, []domain.DevMachineService) (RuntimeInspection, error) {
	return RuntimeInspection{NetworkExists: true, VolumeExists: true, GatewayAttached: true}, nil
}
func (f *runtimeFake) Stats(context.Context, *domain.DevMachine, []domain.DevMachineService) (ResourceUsage, error) {
	return ResourceUsage{}, nil
}
func (f *runtimeFake) GitState(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout) (GitState, error) {
	return GitState{}, nil
}
func (f *runtimeFake) PrepareCheckout(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout, string) error {
	return nil
}
func (f *runtimeFake) SnapshotEnvironment(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineEnvironment) (string, error) {
	return "sha256:test", nil
}
func (f *runtimeFake) DeleteEnvironmentImage(_ context.Context, environment *domain.DevMachineEnvironment) error {
	f.deletedEnvironmentIDs = append(f.deletedEnvironmentIDs, environment.ID)
	return nil
}
func (f *runtimeFake) Ping(context.Context) error { return nil }

func TestAgentToolchainFailureOverridesOuterSuccess(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID,
		ProviderID: "opencode", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
		MaxRuntimeSeconds: 30,
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		agentRun: run,
		provider: &domain.DevMachineAgentProvider{
			MachineID: machineID, ProviderID: "opencode", ImageRef: "kuayle/opencode:test", Enabled: true,
		},
	}
	runtime := &runtimeFake{agentExecution: &AgentExecution{
		ContainerID: "agent-container", ExitCode: 0,
		Stdout: `{"type":"tool_use","part":{"type":"tool","tool":"bash","state":{"status":"completed","title":"Build project","output":"go: command not found\n","metadata":{"exit":127}}}}` + "\n" +
			`{"type":"text","part":{"type":"text","text":"Unable to verify the build"}}`,
	}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), make([]byte, 32), "test")

	err := manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID,
		Action: domain.DevMachineOpRunAgent, Generation: 1,
	})

	require.NoError(t, err)
	require.NotNil(t, store.completedRun)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.Equal(t, "Unable to verify the build", *store.completedRun.Summary)
	require.Contains(t, string(store.completedRun.RiskNotes), "Build project")
}

func TestReconcileQueuesIdlePause(t *testing.T) {
	machine := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 4,
	}
	store := &managerStoreFake{machine: &machine, idle: []domain.DevMachine{machine}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, []domain.DevMachineStatus{domain.DevMachineStatusPaused}, store.desired)
	require.Equal(t, []domain.DevMachineOperationAction{domain.DevMachineOpPause}, store.actions)
}

func TestManagerLifecycleSpawnPauseAndTeardown(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusQueued},
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), make([]byte, 32), "test")
	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpSpawn})
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status)
	require.Equal(t, 1, runtime.spawned)
	err = manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpPause})
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineStatusPaused, store.machine.Status)
	err = manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpTeardown})
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineStatusDestroyed, store.machine.Status)
	require.Equal(t, 1, runtime.tornDown)
}

func TestManagerSkipsStaleOperationGeneration(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusQueued, Generation: 2},
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), make([]byte, 32), "test")
	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpSpawn, Generation: 1,
	})
	require.NoError(t, err)
	require.Zero(t, runtime.spawned)
}

func TestManagerStartResumesSpawnAfterRetryableFailure(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusSpawning},
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), make([]byte, 32), "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpStart,
	})

	require.NoError(t, err)
	require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status)
	require.Equal(t, 1, runtime.spawned)
}

func TestManagerCheckoutTokenUsesCheckoutRepository(t *testing.T) {
	machineID, workspaceID, checkoutID := uuid.New(), uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, Generation: 2, RepoOwner: "legacy", RepoName: "old"},
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
		checkout: &domain.DevMachineCheckout{ID: checkoutID, WorkspaceID: workspaceID, MachineID: machineID, RepositoryFullName: "selected/repo", BaseBranch: "main", WorkingBranch: "kuayle/test", WorkspacePath: "/workspace/tasks/eng-1", Status: "queued"},
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpCheckoutIssue, CheckoutID: &checkoutID, Generation: 2})

	require.Error(t, err)
	require.Equal(t, []string{"selected/repo"}, store.installationFullNameCalls)
}

func TestManagerMarksStaleCheckoutFailed(t *testing.T) {
	machineID, workspaceID, checkoutID := uuid.New(), uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, Generation: 2},
		checkout: &domain.DevMachineCheckout{ID: checkoutID, Status: "queued"},
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpCheckoutIssue, CheckoutID: &checkoutID, Generation: 1})

	require.NoError(t, err)
	require.Equal(t, "failed", store.checkoutStatus)
	require.NotNil(t, store.checkoutError)
	require.Contains(t, *store.checkoutError, "state changed")
}

func TestManagerSnapshotEnvironmentTransitionsThroughBuilding(t *testing.T) {
	machineID, workspaceID, environmentID := uuid.New(), uuid.New(), uuid.New()
	environment := &domain.DevMachineEnvironment{ID: environmentID, WorkspaceID: workspaceID, Name: "base", ImageRef: "kuayle/dev-environment-test:snapshot", Status: "pending"}
	store := &managerStoreFake{
		machine:     &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusPaused, Generation: 3},
		services:    []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
		environment: environment,
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpSnapshotEnvironment, EnvironmentID: &environmentID, Generation: 3})

	require.NoError(t, err)
	require.Equal(t, "ready", environment.Status)
	require.NotNil(t, environment.ImageDigest)
	require.Equal(t, "sha256:test", *environment.ImageDigest)
	require.Equal(t, "sha256:test", environment.ImageRef)
}

func TestManagerSnapshotRetryDoesNotUndoEnvironmentDeletion(t *testing.T) {
	machineID, workspaceID, environmentID := uuid.New(), uuid.New(), uuid.New()
	environment := &domain.DevMachineEnvironment{
		ID: environmentID, WorkspaceID: workspaceID, Name: "base",
		ImageRef: "kuayle/dev-environment-test:snapshot", Status: "delete_requested",
	}
	store := &managerStoreFake{
		machine:     &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusPaused, Generation: 3},
		services:    []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
		environment: environment,
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpSnapshotEnvironment,
		EnvironmentID: &environmentID, Generation: 3,
	})

	require.NoError(t, err)
	require.Equal(t, "delete_requested", environment.Status)
	require.Nil(t, environment.ImageDigest)
}

func TestReconcileDeletesRequestedEnvironmentImagesBeforeRecords(t *testing.T) {
	environment := domain.DevMachineEnvironment{ID: uuid.New(), WorkspaceID: uuid.New(), ImageRef: "kuayle/dev-environment-test:snapshot", Status: "delete_requested"}
	store := &managerStoreFake{deleteRequestedEnvs: []domain.DevMachineEnvironment{environment}}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, []uuid.UUID{environment.ID}, runtime.deletedEnvironmentIDs)
	require.Equal(t, []uuid.UUID{environment.ID}, store.deletedEnvironmentIDs)
}

func TestReconcilePermanentDeletePurgesOnlySafeRowsAndQueuesUnsafeRows(t *testing.T) {
	now := time.Now().UTC()
	networkName := "machine-network"
	volumeName := "workspace-volume"
	running := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 3, DeleteRequestedAt: &now,
		DockerNetworkName: &networkName, WorkspaceVolumeName: &volumeName,
	}
	destroyed := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusDestroyed,
		DesiredStatus: domain.DevMachineStatusDestroyed, Generation: 4, DeleteRequestedAt: &now,
	}
	failedWithResources := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusFailed,
		DesiredStatus: domain.DevMachineStatusFailed, Generation: 5, DeleteRequestedAt: &now,
		DockerNetworkName: &networkName, WorkspaceVolumeName: &volumeName,
	}
	store := &managerStoreFake{permanentDeleteMachines: []domain.DevMachine{running, destroyed, failedWithResources}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, []uuid.UUID{destroyed.ID}, store.purgedMachineIDs)
	require.Equal(t, 2, store.permanentDeleteRequests)
	require.Equal(t, []domain.DevMachineStatus{domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed}, store.desired)
	require.Equal(t, []domain.DevMachineOperationAction{domain.DevMachineOpTeardown, domain.DevMachineOpTeardown}, store.actions)
}

func TestManagerDoesNotPublishRunningAfterGenerationChanges(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusQueued, Generation: 1},
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide"}},
	}
	runtime := &runtimeFake{onSpawn: func() {
		store.machine.Generation = 2
		store.machine.DesiredStatus = domain.DevMachineStatusDestroyed
	}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), make([]byte, 32), "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpSpawn, Generation: 1,
	})

	require.NoError(t, err)
	require.NotEqual(t, domain.DevMachineStatusRunning, store.machine.Status)
	require.Equal(t, 1, runtime.tornDown)
}

func TestWorkspaceMountDisablesImageCopyUp(t *testing.T) {
	runtime := &DockerRuntime{}
	machine := &domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512}

	hostConfig := runtime.secureHostConfig(machine, "ide", "machine-network", "workspace-volume")

	require.Len(t, hostConfig.Mounts, 1)
	require.NotNil(t, hostConfig.Mounts[0].VolumeOptions)
	require.True(t, hostConfig.Mounts[0].VolumeOptions.NoCopy)
	require.Equal(t, "json-file", hostConfig.LogConfig.Type)
	require.Equal(t, "10m", hostConfig.LogConfig.Config["max-size"])
}

func TestOwnedByMachineRequiresManagedLabel(t *testing.T) {
	machineID := uuid.New()
	require.True(t, ownedByMachine(map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": machineID.String()}, machineID))
	require.False(t, ownedByMachine(map[string]string{"com.kuayle.machine-id": machineID.String()}, machineID))
	require.False(t, ownedByMachine(map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": uuid.NewString()}, machineID))
}

func TestUniqueContainerIDsDeduplicatesSharedDeveloperServices(t *testing.T) {
	containerID := "developer-container"
	ids := uniqueContainerIDs([]domain.DevMachineService{
		{ServiceType: "ide", ServiceKey: "ide", ContainerID: &containerID},
		{ServiceType: "terminal", ServiceKey: "terminal", ContainerID: &containerID},
	})
	require.Equal(t, []string{containerID}, ids)
}
