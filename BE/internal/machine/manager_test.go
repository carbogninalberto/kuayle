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
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

type managerStoreFake struct {
	ManagerStore
	signIns                   map[string]string
	envVars                   []domain.DevMachineEnvVar
	onAuthorizeAgentRun       func() error
	outputChunks              []domain.DevMachineLogChunk
	outputErr                 error
	authorizeAgentRunErr      error
	githubAppConfigCalls      int
	machine                   *domain.DevMachine
	services                  []domain.DevMachineService
	states                    []domain.DevMachineStatus
	events                    []domain.DevMachineEvent
	idle                      []domain.DevMachine
	runtimeMachines           []domain.DevMachine
	permanentDeleteMachines   []domain.DevMachine
	permanentDeleteRequests   int
	purgedMachineIDs          []uuid.UUID
	desired                   []domain.DevMachineStatus
	actions                   []domain.DevMachineOperationAction
	operations                []*domain.DevMachineOperation
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
	markAgentRunStarting      func() error
	updateAgentRunStarted     func() error
	envVarListCalls           int
	runtimeServicesCreated    int
	modelServer               *domain.AgentModelServer
	reclaimable               *domain.DevMachine
	reclaimWaitingIDs         []uuid.UUID
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
	return f.envVars, nil
}
func (f *managerStoreFake) GetModelServer(context.Context, uuid.UUID, *uuid.UUID) (*domain.AgentModelServer, error) {
	return f.modelServer, nil
}
func (f *managerStoreFake) GetAgentSignIn(_ context.Context, _ uuid.UUID, user uuid.UUID, provider string) (string, error) {
	return f.signIns[user.String()+"/"+provider], nil
}
func (f *managerStoreFake) SaveAgentSignIn(_ context.Context, _ uuid.UUID, user uuid.UUID, provider, encrypted string) error {
	if f.signIns == nil {
		f.signIns = map[string]string{}
	}
	f.signIns[user.String()+"/"+provider] = encrypted
	return nil
}

// deepSeekServer is the model server migrated from a workspace DeepSeek key.
func deepSeekServer(workspaceID uuid.UUID, encrypted, model, thinking string) *domain.AgentModelServer {
	return &domain.AgentModelServer{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "DeepSeek", EncryptedAPIKey: encrypted,
		OpenAIBaseURL: ptr("https://api.deepseek.com"), AnthropicBaseURL: ptr("https://api.deepseek.com/anthropic"),
		Models: json.RawMessage(`["deepseek-flash","deepseek-v4-pro"]`), DefaultModel: model, FastModel: "deepseek-flash",
		Thinking: thinking, IsDefault: true,
	}
}

func serverRef() *uuid.UUID {
	id := uuid.New()
	return &id
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
func (f *managerStoreFake) AuthorizeAgentRun(context.Context, uuid.UUID) error {
	if f.onAuthorizeAgentRun != nil {
		return f.onAuthorizeAgentRun()
	}
	return f.authorizeAgentRunErr
}
func (f *managerStoreFake) SealAgentRunSecrets(_ context.Context, _ uuid.UUID, encrypted string) (string, error) {
	if f.agentRun.EncryptedOutputSecrets != nil {
		return *f.agentRun.EncryptedOutputSecrets, nil
	}
	f.agentRun.EncryptedOutputSecrets = &encrypted
	return encrypted, nil
}
func (f *managerStoreFake) MarkAgentRunStarting(context.Context, uuid.UUID) error {
	if f.markAgentRunStarting != nil {
		return f.markAgentRunStarting()
	}
	if f.agentRun != nil && f.agentRun.Status == domain.DevMachineAgentRunStatusQueued {
		f.agentRun.Status = domain.DevMachineAgentRunStatusStarting
	}
	return nil
}
func (f *managerStoreFake) UpdateAgentRunStarted(context.Context, uuid.UUID) error {
	if f.updateAgentRunStarted != nil {
		return f.updateAgentRunStarted()
	}
	if f.agentRun != nil {
		if f.agentRun.Status != domain.DevMachineAgentRunStatusWaitingInput {
			f.agentRun.Status = domain.DevMachineAgentRunStatusRunning
		}
		if f.agentRun.StartedAt == nil {
			now := time.Now()
			f.agentRun.StartedAt = &now
		}
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
func (f *managerStoreFake) CreateAgentOutput(_ context.Context, chunk *domain.DevMachineLogChunk, events []domain.DevMachineEvent) error {
	if f.outputErr != nil {
		return f.outputErr
	}
	f.outputChunks = append(f.outputChunks, *chunk)
	f.events = append(f.events, events...)
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
	return f.runtimeMachines, nil
}

func (f *managerStoreFake) SetDesiredAndEnqueue(_ context.Context, _ uuid.UUID, _ uuid.UUID, desired domain.DevMachineStatus, operation *domain.DevMachineOperation) error {
	f.desired = append(f.desired, desired)
	f.actions = append(f.actions, operation.Action)
	f.operations = append(f.operations, operation)
	return nil
}

func (f *managerStoreFake) ReclaimIdleMachine(_ context.Context, waiting uuid.UUID) (*domain.DevMachine, error) {
	f.reclaimWaitingIDs = append(f.reclaimWaitingIDs, waiting)
	return f.reclaimable, nil
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
	f.githubAppConfigCalls++
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

func (f *githubAPIFake) FindOpenPullRequest(string, string, string, string, string) (*githubclient.PullRequest, error) {
	return nil, nil
}

func (f *githubAPIFake) CreatePullRequest(string, string, string, string, string, string, string) (*githubclient.PullRequest, error) {
	return &githubclient.PullRequest{HTMLURL: "https://github.com/kuayle/api/pull/1", Number: 1}, nil
}

func (f *runtimeFake) CollectValidationArtifacts(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineCheckout) ([]domain.ValidationArtifact, error) {
	return f.artifacts, nil
}

func (f *runtimeFake) RemoveCheckServices(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun) error {
	f.checkCleanups++
	return nil
}

func (f *runtimeFake) HostResources(context.Context) (HostResources, error) {
	return f.host, nil
}

type runtimeFake struct {
	Runtime
	beforeAgentStarted func()
	host               HostResources
	artifacts          []domain.ValidationArtifact
	checkCleanups      int

	agentCancellations                          int
	onAgentOutput                               func(AgentOutput) error
	spawned, started, paused, stopped, tornDown int
	snapshots                                   int
	agentRuns                                   int
	terminatedTerminals                         int
	terminatedSession                           *domain.DevMachineTerminalSession
	terminateTerminalErr                        error
	deletedEnvironmentIDs                       []uuid.UUID
	deleteEnvironmentImageErr                   error
	environmentDeleteAttempts                   int
	onSpawn                                     func()
	agentExecution                              *AgentExecution
	stats                                       map[uuid.UUID]ResourceUsage
	hostDiskUsage                               HostDiskUsage
	hostDiskUsageErr                            error
	hostDiskUsageCalls                          int
	spawnSecrets                                map[string]map[string]string
	agentSecrets                                map[string]string
	terminalSignIn                              string
	returnedSignIn                              string
	startErr                                    error
}

func (f *runtimeFake) ReadSignIn(context.Context, []domain.DevMachineService, string) (string, error) {
	return f.terminalSignIn, nil
}
func (f *runtimeFake) TakeSignInReturn(context.Context, []domain.DevMachineService, uuid.UUID) (string, error) {
	returned := f.returnedSignIn
	f.returnedSignIn = ""
	return returned, nil
}

func (f *runtimeFake) Spawn(_ context.Context, _ *domain.DevMachine, services []domain.DevMachineService, secrets map[string]map[string]string) (string, string, map[string]string, error) {
	f.spawned++
	f.spawnSecrets = secrets
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
	f.started++
	return f.startErr
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
func (f *runtimeFake) RunAgent(_ context.Context, _ *domain.DevMachine, _ *domain.DevMachineAgentRun, _ *domain.DevMachineAgentProvider, _ *domain.DevMachineCheckout, secrets map[string]string, observer *AgentObserver) (*AgentExecution, error) {
	f.agentRuns++
	if f.beforeAgentStarted != nil {
		f.beforeAgentStarted()
	}
	f.agentSecrets = secrets
	var output AgentOutput
	if observer != nil {
		if observer.Authorize != nil {
			if err := observer.Authorize(); err != nil {
				f.agentCancellations++
				return f.agentExecution, err
			}
		}
		if observer.Started != nil {
			containerID := "native"
			if f.agentExecution != nil {
				containerID = f.agentExecution.ContainerID
			}
			if err := observer.Started(containerID); err != nil {
				f.agentCancellations++
				return f.agentExecution, err
			}
		}
		output = observer.Output
	}
	if f.onAgentOutput != nil {
		if err := f.onAgentOutput(output); err != nil {
			return f.agentExecution, err
		}
	} else if output != nil && f.agentExecution != nil {
		if err := output("stdout", []byte(timestampFixtureLog(f.agentExecution.Stdout))); err != nil {
			return f.agentExecution, err
		}
		if err := output("stderr", []byte(timestampFixtureLog(f.agentExecution.Stderr))); err != nil {
			return f.agentExecution, err
		}
	}
	return f.agentExecution, nil
}
func (f *runtimeFake) CancelAgent(context.Context, *domain.DevMachineAgentRun) error {
	f.agentCancellations++
	return nil
}
func (f *runtimeFake) TerminateTerminal(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, session *domain.DevMachineTerminalSession) error {
	f.terminatedTerminals++
	f.terminatedSession = session
	return f.terminateTerminalErr
}
func (f *runtimeFake) Inspect(context.Context, *domain.DevMachine, []domain.DevMachineService) (RuntimeInspection, error) {
	return RuntimeInspection{NetworkExists: true, VolumeExists: true, GatewayAttached: true}, nil
}
func (f *runtimeFake) Stats(_ context.Context, machine *domain.DevMachine, _ []domain.DevMachineService) (ResourceUsage, error) {
	if usage, ok := f.stats[machine.ID]; ok {
		return usage, nil
	}
	return ResourceUsage{}, nil
}
func (f *runtimeFake) HostDiskUsage(context.Context) (HostDiskUsage, error) {
	f.hostDiskUsageCalls++
	return f.hostDiskUsage, f.hostDiskUsageErr
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
	store.markAgentRunStarting = func() error {
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
	require.Equal(t, 1, store.envVarListCalls)
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

func TestReconcileEmitsDiskQuotaWarningOncePerGenerationAndStillStopsAtLimit(t *testing.T) {
	machine := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 2, DiskGB: 10,
	}
	store := &managerStoreFake{machine: &machine, runtimeMachines: []domain.DevMachine{machine}}
	runtime := &runtimeFake{stats: map[uuid.UUID]ResourceUsage{}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")
	const gib = int64(1024 * 1024 * 1024)

	runtime.stats[machine.ID] = ResourceUsage{DiskBytes: 8 * gib}
	require.NoError(t, manager.reconcile(context.Background()))
	require.Empty(t, store.events)
	require.Empty(t, store.actions)

	runtime.stats[machine.ID] = ResourceUsage{DiskBytes: 9 * gib}
	require.NoError(t, manager.reconcile(context.Background()))
	require.NoError(t, manager.reconcile(context.Background()))
	require.Len(t, store.events, 1)
	require.Equal(t, "resource.disk_quota_warning", store.events[0].EventType)
	require.JSONEq(t, `{"used_bytes":9663676416,"limit_bytes":10737418240,"percent":90}`, string(store.events[0].Payload))
	require.Empty(t, store.actions)

	machine.Generation = 3
	store.runtimeMachines[0].Generation = 3
	require.NoError(t, manager.reconcile(context.Background()))
	require.Len(t, store.events, 2)
	require.Equal(t, "resource.disk_quota_warning", store.events[1].EventType)

	runtime.stats[machine.ID] = ResourceUsage{DiskBytes: 10 * gib}
	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, []domain.DevMachineStatus{domain.DevMachineStatusStopped}, store.desired)
	require.Equal(t, []domain.DevMachineOperationAction{domain.DevMachineOpStop}, store.actions)
	require.Equal(t, "resource.disk_quota_exceeded", store.events[2].EventType)
}

func TestReconcileStopsRunningMachinesOnHostDiskFloorBreach(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	machines := []domain.DevMachine{
		{
			ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
			DesiredStatus: domain.DevMachineStatusRunning, Generation: 4, DiskGB: 10,
		},
		{
			ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
			DesiredStatus: domain.DevMachineStatusRunning, Generation: 7, DiskGB: 20,
		},
	}
	tests := []struct {
		name      string
		usage     HostDiskUsage
		minFreeGB int
		minFree   int
	}{
		{name: "minimum free GB", usage: HostDiskUsage{TotalBytes: 100 * gib, FreeBytes: 20 * gib}, minFreeGB: 25, minFree: 10},
		{name: "minimum free percent", usage: HostDiskUsage{TotalBytes: 100 * gib, FreeBytes: 10 * gib}, minFreeGB: 1, minFree: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &managerStoreFake{runtimeMachines: machines}
			runtime := &runtimeFake{hostDiskUsage: test.usage}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test", DiskQuotaConfig{
				Mode: "monitored", HostMinFreePercent: test.minFree, HostMinFreeGB: test.minFreeGB,
			})

			require.NoError(t, manager.reconcile(context.Background()))
			require.Equal(t, 1, runtime.hostDiskUsageCalls)
			require.Equal(t, []domain.DevMachineStatus{
				domain.DevMachineStatusStopped,
				domain.DevMachineStatusStopped,
			}, store.desired)
			require.Equal(t, []domain.DevMachineOperationAction{
				domain.DevMachineOpStop,
				domain.DevMachineOpStop,
			}, store.actions)
			require.Len(t, store.operations, 2)
			require.Equal(t, fmt.Sprintf("host-disk-floor:%d", machines[0].Generation), store.operations[0].IdempotencyKey)
			require.Equal(t, fmt.Sprintf("host-disk-floor:%d", machines[1].Generation), store.operations[1].IdempotencyKey)

			var floorEvents []domain.DevMachineEvent
			for _, event := range store.events {
				if event.EventType == "resource.host_disk_floor" {
					floorEvents = append(floorEvents, event)
				}
			}
			require.Len(t, floorEvents, 2)
			require.JSONEq(t, fmt.Sprintf(
				`{"free_bytes":%d,"total_bytes":%d,"min_free_bytes":%d,"min_free_percent":%d}`,
				test.usage.FreeBytes, test.usage.TotalBytes, int64(test.minFreeGB)*gib, test.minFree,
			), string(floorEvents[0].Payload))
			metrics := manager.Metrics()
			require.Equal(t, uint64(test.usage.FreeBytes), metrics["host_disk_free_bytes"])
			require.Equal(t, uint64(test.usage.TotalBytes), metrics["host_disk_total_bytes"])
			require.Equal(t, uint64(1), metrics["host_disk_floor_breached"])
		})
	}
}

func TestReconcileHostDiskFloorOnlyStopsMachinesDesiredToRun(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	stopping := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusStopped, Generation: 4, DiskGB: 10,
	}
	running := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 7, DiskGB: 20,
	}
	store := &managerStoreFake{machine: &stopping, runtimeMachines: []domain.DevMachine{stopping, running}}
	runtime := &runtimeFake{hostDiskUsage: HostDiskUsage{TotalBytes: 100 * gib, FreeBytes: 20 * gib}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test", DiskQuotaConfig{
		Mode: "monitored", HostMinFreePercent: 10, HostMinFreeGB: 25,
	})

	require.NoError(t, manager.reconcile(context.Background()))

	require.Equal(t, []domain.DevMachineStatus{domain.DevMachineStatusStopped}, store.desired)
	require.Equal(t, []domain.DevMachineOperationAction{domain.DevMachineOpStop}, store.actions)
	require.Len(t, store.operations, 1)
	require.Equal(t, running.ID, store.operations[0].MachineID)

	var floorEvents []domain.DevMachineEvent
	for _, event := range store.events {
		if event.EventType == "resource.host_disk_floor" {
			floorEvents = append(floorEvents, event)
		}
	}
	require.Len(t, floorEvents, 1)
	require.Equal(t, running.ID, floorEvents[0].MachineID)
}

func TestReconcileHostDiskFloorNoBreachAndUnknownDoNotStopMachines(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	machine := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 4, DiskGB: 10,
	}
	tests := []struct {
		name  string
		usage HostDiskUsage
		err   error
	}{
		{name: "no breach", usage: HostDiskUsage{TotalBytes: 100 * gib, FreeBytes: 50 * gib}},
		{name: "unknown", err: ErrHostDiskUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &managerStoreFake{runtimeMachines: []domain.DevMachine{machine}}
			runtime := &runtimeFake{hostDiskUsage: test.usage, hostDiskUsageErr: test.err}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test", DiskQuotaConfig{
				Mode: "monitored", HostMinFreePercent: 15, HostMinFreeGB: 25,
			})

			require.NoError(t, manager.reconcile(context.Background()))
			require.Equal(t, 1, runtime.hostDiskUsageCalls)
			require.Empty(t, store.actions)
			for _, event := range store.events {
				require.NotEqual(t, "resource.host_disk_floor", event.EventType)
			}
			require.Equal(t, uint64(0), manager.Metrics()["host_disk_floor_breached"])
		})
	}
}

func TestReconcileHostDiskFloorRetainsBreachWhenMeasurementFails(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	breached := HostDiskUsage{TotalBytes: 100 * gib, FreeBytes: 20 * gib}
	healthy := HostDiskUsage{TotalBytes: 100 * gib, FreeBytes: 50 * gib}
	tests := []struct {
		name      string
		status    domain.DevMachineStatus
		action    domain.DevMachineOperationAction
		wantSpawn int
		wantStart int
	}{
		{name: "spawn", status: domain.DevMachineStatusQueued, action: domain.DevMachineOpSpawn, wantSpawn: 1},
		{name: "start", status: domain.DevMachineStatusPaused, action: domain.DevMachineOpStart, wantStart: 1},
	}
	measurementErrors := []struct {
		name string
		err  error
	}{
		{name: "unknown", err: ErrHostDiskUnknown},
		{name: "error", err: errors.New("statfs failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machineID, workspaceID := uuid.New(), uuid.New()
			store := &managerStoreFake{machine: &domain.DevMachine{
				ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
				Status: test.status, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3,
			}}
			runtime := &runtimeFake{hostDiskUsage: breached}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test", DiskQuotaConfig{
				Mode: "monitored", HostMinFreePercent: 15, HostMinFreeGB: 25,
			})

			require.NoError(t, manager.reconcile(context.Background()))
			require.Equal(t, uint64(1), manager.Metrics()["host_disk_floor_breached"])

			for _, measurementError := range measurementErrors {
				runtime.hostDiskUsageErr = measurementError.err
				require.NoError(t, manager.reconcile(context.Background()))
				metrics := manager.Metrics()
				require.Equal(t, uint64(1), metrics["host_disk_floor_breached"], measurementError.name)
				require.Equal(t, uint64(breached.FreeBytes), metrics["host_disk_free_bytes"], measurementError.name)
				require.Equal(t, uint64(breached.TotalBytes), metrics["host_disk_total_bytes"], measurementError.name)

				err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
					MachineID: machineID, WorkspaceID: workspaceID, Action: test.action, Generation: 3,
				})
				var terminalErr *terminalOperationError
				require.ErrorAs(t, err, &terminalErr)
				require.Equal(t, "host_disk_low", terminalErr.code)
				require.Zero(t, runtime.spawned)
				require.Zero(t, runtime.started)
			}

			runtime.hostDiskUsage = healthy
			runtime.hostDiskUsageErr = nil
			require.NoError(t, manager.reconcile(context.Background()))
			require.Equal(t, uint64(0), manager.Metrics()["host_disk_floor_breached"])

			err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
				MachineID: machineID, WorkspaceID: workspaceID, Action: test.action, Generation: 3,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSpawn, runtime.spawned)
			require.Equal(t, test.wantStart, runtime.started)
		})
	}
}

func TestReconcileHardModeNeverReadsHostDiskUsage(t *testing.T) {
	machine := domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 4, DiskGB: 10,
	}
	store := &managerStoreFake{runtimeMachines: []domain.DevMachine{machine}}
	runtime := &runtimeFake{hostDiskUsage: HostDiskUsage{TotalBytes: 100, FreeBytes: 0}}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Zero(t, runtime.hostDiskUsageCalls)
	require.Empty(t, store.actions)
}

func TestManagerRejectsSpawnAndStartDuringHostDiskFloorBreach(t *testing.T) {
	tests := []struct {
		name      string
		status    domain.DevMachineStatus
		action    domain.DevMachineOperationAction
		wantSpawn int
		wantStart int
	}{
		{name: "spawn", status: domain.DevMachineStatusQueued, action: domain.DevMachineOpSpawn, wantSpawn: 0, wantStart: 0},
		{name: "start", status: domain.DevMachineStatusPaused, action: domain.DevMachineOpStart, wantSpawn: 0, wantStart: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machineID, workspaceID := uuid.New(), uuid.New()
			store := &managerStoreFake{machine: &domain.DevMachine{
				ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
				Status: test.status, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3,
			}}
			runtime := &runtimeFake{}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test", DiskQuotaConfig{
				Mode: "monitored", HostMinFreePercent: 15, HostMinFreeGB: 25,
			})
			manager.hostDiskBreached.Store(true)

			err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
				MachineID: machineID, WorkspaceID: workspaceID, Action: test.action, Generation: 3,
			})

			var terminalErr *terminalOperationError
			require.ErrorAs(t, err, &terminalErr)
			require.Equal(t, "host_disk_low", terminalErr.code)
			require.Contains(t, terminalErr.message, "free disk space")
			require.Equal(t, test.wantSpawn, runtime.spawned)
			require.Equal(t, test.wantStart, runtime.started)
			require.NotEqual(t, domain.DevMachineStatusSpawning, store.machine.Status)
			require.True(t, store.failedOperationRetryable == nil)
		})
	}
}

// A stopped or paused machine whose runtime was never completed (its first
// spawn failed before the workspace volume was recorded) or was removed is
// started by provisioning it again, which reuses whatever still exists.
func TestStartProvisionsAnIncompleteRuntimeAgain(t *testing.T) {
	for _, status := range []domain.DevMachineStatus{domain.DevMachineStatusStopped, domain.DevMachineStatusPaused} {
		t.Run(string(status), func(t *testing.T) {
			machineID, workspaceID := uuid.New(), uuid.New()
			store := &managerStoreFake{
				machine: &domain.DevMachine{
					ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
					Status: status, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3,
				},
				services: []domain.DevMachineService{{ID: uuid.New(), ServiceKey: "terminal", ServiceType: "terminal"}},
			}
			runtime := &runtimeFake{startErr: fmt.Errorf("inspect terminal: %w", ErrRuntimeIncomplete)}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test")

			err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
				MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpStart, Generation: 3,
			})
			require.NoError(t, err)
			require.Equal(t, 1, runtime.started)
			require.Equal(t, 1, runtime.spawned)
			require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status)
			require.NotNil(t, store.machine.WorkspaceVolumeName)
			require.Equal(t, "workspace-volume", *store.machine.WorkspaceVolumeName)

			// Other start failures are reported, not hidden by provisioning.
			store.machine.Status = status
			runtime.startErr = errors.New("container exited with code 1")
			err = manager.processOperation(context.Background(), &domain.DevMachineOperation{
				MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpStart, Generation: 3,
			})
			require.ErrorContains(t, err, "exited with code 1")
			require.Equal(t, 1, runtime.spawned)
		})
	}
}

func TestManagerAllowsSpawnAndStartWithoutHostDiskFloorBreach(t *testing.T) {
	tests := []struct {
		name      string
		status    domain.DevMachineStatus
		action    domain.DevMachineOperationAction
		wantSpawn int
		wantStart int
	}{
		{name: "spawn", status: domain.DevMachineStatusQueued, action: domain.DevMachineOpSpawn, wantSpawn: 1, wantStart: 0},
		{name: "start", status: domain.DevMachineStatusPaused, action: domain.DevMachineOpStart, wantSpawn: 0, wantStart: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machineID, workspaceID := uuid.New(), uuid.New()
			store := &managerStoreFake{machine: &domain.DevMachine{
				ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
				Status: test.status, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3,
			}}
			runtime := &runtimeFake{}
			manager := NewManager(store, runtime, agent.NewRegistry(), nil, nil, nil, "test", DiskQuotaConfig{
				Mode: "monitored", HostMinFreePercent: 15, HostMinFreeGB: 25,
			})

			err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
				MachineID: machineID, WorkspaceID: workspaceID, Action: test.action, Generation: 3,
			})

			require.NoError(t, err)
			require.Equal(t, test.wantSpawn, runtime.spawned)
			require.Equal(t, test.wantStart, runtime.started)
			require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status)
		})
	}
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

func TestManagerInjectsTheDefaultModelServerOnlyIntoDeveloperServices(t *testing.T) {
	workspaceID := uuid.New()
	key := cryptoutil.DeriveKey("deepseek-manager-test")
	encrypted, err := cryptoutil.Encrypt("sk-deepseek-test", key)
	require.NoError(t, err)
	store := &managerStoreFake{
		machine:     &domain.DevMachine{ID: uuid.New(), WorkspaceID: workspaceID},
		modelServer: deepSeekServer(workspaceID, encrypted, "deepseek-v4-pro", "high"),
	}
	services := []domain.DevMachineService{
		{ServiceKey: "ide", ServiceType: "ide"},
		{ServiceKey: "terminal", ServiceType: "terminal"},
		{ServiceKey: "collector", ServiceType: "collector"},
		{ServiceKey: "browser", ServiceType: "browser"},
		{ServiceKey: "egress", ServiceType: "egress"},
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, key, nil, "test")

	secrets, err := manager.serviceSecrets(context.Background(), store.machine, services)
	require.NoError(t, err)

	expected := map[string]string{
		"KUAYLE_MODEL_API_KEY":                     "sk-deepseek-test",
		"KUAYLE_MODEL":                             "deepseek-v4-pro",
		"KUAYLE_MODEL_THINKING":                    "high",
		"KUAYLE_MODEL_OPENAI_BASE_URL":             "https://api.deepseek.com",
		"KUAYLE_MODEL_LIST":                        "deepseek-flash,deepseek-v4-pro",
		"ANTHROPIC_BASE_URL":                       "https://api.deepseek.com/anthropic",
		"ANTHROPIC_AUTH_TOKEN":                     "sk-deepseek-test",
		"ANTHROPIC_MODEL":                          "deepseek-v4-pro",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "deepseek-v4-pro",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "deepseek-v4-pro",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "deepseek-flash",
		"CLAUDE_CODE_SUBAGENT_MODEL":               "deepseek-flash",
		"CLAUDE_CODE_EFFORT_LEVEL":                 "high",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	require.Equal(t, expected, secrets["ide"])
	require.Equal(t, expected, secrets["terminal"])
	for _, service := range []string{"collector", "browser", "egress"} {
		for name := range expected {
			require.NotContains(t, secrets[service], name, service)
		}
	}
}

func TestManagerOmitsClaudeEffortWhenThinkingIsOff(t *testing.T) {
	workspaceID := uuid.New()
	key := cryptoutil.DeriveKey("deepseek-manager-off-test")
	encrypted, err := cryptoutil.Encrypt("sk-deepseek-test", key)
	require.NoError(t, err)
	store := &managerStoreFake{
		machine:     &domain.DevMachine{ID: uuid.New(), WorkspaceID: workspaceID},
		modelServer: deepSeekServer(workspaceID, encrypted, "deepseek-flash", "off"),
	}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, key, nil, "test")

	secrets, err := manager.serviceSecrets(context.Background(), store.machine, []domain.DevMachineService{{ServiceKey: "ide", ServiceType: "ide"}})
	require.NoError(t, err)
	require.NotContains(t, secrets["ide"], "CLAUDE_CODE_EFFORT_LEVEL")
	require.Equal(t, "off", secrets["ide"]["KUAYLE_MODEL_THINKING"])
}

func TestManagerAddsNoModelSecretsWithoutAModelServer(t *testing.T) {
	workspaceID := uuid.New()
	store := &managerStoreFake{machine: &domain.DevMachine{ID: uuid.New(), WorkspaceID: workspaceID}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, cryptoutil.DeriveKey("deepseek-manager-empty-test"), nil, "test")

	secrets, err := manager.serviceSecrets(context.Background(), store.machine, []domain.DevMachineService{{ServiceKey: "ide", ServiceType: "ide"}})
	require.NoError(t, err)
	for _, name := range []string{"KUAYLE_MODEL_API_KEY", "KUAYLE_MODEL", "KUAYLE_MODEL_THINKING", "ANTHROPIC_AUTH_TOKEN"} {
		require.NotContains(t, secrets["ide"], name)
	}
}

func TestManagerInjectsTheRunsModelServerIntoAgentRun(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	key := cryptoutil.DeriveKey("deepseek-agent-run-test")
	encrypted, err := cryptoutil.Encrypt("sk-deepseek-agent", key)
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID,
		ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
		AllowedSecrets: json.RawMessage(`[]`), CommandArgv: json.RawMessage(`["codex"]`), MaxRuntimeSeconds: 30,
		ModelServerID: serverRef(),
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
			DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume"),
		},
		agentRun:    run,
		provider:    &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "codex", ImageRef: "kuayle/codex:test", Enabled: true},
		modelServer: deepSeekServer(workspaceID, encrypted, "deepseek-v4-pro", "max"),
	}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: 0}}
	runtime.onAgentOutput = func(output AgentOutput) error {
		require.NotNil(t, output)
		require.NoError(t, output("stdout", []byte(`2026-10-26T20:00:00.000000001Z {"type":"tool.started","message":"sk-deep`)))
		require.Empty(t, store.outputChunks)
		require.NoError(t, output("stdout", []byte("seek-agent\"}\n")))
		require.Len(t, store.outputChunks, 1)
		require.NotContains(t, store.outputChunks[0].Content, "sk-deepseek-agent")
		require.Contains(t, store.outputChunks[0].Content, "[REDACTED]")
		require.Len(t, store.events, 1)
		require.Equal(t, "tool.started", store.events[0].EventType)
		require.Nil(t, store.completedRun, "output persisted before native execution returns")
		return nil
	}

	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("kuayle/codex:test")), nil, key, nil, "test")

	err = manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Generation: 1,
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"KUAYLE_MODEL_API_KEY":                     "sk-deepseek-agent",
		"KUAYLE_MODEL":                             "deepseek-v4-pro",
		"KUAYLE_MODEL_THINKING":                    "max",
		"KUAYLE_MODEL_OPENAI_BASE_URL":             "https://api.deepseek.com",
		"KUAYLE_MODEL_LIST":                        "deepseek-flash,deepseek-v4-pro",
		"ANTHROPIC_BASE_URL":                       "https://api.deepseek.com/anthropic",
		"ANTHROPIC_AUTH_TOKEN":                     "sk-deepseek-agent",
		"ANTHROPIC_MODEL":                          "deepseek-v4-pro",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "deepseek-v4-pro",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "deepseek-v4-pro",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "deepseek-flash",
		"CLAUDE_CODE_SUBAGENT_MODEL":               "deepseek-flash",
		"CLAUDE_CODE_EFFORT_LEVEL":                 "max",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}, runtime.agentSecrets)
}

func TestManagerPerRunThinkingOffSuppressesWorkspaceClaudeEffort(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	key := cryptoutil.DeriveKey("deepseek-agent-thinking-off-test")
	encrypted, err := cryptoutil.Encrypt("sk-deepseek-agent", key)
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID,
		ProviderID: "claude-code", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
		AllowedSecrets: json.RawMessage(`[]`), CommandArgv: json.RawMessage(`["claude"]`),
		CommandEnvironment: json.RawMessage(`{}`), Thinking: "off", MaxRuntimeSeconds: 30, ModelServerID: serverRef(),
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
			DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume"),
		},
		agentRun:    run,
		provider:    &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "claude-code", ImageRef: "kuayle/claude:test", Enabled: true},
		modelServer: deepSeekServer(workspaceID, encrypted, "deepseek-v4-pro", "high"),
	}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: 0}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewClaudeCodeProvider("kuayle/claude:test")), nil, key, nil, "test")

	err = manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Generation: 1,
	})
	require.NoError(t, err)
	require.NotContains(t, runtime.agentSecrets, "CLAUDE_CODE_EFFORT_LEVEL")
	require.Equal(t, "high", runtime.agentSecrets["KUAYLE_MODEL_THINKING"])
}

func signInRunFixture(t *testing.T) (*managerStoreFake, *runtimeFake, *Manager, *domain.DevMachineAgentRun, []byte) {
	t.Helper()
	machineID, workspaceID, runID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID, RequestedByUserID: &userID,
		ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
		AllowedSecrets: json.RawMessage(`[]`), CommandArgv: json.RawMessage(`["codex"]`),
		CommandEnvironment: json.RawMessage(`{}`), MaxRuntimeSeconds: 30,
	}
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
			DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume"),
		},
		agentRun: run,
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "codex", ImageRef: "kuayle/codex:test", Enabled: true},
		// A default server exists, but a run uses only the server recorded on it.
		modelServer: deepSeekServer(workspaceID, "sealed", "deepseek-flash", "high"),
	}
	key := cryptoutil.DeriveKey("sign-in-test")
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: 0}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("kuayle/codex:test")), nil, key, nil, "test")
	return store, runtime, manager, run, key
}

func TestManagerStopsAgentRunsThatNeedTheUsersSignIn(t *testing.T) {
	store, runtime, manager, run, _ := signInRunFixture(t)
	err := manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: run.MachineID, WorkspaceID: run.WorkspaceID, AgentRunID: &run.ID, Generation: 1,
	})
	var terminal *terminalOperationError
	require.ErrorAs(t, err, &terminal, "the operation fails without retrying")
	require.Equal(t, "sign_in_required", terminal.code)
	require.Zero(t, runtime.agentRuns, "no native process starts without credentials")
}

func TestManagerInjectsTheUsersSignInAndKeepsItsRefresh(t *testing.T) {
	store, runtime, manager, run, key := signInRunFixture(t)
	original := `{"tokens":{"access_token":"fixture-access-token-0001","refresh_token":"fixture-refresh-token-0001","account_id":"acct"}}`
	sealed, err := cryptoutil.Encrypt(original, key)
	require.NoError(t, err)
	require.NoError(t, store.SaveAgentSignIn(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "codex", sealed))
	refreshed := `{"tokens":{"access_token":"fixture-access-token-0002","refresh_token":"fixture-refresh-token-0002","account_id":"acct"}}`
	runtime.returnedSignIn = refreshed

	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: run.MachineID, WorkspaceID: run.WorkspaceID, AgentRunID: &run.ID, Generation: 1,
	}))
	require.Equal(t, original, runtime.agentSecrets["KUAYLE_CODEX_AUTH"])
	require.NotContains(t, runtime.agentSecrets, "KUAYLE_MODEL_API_KEY", "no model server was recorded on the run")
	tokens := []string{}
	for name, value := range runtime.agentSecrets {
		if strings.HasPrefix(name, "KUAYLE_SIGN_IN_SECRET_") {
			tokens = append(tokens, value)
		}
	}
	require.ElementsMatch(t, []string{"fixture-access-token-0001", "fixture-refresh-token-0001"}, tokens, "tokens are redacted from output")
	stored, err := cryptoutil.Decrypt(store.signIns[run.RequestedByUserID.String()+"/codex"], key)
	require.NoError(t, err)
	require.Equal(t, refreshed, stored, "a rotated refresh token is kept for the next run")
}

func TestManagerRunsAgentsWithAnAPIKeyWithoutSignIn(t *testing.T) {
	store, runtime, manager, run, key := signInRunFixture(t)
	sealed, err := cryptoutil.Encrypt("fixture-openai-key", key)
	require.NoError(t, err)
	run.AllowedSecrets = json.RawMessage(`["OPENAI_API_KEY"]`)
	store.envVars = []domain.DevMachineEnvVar{{Name: "OPENAI_API_KEY", EncryptedValue: sealed}}
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: run.MachineID, WorkspaceID: run.WorkspaceID, AgentRunID: &run.ID, Generation: 1,
	}))
	require.Equal(t, 1, runtime.agentRuns)
	require.NotContains(t, runtime.agentSecrets, "KUAYLE_CODEX_AUTH")
}

func TestManagerCapturesTheSignInFromTheTerminal(t *testing.T) {
	store, runtime, manager, run, key := signInRunFixture(t)
	op := &domain.DevMachineOperation{MachineID: run.MachineID, WorkspaceID: run.WorkspaceID, AgentRunID: &run.ID, RequestedByUserID: run.RequestedByUserID, Action: domain.DevMachineOpCaptureSignIn}
	var terminal *terminalOperationError
	require.ErrorAs(t, manager.captureSignIn(context.Background(), store.machine, op), &terminal)
	require.Equal(t, "sign_in_not_found", terminal.code)
	runtime.terminalSignIn = "not json"
	require.ErrorAs(t, manager.captureSignIn(context.Background(), store.machine, op), &terminal)

	runtime.terminalSignIn = `{"claudeAiOauth":{"accessToken":"fixture-access-token"}}` + "\n"
	require.NoError(t, manager.captureSignIn(context.Background(), store.machine, op))
	stored, err := cryptoutil.Decrypt(store.signIns[run.RequestedByUserID.String()+"/codex"], key)
	require.NoError(t, err)
	require.Equal(t, `{"claudeAiOauth":{"accessToken":"fixture-access-token"}}`, stored)
	require.NotContains(t, store.signIns[run.RequestedByUserID.String()+"/codex"], "fixture-access-token", "stored encrypted")
}

func ptr[T any](value T) *T {
	return &value
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

func TestRepositoryTokenWithUnsetGlobalClientUsesWorkspaceConfig(t *testing.T) {
	store := &managerStoreFake{githubInstallationID: 42}
	var globalClient *githubclient.Client
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), globalClient, nil, cryptoutil.DeriveKey("workspace-key"), "test")
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	client, token, err := manager.repositoryToken(context.Background(), machine, "kuayle/api")
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.Equal(t, 1, store.githubAppConfigCalls)
	require.Nil(t, client)
	require.Empty(t, token)
}

func TestManagerRechecksTaskAuthorityImmediatelyBeforeNativeDispatch(t *testing.T) {
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
	checks := 0
	store.onAuthorizeAgentRun = func() error {
		checks++
		if checks > 1 {
			return repository.ErrAgentTaskUnauthorized
		}
		return nil
	}
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), nil, "test")

	err := manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID,
		Action: domain.DevMachineOpRunAgent, Generation: 1,
	})

	require.NoError(t, err)
	require.NotNil(t, store.completedRun)
	require.Equal(t, domain.DevMachineAgentRunStatusCancelled, store.completedRun.Status)
	require.Equal(t, 1, store.envVarListCalls)
	require.Zero(t, store.runtimeServicesCreated)
	require.Zero(t, runtime.agentRuns)
}

func TestManagerRecoveryUsesOriginalSecretsAfterCredentialRotation(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	key := cryptoutil.DeriveKey("snapshot-recovery-test")
	original := "fixture-original-provider-key"
	current, _ := cryptoutil.Encrypt("fixture-rotated-provider-key", key)
	snapshot, _ := cryptoutil.Encrypt(`{"KUAYLE_MODEL_API_KEY":"`+original+`"}`, key)
	run := &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusRunning, EncryptedOutputSecrets: &snapshot, MaxRuntimeSeconds: 30}
	store := &managerStoreFake{machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}, agentRun: run, provider: &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"}, modelServer: &domain.AgentModelServer{EncryptedAPIKey: current, DefaultModel: "deepseek-flash"}}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "existing", Stdout: "work " + original + "\n", ExitCode: 0}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{AgentRunID: &runID, Generation: 1}))
	require.Equal(t, original, runtime.agentSecrets["KUAYLE_MODEL_API_KEY"])
	require.Zero(t, store.envVarListCalls, "recovery does not replace a running container's credentials")
	require.Len(t, store.outputChunks, 1)
	require.Equal(t, "work [REDACTED]\n", store.outputChunks[0].Content)
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), snapshot)
	require.NotContains(t, string(encoded), original)
}

func TestManagerStopsRecoveryWithoutUsableOriginalCredentials(t *testing.T) {
	for _, scenario := range []string{"missing", "corrupt", "invalid_json", "unknown_version", "missing_redactions", "mismatched_redactions"} {
		t.Run(scenario, func(t *testing.T) {
			machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
			key := cryptoutil.DeriveKey("recovery-test")
			run := &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusRunning, MaxRuntimeSeconds: 30}
			if scenario == "corrupt" {
				run.EncryptedOutputSecrets = ptr("invalid-ciphertext")
			}
			if scenario == "invalid_json" {
				encoded, err := cryptoutil.Encrypt(`{"unexpected":3}`, key)
				require.NoError(t, err)
				run.EncryptedOutputSecrets = &encoded
			}
			malformed := map[string]string{
				"unknown_version":       `{"version":2,"environment":{},"redactions":{}}`,
				"missing_redactions":    `{"version":1,"environment":{"TOKEN":"fixture"}}`,
				"mismatched_redactions": `{"version":1,"environment":{"TOKEN":"fixture"},"redactions":{"TOKEN":"different"}}`,
			}
			if payload, ok := malformed[scenario]; ok {
				encoded, err := cryptoutil.Encrypt(payload, key)
				require.NoError(t, err)
				run.EncryptedOutputSecrets = &encoded
			}
			store := &managerStoreFake{machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}, agentRun: run, provider: &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"}}
			runtime := &runtimeFake{}
			manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")
			err := manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{AgentRunID: &runID, Generation: 1})
			require.ErrorContains(t, err, "original credentials")
			require.Equal(t, 1, runtime.agentCancellations)
			require.Zero(t, runtime.agentRuns)
			require.Zero(t, store.envVarListCalls)
			require.Empty(t, store.outputChunks)
		})
	}
}

func timestampFixtureLog(value string) string {
	if value == "" {
		return ""
	}
	lines := strings.SplitAfter(value, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "2026-10-26T20:00:00.000000001Z " + line
		}
	}
	return strings.Join(lines, "")
}

func TestManagerWaitsForNativeStartBeforePublishingRunning(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30, ModelServerID: serverRef()}
	store := &managerStoreFake{machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}, agentRun: run, provider: &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"}}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "native", ExitCode: 0}}
	runtime.beforeAgentStarted = func() {
		require.Equal(t, domain.DevMachineAgentRunStatusStarting, store.agentRun.Status, "image preparation is not native execution")
		require.Nil(t, store.agentRun.StartedAt)
	}
	runtime.onAgentOutput = func(output AgentOutput) error {
		require.Equal(t, domain.DevMachineAgentRunStatusRunning, store.agentRun.Status)
		require.NotNil(t, store.agentRun.StartedAt)
		return nil
	}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, make([]byte, 32), nil, "test")
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{AgentRunID: &runID, Generation: 1}))
}

func TestManagerRedactsCredentialsWithoutCorruptingModelConfigurationOrNumbers(t *testing.T) {
	key := cryptoutil.DeriveKey("public-model-output-test")
	encrypted, err := cryptoutil.Encrypt("fixture-model-key", key)
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New(), ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30, ModelServerID: serverRef()}
	store := &managerStoreFake{machine: &domain.DevMachine{ID: run.MachineID, WorkspaceID: run.WorkspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}, agentRun: run, provider: &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"}, modelServer: &domain.AgentModelServer{EncryptedAPIKey: encrypted, DefaultModel: "deepseek-flash", Thinking: "high"}}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "native", Stdout: `{"type":"tool.started","count":1,"message":"deepseek-flash high fixture-model-key"}` + "\n"}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			run.Status = domain.DevMachineAgentRunStatusRunning
			store.modelServer = nil
			store.outputChunks = nil
			store.events = nil
		}
		require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{AgentRunID: &run.ID, Generation: 1}))
		require.Len(t, store.outputChunks, 1)
		require.JSONEq(t, `{"type":"tool.started","count":1,"message":"deepseek-flash high [REDACTED]"}`, store.outputChunks[0].Content)
		require.Len(t, store.events, 2)
		require.Equal(t, "tool.started", store.events[0].EventType)
		require.Equal(t, "fixture-model-key", runtime.agentSecrets["KUAYLE_MODEL_API_KEY"])
	}
}
