package machine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
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
	terminalSession           *domain.DevMachineTerminalSession
	completedTerminalIDs      []uuid.UUID
	failedTerminalIDs         []uuid.UUID
	environment               *domain.DevMachineEnvironment
	deleteRequestedEnvs       []domain.DevMachineEnvironment
	deletedEnvironmentIDs     []uuid.UUID
	installationFullNameCalls []string
	githubInstallationID      int64
	agentRun                  *domain.DevMachineAgentRun
	provider                  *domain.DevMachineAgentProvider
	completedRun              *domain.DevMachineAgentRun
	runtimeCredentials        []domain.DevMachineRuntimeCredential
	purgedCredentialNow       *time.Time
	purgedAccessLogBefore     *time.Time
	purgedAccessLogLimit      int
	revokedMachineIDs         []uuid.UUID
	failedOperationCode       string
	failedOperationMessage    string
	failedOperationRetryable  *bool
	failOperationWillRetry    *bool
	completedOperations       int
	orphanedEnvironmentCount  int
	orphanReconcileCalls      int
	orphanedCheckoutCount     int
	checkoutReconcileCalls    int
	updateAgentRunStarted     func() error
	envVarListCalls           int
	runtimeServicesCreated    int
}

func (f *managerStoreFake) LeaseOperations(context.Context, string, int, time.Duration) ([]domain.DevMachineOperation, error) {
	return nil, nil
}
func (f *managerStoreFake) RenewOperationLease(context.Context, uuid.UUID, string, time.Duration) error {
	return nil
}
func (f *managerStoreFake) CompleteOperation(context.Context, uuid.UUID, string) error {
	f.completedOperations++
	return nil
}
func (f *managerStoreFake) FailOperation(_ context.Context, _ uuid.UUID, _ string, code, message string, retryable bool) (bool, error) {
	f.failedOperationCode = code
	f.failedOperationMessage = message
	f.failedOperationRetryable = &retryable
	if f.failOperationWillRetry != nil {
		return *f.failOperationWillRetry, nil
	}
	return retryable, nil
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
	f.envVarListCalls++
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
	f.runtimeServicesCreated++
	return nil
}
func (f *managerStoreFake) UpdateAgentRunStarted(context.Context, uuid.UUID) error {
	if f.updateAgentRunStarted != nil {
		return f.updateAgentRunStarted()
	}
	if f.agentRun != nil {
		f.agentRun.Status = domain.DevMachineAgentRunStatusRunning
	}
	return nil
}
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
func (f *managerStoreFake) RevokeMachineAccess(_ context.Context, machineID uuid.UUID) error {
	f.revokedMachineIDs = append(f.revokedMachineIDs, machineID)
	return nil
}
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
	return f.githubInstallationID, nil
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
func (f *managerStoreFake) GetTerminalSessionInternal(context.Context, uuid.UUID) (*domain.DevMachineTerminalSession, error) {
	return f.terminalSession, nil
}
func (f *managerStoreFake) CompleteTerminalSessionClose(_ context.Context, sessionID uuid.UUID) error {
	f.completedTerminalIDs = append(f.completedTerminalIDs, sessionID)
	if f.terminalSession != nil {
		f.terminalSession.Status = "closed"
	}
	return nil
}
func (f *managerStoreFake) FailTerminalSessionClose(_ context.Context, sessionID uuid.UUID) error {
	f.failedTerminalIDs = append(f.failedTerminalIDs, sessionID)
	if f.terminalSession != nil {
		f.terminalSession.Status = "close_failed"
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
func (f *managerStoreFake) ReconcileOrphanedEnvironments(context.Context, int) (int, error) {
	f.orphanReconcileCalls++
	return f.orphanedEnvironmentCount, nil
}
func (f *managerStoreFake) ReconcileOrphanedCheckouts(context.Context, int) (int, error) {
	f.checkoutReconcileCalls++
	return f.orphanedCheckoutCount, nil
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
func (f *managerStoreFake) UpsertRuntimeCredential(_ context.Context, credential *domain.DevMachineRuntimeCredential) error {
	f.runtimeCredentials = append(f.runtimeCredentials, *credential)
	return nil
}
func (f *managerStoreFake) PurgeExpiredRuntimeCredentials(_ context.Context, now time.Time) (int, error) {
	f.purgedCredentialNow = &now
	return 0, nil
}
func (f *managerStoreFake) PurgeAccessLogs(_ context.Context, before time.Time, limit int) (int, error) {
	f.purgedAccessLogBefore = &before
	f.purgedAccessLogLimit = limit
	return 0, nil
}

type githubAPIFake struct {
	token          string
	expiresAt      time.Time
	installationID int64
	repository     string
}

func (f *githubAPIFake) GetRepositoryInstallationToken(installationID int64, repository string) (string, time.Time, error) {
	f.installationID = installationID
	f.repository = repository
	return f.token, f.expiresAt, nil
}

func (f *githubAPIFake) CreatePullRequest(string, string, string, string, string, string, string) (*githubclient.PullRequest, error) {
	return &githubclient.PullRequest{HTMLURL: "https://github.com/kuayle/api/pull/1", Number: 1}, nil
}

type runtimeFake struct {
	spawned, paused, stopped, tornDown int
	snapshots                          int
	agentRuns                          int
	terminatedTerminals                int
	terminatedSession                  *domain.DevMachineTerminalSession
	terminateTerminalErr               error
	deletedEnvironmentIDs              []uuid.UUID
	deleteEnvironmentImageErr          error
	environmentDeleteAttempts          int
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
	f.agentRuns++
	return f.agentExecution, nil
}
func (f *runtimeFake) CancelAgent(context.Context, *domain.DevMachineAgentRun) error { return nil }
func (f *runtimeFake) TerminateTerminal(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, session *domain.DevMachineTerminalSession) error {
	f.terminatedTerminals++
	f.terminatedSession = session
	return f.terminateTerminalErr
}
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
	f.snapshots++
	return "sha256:test", nil
}
func (f *runtimeFake) DeleteEnvironmentImage(_ context.Context, environment *domain.DevMachineEnvironment) error {
	f.environmentDeleteAttempts++
	if f.deleteEnvironmentImageErr != nil {
		return f.deleteEnvironmentImageErr
	}
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

func TestManagerDoesNotLaunchAgentCancelledBeforeStart(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID,
		ProviderID: "opencode", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
		AllowedSecrets: json.RawMessage(`["API_KEY"]`), MaxRuntimeSeconds: 30,
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
	store.updateAgentRunStarted = func() error {
		store.agentRun.Status = domain.DevMachineAgentRunStatusCancelled
		return sql.ErrNoRows
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), nil, "test")

	err := manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID,
		Action: domain.DevMachineOpRunAgent, Generation: 1,
	})

	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStatusCancelled, store.agentRun.Status)
	require.Zero(t, store.envVarListCalls)
	require.Zero(t, store.runtimeServicesCreated)
	require.Zero(t, runtime.agentRuns)
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

func TestManagerPauseRevokesMachineAccess(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	containerID := "container-ide"
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusPaused, Generation: 3,
		},
		services: []domain.DevMachineService{{
			ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide",
			ContainerID: &containerID, Status: "running",
		}},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), make([]byte, 32), "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpPause, Generation: 3,
	})

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{machineID}, store.revokedMachineIDs)
	require.Equal(t, 1, runtime.paused)
	require.Equal(t, domain.DevMachineStatusPaused, store.machine.Status)
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

func TestManagerKeepsCheckoutPreparingWhileOperationRetries(t *testing.T) {
	machineID, workspaceID, checkoutID := uuid.New(), uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 2},
		checkout: &domain.DevMachineCheckout{ID: checkoutID, WorkspaceID: workspaceID, MachineID: machineID, RepositoryFullName: "selected/repo", Status: "queued"},
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")

	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	manager.processLeasedOperation(context.Background(), domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpCheckoutIssue,
		CheckoutID: &checkoutID, Generation: 2,
	})

	require.Equal(t, "preparing", store.checkoutStatus)
	require.Nil(t, store.checkoutError)
	require.NotNil(t, store.failedOperationRetryable)
	require.True(t, *store.failedOperationRetryable)
}

func TestManagerTerminatesStaleTerminalSessionAndCompletesClose(t *testing.T) {
	machineID, workspaceID, sessionID := uuid.New(), uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning,
			DesiredStatus: domain.DevMachineStatusRunning, Generation: 4,
		},
		terminalSession: &domain.DevMachineTerminalSession{
			ID: sessionID, MachineID: machineID, WorkspaceID: workspaceID, RuntimeSessionName: "term-test", Status: "closing",
		},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpTerminateTerminal,
		TerminalSessionID: &sessionID, Generation: 3,
	})

	require.NoError(t, err)
	require.Equal(t, 1, runtime.terminatedTerminals)
	require.Same(t, store.terminalSession, runtime.terminatedSession)
	require.Equal(t, []uuid.UUID{sessionID}, store.completedTerminalIDs)
	require.Equal(t, "closed", store.terminalSession.Status)
}

func TestManagerTerminalCloseFailureRemainsExplicitAfterRetriesExhausted(t *testing.T) {
	machineID, workspaceID, sessionID := uuid.New(), uuid.New(), uuid.New()
	willRetry := false
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning,
			DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		terminalSession: &domain.DevMachineTerminalSession{
			ID: sessionID, MachineID: machineID, WorkspaceID: workspaceID, RuntimeSessionName: "term-test", Status: "closing",
		},
		failOperationWillRetry: &willRetry,
	}
	runtime := &runtimeFake{terminateTerminalErr: errors.New("docker unavailable")}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)

	manager.processLeasedOperation(context.Background(), domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpTerminateTerminal,
		TerminalSessionID: &sessionID, Generation: 1,
	})

	require.NotNil(t, store.failedOperationRetryable)
	require.True(t, *store.failedOperationRetryable)
	require.Equal(t, []uuid.UUID{sessionID}, store.failedTerminalIDs)
	require.Equal(t, "close_failed", store.terminalSession.Status)
	require.Empty(t, store.completedTerminalIDs)
}

func TestRepositoryTokenRegistersRuntimeCredentialBeforeReturning(t *testing.T) {
	machineID, workspaceID := uuid.New(), uuid.New()
	token := "ghs_" + strings.Repeat("runtime-token", 4)
	expiresAt := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	key := cryptoutil.DeriveKey("manager-runtime-credential-test")
	store := &managerStoreFake{
		machine:              &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RepoOwner: "kuayle", RepoName: "api"},
		githubInstallationID: 42,
	}
	github := &githubAPIFake{token: token, expiresAt: expiresAt}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), github, key, nil, "test")

	client, returnedToken, err := manager.repositoryToken(context.Background(), store.machine, "kuayle/api")

	require.NoError(t, err)
	require.Same(t, github, client)
	require.Equal(t, token, returnedToken)
	require.Equal(t, int64(42), github.installationID)
	require.Equal(t, "api", github.repository)
	require.Len(t, store.runtimeCredentials, 1)
	credential := store.runtimeCredentials[0]
	require.Equal(t, machineID, credential.MachineID)
	require.Equal(t, domain.DevMachineRuntimeCredentialScopeMachine, credential.Scope)
	require.Equal(t, domain.DevMachineRuntimeCredentialTypeGitHubToken, credential.CredentialType)
	require.Equal(t, expiresAt, credential.ExpiresAt)
	require.NotEmpty(t, credential.EncryptedValue)
	require.NotEqual(t, token, credential.EncryptedValue)
	decrypted, err := cryptoutil.Decrypt(credential.EncryptedValue, key)
	require.NoError(t, err)
	require.Equal(t, token, decrypted)
	fingerprint := sha256.Sum256([]byte(token))
	require.Equal(t, hex.EncodeToString(fingerprint[:]), credential.FingerprintSHA256)
	encoded, err := json.Marshal(credential)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), credential.EncryptedValue)
	require.NotContains(t, string(encoded), credential.FingerprintSHA256)
	require.NotContains(t, string(encoded), token)
}

func TestReconcilePurgesExpiredRuntimeCredentials(t *testing.T) {
	store := &managerStoreFake{orphanedEnvironmentCount: 2, orphanedCheckoutCount: 3}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.NotNil(t, store.purgedCredentialNow)
	require.NotNil(t, store.purgedAccessLogBefore)
	require.WithinDuration(t, store.purgedCredentialNow.Add(-gatewayAccessLogRetention), *store.purgedAccessLogBefore, time.Second)
	require.Equal(t, gatewayAccessLogPurgeBatch, store.purgedAccessLogLimit)
	require.Equal(t, 1, store.orphanReconcileCalls)
	require.Equal(t, 1, store.checkoutReconcileCalls)
}

func TestReconcileRetriesEnvironmentImageCleanupFailure(t *testing.T) {
	environmentID, workspaceID := uuid.New(), uuid.New()
	store := &managerStoreFake{deleteRequestedEnvs: []domain.DevMachineEnvironment{{
		ID: environmentID, WorkspaceID: workspaceID, Status: "delete_requested", ImageRef: "kuayle/environment:test",
	}}}
	runtime := &runtimeFake{deleteEnvironmentImageErr: errors.New("image is busy")}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, 1, runtime.environmentDeleteAttempts)
	require.Empty(t, store.deletedEnvironmentIDs)

	runtime.deleteEnvironmentImageErr = nil
	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, 2, runtime.environmentDeleteAttempts)
	require.Equal(t, []uuid.UUID{environmentID}, store.deletedEnvironmentIDs)
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

func TestManagerRejectsRepositoryAgentRunWithoutCheckout(t *testing.T) {
	machineID, workspaceID, runID, repositoryID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{
		ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepositoryAffinityID: &repositoryID,
	}
	store := &managerStoreFake{
		machine: machine,
		agentRun: &domain.DevMachineAgentRun{
			ID: runID, MachineID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineAgentRunStatusQueued,
		},
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")

	err := manager.runAgent(context.Background(), machine, &domain.DevMachineOperation{AgentRunID: &runID, Generation: 1})

	var terminalErr *terminalOperationError
	require.ErrorAs(t, err, &terminalErr)
	require.Equal(t, "checkout_not_ready", terminalErr.code)
	require.Zero(t, runtime.agentRuns)
	require.Zero(t, store.envVarListCalls)
}

func TestManagerSnapshotEnvironmentTransitionsThroughBuilding(t *testing.T) {
	machineID, workspaceID, environmentID := uuid.New(), uuid.New(), uuid.New()
	environment := &domain.DevMachineEnvironment{ID: environmentID, WorkspaceID: workspaceID, Name: "base", ImageRef: "kuayle/dev-environment-test:snapshot", Status: "pending"}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusPaused, DesiredStatus: domain.DevMachineStatusPaused, Generation: 3,
		},
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

func TestManagerFailsGenerationMismatchedSnapshotsWithoutRetry(t *testing.T) {
	for _, generation := range []int64{2, 4} {
		t.Run(fmt.Sprintf("operation generation %d", generation), func(t *testing.T) {
			machineID, workspaceID, environmentID := uuid.New(), uuid.New(), uuid.New()
			environment := &domain.DevMachineEnvironment{
				ID: environmentID, WorkspaceID: workspaceID, Name: "base",
				ImageRef: "kuayle/dev-environment-test:snapshot", Status: "pending",
			}
			store := &managerStoreFake{
				machine: &domain.DevMachine{
					ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusPaused,
					DesiredStatus: domain.DevMachineStatusPaused, Generation: 3,
				},
				environment: environment,
			}
			runtime := &runtimeFake{}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")
			operation := domain.DevMachineOperation{
				ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID,
				Action: domain.DevMachineOpSnapshotEnvironment, EnvironmentID: &environmentID,
				Status: domain.DevMachineOpStatusLeased, Generation: generation, MaxAttempts: 2,
			}
			manager.operationSlots <- struct{}{}
			manager.operations.Add(1)

			manager.processLeasedOperation(context.Background(), operation)

			require.Equal(t, "failed", environment.Status)
			require.Zero(t, runtime.snapshots)
			require.NotNil(t, store.failedOperationRetryable)
			require.False(t, *store.failedOperationRetryable)
			require.Equal(t, "environment_snapshot_stale", store.failedOperationCode)
			require.Contains(t, store.failedOperationMessage, "machine generation changed")
			require.Zero(t, store.completedOperations)
		})
	}
}

func TestManagerFailsSnapshotWhenMachineStateChanged(t *testing.T) {
	machineID, workspaceID, environmentID := uuid.New(), uuid.New(), uuid.New()
	environment := &domain.DevMachineEnvironment{
		ID: environmentID, WorkspaceID: workspaceID, Name: "base",
		ImageRef: "kuayle/dev-environment-test:snapshot", Status: "building",
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusPaused,
			DesiredStatus: domain.DevMachineStatusRunning, Generation: 3,
		},
		environment: environment,
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, make([]byte, 32), nil, "test")
	operation := domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpSnapshotEnvironment, EnvironmentID: &environmentID,
		Status: domain.DevMachineOpStatusLeased, Generation: 3, MaxAttempts: 2,
	}
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)

	manager.processLeasedOperation(context.Background(), operation)

	require.Equal(t, "failed", environment.Status)
	require.Zero(t, runtime.snapshots)
	require.NotNil(t, store.failedOperationRetryable)
	require.False(t, *store.failedOperationRetryable)
	require.Equal(t, "environment_snapshot_state_changed", store.failedOperationCode)
	require.Contains(t, store.failedOperationMessage, "desired state running")
	require.Zero(t, store.completedOperations)
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
