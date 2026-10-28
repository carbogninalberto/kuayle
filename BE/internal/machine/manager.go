package machine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

type ManagerStore interface {
	GetValidation(context.Context, domain.DevMachineOperation) (*domain.DevMachineAgentRunStep, error)
	StartValidation(context.Context, domain.DevMachineOperation, string) error
	CompleteValidation(context.Context, domain.DevMachineOperation, int) error
	CreateValidationOutput(context.Context, domain.DevMachineOperation, *domain.DevMachineLogChunk) error
	ListResourceAllocations(context.Context) ([]domain.DevMachineResourceAllocation, error)
	RecordNativeSession(context.Context, uuid.UUID, string) error
	SaveValidationArtifacts(context.Context, domain.DevMachineOperation, []domain.ValidationArtifact) error
	MachinePorts(context.Context, uuid.UUID) ([]domain.DevMachinePort, error)
	CreatePort(context.Context, *domain.DevMachinePort) error
	DeleteAgentPort(context.Context, uuid.UUID, int) (bool, error)
	SetListeningPorts(context.Context, uuid.UUID, []int, []int) error
	ListPortScanMachines(context.Context, int) ([]domain.DevMachine, error)
	TaskForWorktree(context.Context, uuid.UUID, string) (*uuid.UUID, error)
	PreviewTarget(context.Context, uuid.UUID) (*repository.TaskPreviewTarget, error)
	SetTaskPreview(context.Context, uuid.UUID, string, *string, *domain.DevMachinePort) error
	StopTaskPreview(context.Context, uuid.UUID) error
	StopMachinePreviews(context.Context, uuid.UUID) error
	DeferOperation(context.Context, uuid.UUID, string, string, string, time.Duration) error
	GetPublication(context.Context, domain.DevMachineOperation) (*domain.AgentPublication, error)
	StartPublication(context.Context, domain.DevMachineOperation) error
	CompletePublication(context.Context, domain.DevMachineOperation, domain.AgentPublicationResult) error
	LeaseOperations(context.Context, string, int, time.Duration) ([]domain.DevMachineOperation, error)
	RenewOperationLease(context.Context, uuid.UUID, string, time.Duration) error
	CompleteOperation(context.Context, uuid.UUID, string) error
	FailOperation(context.Context, uuid.UUID, string, string, string, bool) (bool, error)
	GetMachineInternal(context.Context, uuid.UUID) (*domain.DevMachine, error)
	GetProvider(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineAgentProvider, error)
	GetAgentRunInternal(context.Context, uuid.UUID) (*domain.DevMachineAgentRun, error)
	ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error)
	// DockerAllowed reports the workspace policy's Docker daemon switch.
	DockerAllowed(context.Context, uuid.UUID) (bool, error)
	ListEnvVarsInternal(context.Context, uuid.UUID, *string, string) ([]domain.DevMachineEnvVar, error)
	GetModelServer(context.Context, uuid.UUID, *uuid.UUID) (*domain.AgentModelServer, error)
	GetAgentSignIn(context.Context, uuid.UUID, uuid.UUID, string) (string, error)
	GetAgentSignInToken(context.Context, uuid.UUID, uuid.UUID, string) (string, error)
	SaveAgentSignIn(context.Context, uuid.UUID, uuid.UUID, string, string) error
	SetMachineState(context.Context, uuid.UUID, domain.DevMachineStatus, *string, *string, *string, *string) error
	SetMachineStateForOperation(context.Context, uuid.UUID, int64, bool, domain.DevMachineStatus, *string, *string, *string, *string) (bool, error)
	UpdateServiceRuntime(context.Context, uuid.UUID, string, string, string, *string) error
	CreateRuntimeService(context.Context, *domain.DevMachineService) error
	MarkAgentRunStarting(context.Context, uuid.UUID) error
	UpdateAgentRunStarted(context.Context, uuid.UUID) error
	SealAgentRunSecrets(context.Context, uuid.UUID, string) (string, error)
	AuthorizeAgentRun(context.Context, uuid.UUID) error
	CompleteAgentRun(context.Context, *domain.DevMachineAgentRun) error
	CreateEvent(context.Context, *domain.DevMachineEvent) error
	CreateLogChunk(context.Context, *domain.DevMachineLogChunk) error
	CreateAgentOutput(context.Context, *domain.DevMachineLogChunk, []domain.DevMachineEvent) error
	RevokeMachineAccess(context.Context, uuid.UUID) error
	ListExpiredMachines(context.Context, int) ([]domain.DevMachine, error)
	ListTimedOutAgentRuns(context.Context, int) ([]domain.DevMachineAgentRun, error)
	ListRuntimeMachines(context.Context, int, int) ([]domain.DevMachine, error)
	SetDesiredAndEnqueue(context.Context, uuid.UUID, uuid.UUID, domain.DevMachineStatus, *domain.DevMachineOperation) error
	RequestPermanentDelete(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) (*domain.DevMachineOperation, error)
	ListPermanentDeleteRequests(context.Context, int) ([]domain.DevMachine, error)
	PurgePermanentDeleteRequest(context.Context, uuid.UUID, uuid.UUID) error
	CreateResourceSample(context.Context, *domain.DevMachineResourceSample) error
	UpdateVolumeUsage(context.Context, uuid.UUID, int64) error
	CreateGitRef(context.Context, *domain.DevMachineGitRef) error
	GetGitHubInstallationID(context.Context, uuid.UUID, string) (int64, error)
	GetGitHubAppConfig(context.Context, uuid.UUID) (*domain.GitHubAppConfig, error)
	GetCheckoutInternal(context.Context, uuid.UUID) (*domain.DevMachineCheckout, error)
	UpdateCheckoutState(context.Context, uuid.UUID, string, *string) error
	GetTerminalSessionInternal(context.Context, uuid.UUID) (*domain.DevMachineTerminalSession, error)
	CompleteTerminalSessionClose(context.Context, uuid.UUID) error
	FailTerminalSessionClose(context.Context, uuid.UUID) error
	GetEnvironment(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineEnvironment, error)
	UpdateEnvironmentState(context.Context, uuid.UUID, string, string, *string) error
	ReconcileOrphanedEnvironments(context.Context, int) (int, error)
	ReconcileOrphanedCheckouts(context.Context, int) (int, error)
	ListDeleteRequestedEnvironments(context.Context, int) ([]domain.DevMachineEnvironment, error)
	DeleteEnvironment(context.Context, uuid.UUID, uuid.UUID) error
	ListIdleMachines(context.Context, int) ([]domain.DevMachine, error)
	ReclaimIdleMachine(context.Context, uuid.UUID) (*domain.DevMachine, error)
	UpsertRuntimeCredential(context.Context, *domain.DevMachineRuntimeCredential) error
	PurgeExpiredRuntimeCredentials(context.Context, time.Time) (int, error)
	PurgeAccessLogs(context.Context, time.Time, int) (int, error)
}

const gatewayAccessLogRetention = 90 * 24 * time.Hour

const gatewayAccessLogPurgeBatch = 1000

type githubAPI interface {
	GetRepositoryInstallationToken(int64, string) (string, time.Time, error)
	CreatePullRequest(string, string, string, string, string, string, string) (*githubclient.PullRequest, error)
	FindOpenPullRequest(string, string, string, string, string) (*githubclient.PullRequest, error)
}

type DiskQuotaConfig struct {
	Mode               string
	HostMinFreePercent int
	HostMinFreeGB      int
}

func (c DiskQuotaConfig) monitored() bool {
	return monitoredDiskQuotaMode(c.Mode)
}

type Manager struct {
	store         ManagerStore
	runtime       Runtime
	agents        *agent.Registry
	github        githubAPI
	encryptionKey []byte
	githubKey     []byte
	owner         string
	pollInterval  time.Duration
	diskQuota     DiskQuotaConfig

	agentGitIdentity GitIdentity
	// attachmentFiles reads task images from storage; nil copies none.
	attachmentFiles attachmentReader

	capacity    *CapacityConfig
	admissionMu sync.Mutex
	admitted    map[uuid.UUID]HostResources

	operationsCompleted atomic.Uint64
	operationsFailed    atomic.Uint64
	agentRunsCompleted  atomic.Uint64
	lastReconcileUnix   atomic.Int64
	hostDiskFreeBytes   atomic.Int64
	hostDiskTotalBytes  atomic.Int64
	hostDiskBreached    atomic.Bool
	operationSlots      chan struct{}
	operations          sync.WaitGroup
	activeRunsMu        sync.Mutex
	activeRuns          map[uuid.UUID]map[uuid.UUID]context.CancelFunc
	diskQuotaWarnMu     sync.Mutex
	diskQuotaWarnings   map[uuid.UUID]int64

	// machineDomain builds app port URLs for agents; empty omits them.
	machineDomain string
	portScanning  atomic.Bool
	portStateMu   sync.Mutex
	portStates    map[uuid.UUID]publishedPortState
}

type terminalOperationError struct {
	code    string
	message string
}

func (e *terminalOperationError) Error() string {
	return e.code + ": " + e.message
}

func NewManager(store ManagerStore, runtime Runtime, agents *agent.Registry, github githubAPI, encryptionKey, githubKey []byte, owner string, diskQuotas ...DiskQuotaConfig) *Manager {
	// The optional global client arrives as a typed nil when only workspace apps
	// are configured. Normalize it so repositoryToken can use its workspace fallback.
	if client, ok := github.(*githubclient.Client); ok && client == nil {
		github = nil
	}
	var diskQuota DiskQuotaConfig
	if len(diskQuotas) > 0 {
		diskQuota = diskQuotas[0]
	}
	return &Manager{
		store: store, runtime: runtime, agents: agents, github: github, encryptionKey: encryptionKey,
		githubKey: githubKey, owner: owner, pollInterval: time.Second, operationSlots: make(chan struct{}, 8),
		activeRuns:        make(map[uuid.UUID]map[uuid.UUID]context.CancelFunc),
		diskQuotaWarnings: make(map[uuid.UUID]int64),
		diskQuota:         diskQuota,
	}
}

func (m *Manager) Run(ctx context.Context) error {
	operationTicker := time.NewTicker(m.pollInterval)
	reconcileTicker := time.NewTicker(30 * time.Second)
	portTicker := time.NewTicker(portScanInterval)
	defer operationTicker.Stop()
	defer reconcileTicker.Stop()
	defer portTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.operations.Wait()
			return nil
		case <-portTicker.C:
			m.schedulePortScan(ctx)
		case <-operationTicker.C:
			if err := m.processBatch(ctx); err != nil {
				log.WithError(err).Error("dev machine operation batch failed")
			}
		case <-reconcileTicker.C:
			if err := m.reconcile(ctx); err != nil {
				log.WithError(err).Error("dev machine reconciliation failed")
			}
		}
	}
}

func (m *Manager) Ready(ctx context.Context) error {
	return m.runtime.Ping(ctx)
}

func (m *Manager) Metrics() map[string]uint64 {
	return map[string]uint64{
		"operations_completed_total": m.operationsCompleted.Load(),
		"operations_failed_total":    m.operationsFailed.Load(),
		"agent_runs_completed_total": m.agentRunsCompleted.Load(),
		"last_reconcile_unix":        uint64(max(m.lastReconcileUnix.Load(), 0)),
		"host_disk_free_bytes":       uint64(max(m.hostDiskFreeBytes.Load(), 0)),
		"host_disk_total_bytes":      uint64(max(m.hostDiskTotalBytes.Load(), 0)),
		"host_disk_floor_breached":   boolMetric(m.hostDiskBreached.Load()),
	}
}

func (m *Manager) processBatch(ctx context.Context) error {
	available := cap(m.operationSlots) - len(m.operationSlots)
	if available == 0 {
		return nil
	}
	operations, err := m.store.LeaseOperations(ctx, m.owner, available, 2*time.Minute)
	if err != nil {
		return err
	}
	for i := range operations {
		operation := operations[i]
		m.operationSlots <- struct{}{}
		m.operations.Add(1)
		go m.processLeasedOperation(ctx, operation)
	}
	return nil
}

func (m *Manager) processLeasedOperation(ctx context.Context, operation domain.DevMachineOperation) {
	defer func() {
		<-m.operationSlots
		m.operations.Done()
	}()
	started := time.Now()
	operationCtx, cancel := context.WithCancel(ctx)
	if (operation.Action == domain.DevMachineOpRunAgent || operation.Action == domain.DevMachineOpValidateAgent || operation.Action == domain.DevMachineOpPublishAgent) && operation.AgentRunID != nil {
		m.registerActiveRun(operation.MachineID, *operation.AgentRunID, cancel)
		defer m.unregisterActiveRun(operation.MachineID, *operation.AgentRunID)
	}
	leaseErrors := make(chan error, 1)
	renewalDone := make(chan struct{})
	go func() {
		defer close(renewalDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-operationCtx.Done():
				return
			case <-ticker.C:
				if err := m.store.RenewOperationLease(operationCtx, operation.ID, m.owner, 2*time.Minute); err != nil {
					leaseErrors <- err
					cancel()
					return
				}
			}
		}
	}()
	err := m.processOperation(operationCtx, &operation)
	cancel()
	<-renewalDone
	select {
	case leaseErr := <-leaseErrors:
		if err == nil {
			err = leaseErr
		}
	default:
	}
	fields := log.Fields{
		"workspace_id": operation.WorkspaceID, "machine_id": operation.MachineID,
		"agent_run_id": operation.AgentRunID, "event_type": "machine.operation",
		"operation": operation.Action, "duration_ms": time.Since(started).Milliseconds(),
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finalCancel()
	var waitErr *capacityWaitError
	if errors.As(err, &waitErr) {
		if deferErr := m.store.DeferOperation(finalCtx, operation.ID, m.owner, "host_capacity_wait", waitErr.message, capacityRetryDelay); deferErr != nil {
			log.WithFields(fields).WithError(deferErr).Error("failed to defer operation for host capacity")
		} else {
			log.WithFields(fields).Info("operation waiting for host capacity")
		}
		return
	}
	if err != nil {
		m.operationsFailed.Add(1)
		var terminalErr *terminalOperationError
		retryable := !errors.As(err, &terminalErr)
		willRetry, failErr := m.store.FailOperation(finalCtx, operation.ID, m.owner, errorCode(err), safeError(err), retryable)
		if failErr != nil {
			log.WithFields(fields).WithError(failErr).Error("failed to record operation failure")
			return
		}
		if !willRetry && operation.Action == domain.DevMachineOpRunAgent && operation.AgentRunID != nil {
			m.failAgentRun(finalCtx, *operation.AgentRunID, err)
		}
		if !willRetry && operation.Action == domain.DevMachineOpCheckoutIssue && operation.CheckoutID != nil {
			message := safeError(err)
			if checkoutErr := m.store.UpdateCheckoutState(finalCtx, *operation.CheckoutID, "failed", &message); checkoutErr != nil {
				log.WithFields(fields).WithError(checkoutErr).Error("failed to record checkout failure")
			}
		}
		if !willRetry && operation.Action == domain.DevMachineOpTerminateTerminal && operation.TerminalSessionID != nil {
			if terminalErr := m.store.FailTerminalSessionClose(finalCtx, *operation.TerminalSessionID); terminalErr != nil {
				log.WithFields(fields).WithError(terminalErr).Error("failed to record terminal close failure")
			}
		}
		if !willRetry && lifecycleFailureAffectsMachine(operation.Action) {
			code, message := errorCode(err), safeError(err)
			_, _ = m.store.SetMachineStateForOperation(finalCtx, operation.MachineID, operation.Generation, false, domain.DevMachineStatusFailed, nil, nil, &code, &message)
		}
		log.WithFields(fields).WithError(err).Warn("dev machine operation failed")
		return
	}
	if err := m.store.CompleteOperation(finalCtx, operation.ID, m.owner); err != nil {
		log.WithFields(fields).WithError(err).Error("failed to complete operation lease")
		return
	}
	if operation.Action == domain.DevMachineOpTeardown {
		machine, getErr := m.store.GetMachineInternal(finalCtx, operation.MachineID)
		if getErr == nil && machine != nil && machine.DeleteRequestedAt != nil && domain.DevMachineSafelyPurgeable(machine) {
			if purgeErr := m.store.PurgePermanentDeleteRequest(finalCtx, machine.WorkspaceID, machine.ID); purgeErr != nil && !errors.Is(purgeErr, sql.ErrNoRows) {
				log.WithFields(fields).WithError(purgeErr).Warn("permanent machine purge deferred to reconciliation")
			}
		}
	}
	m.operationsCompleted.Add(1)
	log.WithFields(fields).Info("dev machine operation completed")
}

func (m *Manager) processOperation(ctx context.Context, operation *domain.DevMachineOperation) error {
	machine, err := m.store.GetMachineInternal(ctx, operation.MachineID)
	if err != nil {
		return err
	}
	if machine == nil {
		return fmt.Errorf("machine_not_found: machine does not exist")
	}
	if operation.Action == domain.DevMachineOpSnapshotEnvironment && operation.Generation != machine.Generation {
		return m.failSnapshotOperation(ctx, operation, "environment_snapshot_stale",
			fmt.Sprintf("machine generation changed from %d to %d before the snapshot completed", operation.Generation, machine.Generation))
	}
	if operation.Generation < machine.Generation {
		switch operation.Action {
		case domain.DevMachineOpTeardown, domain.DevMachineOpCancelAgent, domain.DevMachineOpTerminateTerminal, domain.DevMachineOpPurgeTaskSession, domain.DevMachineOpStopPreview:
			// Destructive cleanup must still converge after a newer generation.
		case domain.DevMachineOpStartPreview:
			return m.stalePreview(ctx, operation)
		case domain.DevMachineOpCheckoutIssue:
			if operation.CheckoutID != nil {
				message := "machine state changed before checkout preparation; try again"
				return m.store.UpdateCheckoutState(ctx, *operation.CheckoutID, "failed", &message)
			}
			return nil
		case domain.DevMachineOpValidateAgent:
			return &terminalOperationError{code: "validation_stale", message: "Machine changed during validation"}
		case domain.DevMachineOpPublishAgent:
			return &terminalOperationError{code: "publication_stale", message: "Machine changed during publication"}
		case domain.DevMachineOpRunAgent:
			if operation.AgentRunID != nil {
				m.cancelStaleAgentRun(ctx, *operation.AgentRunID)
			}
			return nil
		default:
			return nil
		}
	}
	if operation.Action == domain.DevMachineOpSpawn || operation.Action == domain.DevMachineOpStart {
		if m.diskQuota.monitored() && m.hostDiskBreached.Load() {
			return &terminalOperationError{
				code: "host_disk_low",
				message: "host free disk space is below the configured Dev Machine floor; " +
					"wait for disk usage to fall before starting another machine",
			}
		}
	}
	if operation.Action == domain.DevMachineOpSpawn || (operation.Action == domain.DevMachineOpStart && machine.Status != domain.DevMachineStatusRunning) {
		release, err := m.admit(ctx, machine)
		if err != nil {
			var waitErr *capacityWaitError
			if errors.As(err, &waitErr) {
				m.reclaimHostCapacity(ctx, machine)
			}
			return err
		}
		defer release()
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	if (operation.Action == domain.DevMachineOpSpawn || operation.Action == domain.DevMachineOpStart) && hasDockerService(services) {
		allowed, err := m.store.DockerAllowed(ctx, machine.WorkspaceID)
		if err != nil {
			return err
		}
		if !allowed {
			return &terminalOperationError{code: "docker_not_allowed", message: "this machine has a Docker daemon, which the workspace no longer allows; delete it and create a new machine"}
		}
	}
	switch operation.Action {
	case domain.DevMachineOpSpawn:
		return m.spawn(ctx, machine, services, operation)
	case domain.DevMachineOpStart:
		if machine.Status == domain.DevMachineStatusFailed || machine.Status == domain.DevMachineStatusSpawning || machine.Status == domain.DevMachineStatusQueued {
			return m.spawn(ctx, machine, services, operation)
		}
		if machine.Status == domain.DevMachineStatusPaused || machine.Status == domain.DevMachineStatusStopped {
			if machine.Status == domain.DevMachineStatusPaused {
				if err := m.store.RevokeMachineAccess(ctx, machine.ID); err != nil {
					return err
				}
			}
			serviceSecrets, err := m.serviceSecrets(ctx, machine, services)
			if err != nil {
				return err
			}
			if err := m.runtime.Start(ctx, machine, services, serviceSecrets); err != nil {
				if errors.Is(err, ErrRuntimeIncomplete) {
					log.WithField("machine_id", machine.ID).WithError(err).Info("provisioning incomplete machine runtime on start")
					return m.spawn(ctx, machine, services, operation)
				}
				return err
			}
			for _, service := range services {
				if service.ContainerID == nil || (service.ServiceType == "agent" && (machine.Status != domain.DevMachineStatusPaused || (service.Status != "running" && service.Status != "paused"))) {
					continue
				}
				_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "running", "healthy", nil)
			}
			return m.setStateAndEventForOperation(ctx, machine, operation, domain.DevMachineStatusRunning, "machine.started", nil, false)
		}
		return nil
	case domain.DevMachineOpPause:
		if machine.Status == domain.DevMachineStatusPaused {
			return m.store.RevokeMachineAccess(ctx, machine.ID)
		}
		if err := m.store.RevokeMachineAccess(ctx, machine.ID); err != nil {
			return err
		}
		if err := m.runtime.Pause(ctx, machine, services); err != nil {
			return err
		}
		for _, service := range services {
			if service.ContainerID != nil && (service.ServiceType != "agent" || service.Status == "running") {
				_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "paused", "paused", nil)
			}
		}
		return m.setStateAndEventForOperation(ctx, machine, operation, domain.DevMachineStatusPaused, "machine.paused", nil, false)
	case domain.DevMachineOpStop:
		if machine.Status == domain.DevMachineStatusStopped {
			return nil
		}
		updated, err := m.store.SetMachineStateForOperation(ctx, machine.ID, operation.Generation, false, domain.DevMachineStatusStopping, nil, nil, nil, nil)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		if err := m.runtime.Stop(ctx, machine, services); err != nil {
			return err
		}
		for _, service := range services {
			if service.ContainerID != nil {
				_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "stopped", "stopped", nil)
			}
		}
		_ = m.store.RevokeMachineAccess(ctx, machine.ID)
		m.previewsEnded(ctx, machine)
		return m.setStateAndEventForOperation(ctx, machine, operation, domain.DevMachineStatusStopped, "machine.stopped", nil, false)
	case domain.DevMachineOpTeardown:
		if machine.Status == domain.DevMachineStatusDestroyed {
			return nil
		}
		updated, err := m.store.SetMachineStateForOperation(ctx, machine.ID, operation.Generation, true, domain.DevMachineStatusTearingDown, nil, nil, nil, nil)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		m.cancelActiveRuns(ctx, machine.ID, nil)
		_ = m.store.RevokeMachineAccess(ctx, machine.ID)
		if err := m.runtime.Teardown(ctx, machine, services); err != nil {
			return err
		}
		for _, service := range services {
			if service.ContainerID != nil {
				_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "destroyed", "stopped", nil)
			}
		}
		m.previewsEnded(ctx, machine)
		return m.setStateAndEventForOperation(ctx, machine, operation, domain.DevMachineStatusDestroyed, "machine.destroyed", nil, true)
	case domain.DevMachineOpValidateAgent:
		return m.validateAgent(ctx, machine, operation)
	case domain.DevMachineOpPublishAgent:
		return m.publishAgent(ctx, machine, operation)
	case domain.DevMachineOpPurgeTaskSession:
		return m.purgeTaskSession(ctx, machine, operation)
	case domain.DevMachineOpStartPreview:
		return m.startPreview(ctx, machine, operation)
	case domain.DevMachineOpStopPreview:
		return m.stopPreview(ctx, machine, operation)
	case domain.DevMachineOpCaptureSignIn:
		return m.captureSignIn(ctx, machine, operation)
	case domain.DevMachineOpRunAgent:
		return m.runAgent(ctx, machine, operation)
	case domain.DevMachineOpCancelAgent:
		if operation.AgentRunID == nil {
			return fmt.Errorf("invalid_operation: cancel operation has no agent run")
		}
		run, err := m.store.GetAgentRunInternal(ctx, *operation.AgentRunID)
		if err != nil || run == nil {
			return err
		}
		m.cancelActiveRuns(ctx, machine.ID, &run.ID)
		if err := m.runtime.CancelAgent(ctx, run); err != nil {
			return err
		}
		for _, service := range services {
			if service.AgentRunID != nil && *service.AgentRunID == run.ID && service.ContainerID != nil {
				_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "stopped", "stopped", nil)
			}
		}
		return nil
	case domain.DevMachineOpCheckoutIssue:
		if operation.CheckoutID == nil {
			return fmt.Errorf("invalid_operation: checkout operation has no checkout")
		}
		checkout, err := m.store.GetCheckoutInternal(ctx, *operation.CheckoutID)
		if err != nil || checkout == nil {
			return err
		}
		if checkout.Status == "ready" {
			return nil
		}
		if err := m.store.UpdateCheckoutState(ctx, checkout.ID, "preparing", nil); err != nil {
			return err
		}
		_, token, err := m.repositoryToken(ctx, machine, checkout.RepositoryFullName)
		if err != nil {
			return fmt.Errorf("github_token_failed: %w", err)
		}
		if err := m.runtime.PrepareCheckout(ctx, machine, services, checkout, token); err != nil {
			return err
		}
		if err := m.store.UpdateCheckoutState(ctx, checkout.ID, "ready", nil); err != nil {
			return err
		}
		return m.createEvent(ctx, machine, nil, "git", "checkout.ready", map[string]any{
			"checkout_id": checkout.ID, "issue_id": checkout.IssueID, "branch": checkout.WorkingBranch,
		})
	case domain.DevMachineOpTerminateTerminal:
		if operation.TerminalSessionID == nil {
			return fmt.Errorf("invalid_operation: terminal close operation has no session")
		}
		session, err := m.store.GetTerminalSessionInternal(ctx, *operation.TerminalSessionID)
		if err != nil || session == nil {
			return err
		}
		if session.MachineID != machine.ID || session.WorkspaceID != machine.WorkspaceID {
			return &terminalOperationError{code: "terminal_session_mismatch", message: "terminal session does not belong to the operation machine"}
		}
		if session.Status == "closed" {
			return nil
		}
		if err := m.runtime.TerminateTerminal(ctx, machine, services, session); err != nil {
			return fmt.Errorf("terminal_termination_failed: %w", err)
		}
		return m.store.CompleteTerminalSessionClose(ctx, session.ID)
	case domain.DevMachineOpSnapshotEnvironment:
		if operation.EnvironmentID == nil {
			return fmt.Errorf("invalid_operation: snapshot operation has no environment")
		}
		environment, err := m.store.GetEnvironment(ctx, machine.WorkspaceID, *operation.EnvironmentID)
		if err != nil || environment == nil {
			return err
		}
		if environment.Status == "delete_requested" {
			return nil
		}
		if environment.Status == "ready" {
			return nil
		}
		stable := machine.Status == machine.DesiredStatus && (machine.Status == domain.DevMachineStatusPaused || machine.Status == domain.DevMachineStatusStopped)
		if !stable {
			return m.failSnapshotOperation(ctx, operation, "environment_snapshot_state_changed",
				fmt.Sprintf("machine is %s with desired state %s", machine.Status, machine.DesiredStatus))
		}
		if err := m.store.UpdateEnvironmentState(ctx, environment.ID, "building", environment.ImageRef, nil); err != nil {
			return err
		}
		environment.Status = "building"
		digest, err := m.runtime.SnapshotEnvironment(ctx, machine, services, environment)
		if err != nil {
			_ = m.store.UpdateEnvironmentState(ctx, environment.ID, "failed", "", nil)
			return err
		}
		immutableImageID := normalizeImmutableImageID(digest)
		if !isImmutableLocalImageID(immutableImageID) {
			_ = m.store.UpdateEnvironmentState(ctx, environment.ID, "failed", "", nil)
			return fmt.Errorf("environment_snapshot_invalid_image_id: snapshot did not return an immutable local image ID")
		}
		if err := m.store.UpdateEnvironmentState(ctx, environment.ID, "ready", immutableImageID, &immutableImageID); err != nil {
			return err
		}
		environment.ImageRef = immutableImageID
		environment.ImageDigest = &immutableImageID
		return m.createEvent(ctx, machine, nil, "lifecycle", "environment.snapshot_ready", map[string]any{
			"environment_id": environment.ID, "image_ref": immutableImageID, "image_digest": immutableImageID,
		})
	case domain.DevMachineOpReconcile:
		return nil
	default:
		return fmt.Errorf("invalid_operation: unsupported action %s", operation.Action)
	}
}

func (m *Manager) failSnapshotOperation(ctx context.Context, operation *domain.DevMachineOperation, code, message string) error {
	if operation.EnvironmentID == nil {
		return &terminalOperationError{code: code, message: message + "; snapshot environment is missing"}
	}
	environment, err := m.store.GetEnvironment(ctx, operation.WorkspaceID, *operation.EnvironmentID)
	if err != nil {
		return err
	}
	if environment == nil {
		return &terminalOperationError{code: code, message: message + "; snapshot environment no longer exists"}
	}
	if environment.Status == "ready" || environment.Status == "delete_requested" {
		return nil
	}
	if environment.Status != "failed" {
		if err := m.store.UpdateEnvironmentState(ctx, environment.ID, "failed", "", nil); err != nil {
			return err
		}
	}
	return &terminalOperationError{code: code, message: message}
}

func (m *Manager) spawn(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, operation *domain.DevMachineOperation) error {
	updated, err := m.store.SetMachineStateForOperation(ctx, machine.ID, operation.Generation, false, domain.DevMachineStatusSpawning, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	// Provisioning may replace the developer container and its processes.
	m.previewsEnded(ctx, machine)
	runtimeServices := make([]domain.DevMachineService, 0, len(services))
	for _, service := range services {
		if service.ServiceType != "agent" {
			runtimeServices = append(runtimeServices, service)
		}
	}
	serviceSecrets, err := m.serviceSecrets(ctx, machine, runtimeServices)
	if err != nil {
		return err
	}
	networkName, volumeName, containers, err := m.runtime.Spawn(ctx, machine, runtimeServices, serviceSecrets)
	if err != nil {
		return err
	}
	for _, service := range runtimeServices {
		containerID := containers[service.ServiceKey]
		if err := m.store.UpdateServiceRuntime(ctx, service.ID, containerID, "running", "healthy", nil); err != nil {
			return err
		}
	}
	updated, err = m.store.SetMachineStateForOperation(ctx, machine.ID, operation.Generation, false, domain.DevMachineStatusRunning, &networkName, &volumeName, nil, nil)
	if err != nil {
		return err
	}
	if !updated {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = m.runtime.Teardown(cleanupCtx, machine, runtimeServices)
		return nil
	}
	return m.createEvent(ctx, machine, nil, "lifecycle", "machine.running", map[string]any{
		"docker_network_name": networkName, "workspace_volume_name": volumeName,
	})
}

func (m *Manager) registerActiveRun(machineID, runID uuid.UUID, cancel context.CancelFunc) {
	m.activeRunsMu.Lock()
	defer m.activeRunsMu.Unlock()
	if m.activeRuns[machineID] == nil {
		m.activeRuns[machineID] = make(map[uuid.UUID]context.CancelFunc)
	}
	m.activeRuns[machineID][runID] = cancel
}

func (m *Manager) unregisterActiveRun(machineID, runID uuid.UUID) {
	m.activeRunsMu.Lock()
	defer m.activeRunsMu.Unlock()
	delete(m.activeRuns[machineID], runID)
	if len(m.activeRuns[machineID]) == 0 {
		delete(m.activeRuns, machineID)
	}
}

func (m *Manager) cancelActiveRuns(ctx context.Context, machineID uuid.UUID, runID *uuid.UUID) {
	m.activeRunsMu.Lock()
	active := make(map[uuid.UUID]context.CancelFunc)
	for activeRunID, cancel := range m.activeRuns[machineID] {
		if runID == nil || activeRunID == *runID {
			active[activeRunID] = cancel
		}
	}
	m.activeRunsMu.Unlock()
	for activeRunID, cancel := range active {
		if run, err := m.store.GetAgentRunInternal(ctx, activeRunID); err == nil && run != nil && !terminalAgentStatus(run.Status) {
			run.Status = domain.DevMachineAgentRunStatusCancelled
			_ = m.store.CompleteAgentRun(ctx, run)
		}
		cancel()
	}
}

func (m *Manager) serviceSecrets(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) (map[string]map[string]string, error) {
	serviceSecrets := make(map[string]map[string]string, len(services))
	includeWorkspaceModel := false
	for _, service := range services {
		if service.ServiceType == "agent" {
			continue
		}
		if service.ServiceType == "ide" || service.ServiceType == "terminal" {
			includeWorkspaceModel = true
		}
		envVars, err := m.store.ListEnvVarsInternal(ctx, machine.ID, nil, service.ServiceType)
		if err != nil {
			return nil, err
		}
		values := make(map[string]string, len(envVars))
		for _, envVar := range envVars {
			value, err := cryptoutil.Decrypt(envVar.EncryptedValue, m.encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("secret_decryption_failed: %w", err)
			}
			values[envVar.Name] = value
		}
		serviceSecrets[service.ServiceKey] = values
	}
	if includeWorkspaceModel {
		// Terminals and the IDE get the workspace's default model server.
		modelSecrets, err := m.modelServerSecrets(ctx, machine.WorkspaceID, nil)
		if err != nil {
			return nil, err
		}
		for _, service := range services {
			if service.ServiceType != "ide" && service.ServiceType != "terminal" {
				continue
			}
			for name, value := range modelSecrets {
				serviceSecrets[service.ServiceKey][name] = value
			}
		}
	}
	if hasDockerService(services) {
		client, err := m.dockerClientSecrets(ctx, machine)
		if err != nil {
			return nil, err
		}
		for _, service := range services {
			if service.ServiceType != "ide" && service.ServiceType != "terminal" {
				continue
			}
			for name, value := range client {
				serviceSecrets[service.ServiceKey][name] = value
			}
		}
	}
	_, hasIDE := serviceSecrets["ide"]
	_, hasTerminal := serviceSecrets["terminal"]
	if hasIDE || hasTerminal {
		_, githubToken, err := m.repositoryToken(ctx, machine, "")
		if err == nil && githubToken != "" {
			if hasIDE {
				serviceSecrets["ide"]["GITHUB_TOKEN"] = githubToken
			} else {
				serviceSecrets["terminal"]["GITHUB_TOKEN"] = githubToken
			}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("github_token_failed: %w", err)
		}
	}
	return serviceSecrets, nil
}

// dockerClientSecrets decrypts the machine daemon's client TLS material.
func (m *Manager) dockerClientSecrets(ctx context.Context, machine *domain.DevMachine) (map[string]string, error) {
	envVars, err := m.store.ListEnvVarsInternal(ctx, machine.ID, nil, "docker-client")
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(envVars))
	for _, envVar := range envVars {
		value, err := cryptoutil.Decrypt(envVar.EncryptedValue, m.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("secret_decryption_failed: %w", err)
		}
		values[envVar.Name] = value
	}
	return values, nil
}

// modelServerSecrets is the environment that points Codex and Claude Code at a
// model server: the server with id, or the workspace default when id is nil.
// Codex reads the OpenAI-compatible URL, Claude Code the Anthropic one.
func (m *Manager) modelServerSecrets(ctx context.Context, workspaceID uuid.UUID, id *uuid.UUID) (map[string]string, error) {
	server, err := m.store.GetModelServer(ctx, workspaceID, id)
	if err != nil || server == nil {
		return nil, err
	}
	apiKey, err := cryptoutil.Decrypt(server.EncryptedAPIKey, m.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("secret_decryption_failed: %w", err)
	}
	fast := server.FastModel
	if fast == "" {
		fast = server.DefaultModel
	}
	secrets := map[string]string{
		"KUAYLE_MODEL_API_KEY":  apiKey,
		"KUAYLE_MODEL":          server.DefaultModel,
		"KUAYLE_MODEL_THINKING": server.Thinking,
		"KUAYLE_MODEL_LIST":     strings.Join(server.ModelList(), ","),
	}
	if server.OpenAIBaseURL != nil {
		secrets["KUAYLE_MODEL_OPENAI_BASE_URL"] = *server.OpenAIBaseURL
	}
	if server.AnthropicBaseURL != nil {
		secrets["ANTHROPIC_BASE_URL"] = *server.AnthropicBaseURL
		secrets["ANTHROPIC_AUTH_TOKEN"] = apiKey
		secrets["ANTHROPIC_MODEL"] = server.DefaultModel
		secrets["ANTHROPIC_DEFAULT_OPUS_MODEL"] = server.DefaultModel
		secrets["ANTHROPIC_DEFAULT_SONNET_MODEL"] = server.DefaultModel
		secrets["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = fast
		secrets["CLAUDE_CODE_SUBAGENT_MODEL"] = fast
		secrets["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		if server.Thinking != "off" {
			secrets["CLAUDE_CODE_EFFORT_LEVEL"] = server.Thinking
		}
	}
	return secrets, nil
}

// Keep injected public configuration separate from credentials: redacting values
// such as "1" or "high" corrupts native JSON and ordinary terminal output.
// Both maps are sealed together so recovery uses exactly the original policy.
func (m *Manager) agentRunSecrets(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, checkout *domain.DevMachineCheckout) (*domain.AgentRunSecretSnapshot, error) {
	if run.EncryptedOutputSecrets != nil {
		return m.decodeAgentRunSecrets(*run.EncryptedOutputSecrets)
	}
	if run.Status != domain.DevMachineAgentRunStatusQueued && run.Status != domain.DevMachineAgentRunStatusStarting {
		return nil, repository.ErrAgentRunSecretsUnavailable
	}
	envVars, err := m.store.ListEnvVarsInternal(ctx, machine.ID, &run.ProviderID, "agent")
	if err != nil {
		return nil, err
	}
	secrets := make(map[string]string, len(envVars))
	redactions := make(map[string]string)
	var allowedSecrets []string
	_ = json.Unmarshal(run.AllowedSecrets, &allowedSecrets)
	allowedSecretSet := make(map[string]bool, len(allowedSecrets))
	for _, name := range allowedSecrets {
		allowedSecretSet[name] = true
	}
	for _, envVar := range envVars {
		if !allowedSecretSet[envVar.Name] {
			continue
		}
		value, err := cryptoutil.Decrypt(envVar.EncryptedValue, m.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("secret_decryption_failed: %w", err)
		}
		secrets[envVar.Name] = value
		redactions[envVar.Name] = value
	}
	// A run uses only the model server recorded on it; without one the harness
	// relies on its own sign-in.
	if (run.ProviderID == "codex" || run.ProviderID == "claude-code") && run.ModelServerID != nil {
		modelSecrets, modelErr := m.modelServerSecrets(ctx, machine.WorkspaceID, run.ModelServerID)
		if modelErr != nil {
			return nil, modelErr
		}
		if run.Model != "" && len(modelSecrets) > 0 {
			modelSecrets["KUAYLE_MODEL"] = run.Model
			if _, ok := modelSecrets["ANTHROPIC_MODEL"]; ok {
				modelSecrets["ANTHROPIC_MODEL"] = run.Model
			}
		}
		if run.Thinking != "" && len(modelSecrets) > 0 {
			delete(modelSecrets, "CLAUDE_CODE_EFFORT_LEVEL")
			if run.Thinking != "off" {
				modelSecrets["CLAUDE_CODE_EFFORT_LEVEL"] = run.Thinking
			}
		}
		for name, value := range modelSecrets {
			secrets[name] = value
			delete(redactions, name)
			if name == "KUAYLE_MODEL_API_KEY" || name == "ANTHROPIC_AUTH_TOKEN" {
				redactions[name] = value
			}
		}
	}
	if err := m.agentSignInSecrets(ctx, run, secrets, redactions); err != nil {
		return nil, err
	}
	// Attempts on a machine with a Docker daemon may use it like the
	// developer container; the client key is redacted from their output.
	if services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID); err != nil {
		return nil, err
	} else if hasDockerService(services) {
		client, err := m.dockerClientSecrets(ctx, machine)
		if err != nil {
			return nil, err
		}
		for name, value := range client {
			secrets[name] = value
		}
		secrets["KUAYLE_DOCKER_HOST"] = "tcp://" + machine.RoutingKey + "-docker:" + strconv.Itoa(dockerDaemonPort)
		if value, ok := client["KUAYLE_DOCKER_KEY"]; ok {
			redactions["KUAYLE_DOCKER_KEY"] = value
		}
	}
	githubToken := ""
	if run.PushBranch {
		_, githubToken, err = m.repositoryToken(ctx, machine, checkoutRepositoryFullName(machine, checkout))
		if err != nil || githubToken == "" {
			if err == nil {
				err = errors.New("no linked GitHub App installation")
			}
			return nil, fmt.Errorf("github_token_failed: repository-scoped GitHub App token is unavailable: %w", err)
		}
		secrets["GITHUB_TOKEN"] = githubToken
		redactions["GITHUB_TOKEN"] = githubToken
	}

	payload, err := json.Marshal(domain.AgentRunSecretSnapshot{Version: 1, Environment: secrets, Redactions: redactions})
	if err != nil {
		return nil, repository.ErrAgentRunSecretsUnavailable
	}
	encrypted, err := cryptoutil.Encrypt(string(payload), m.encryptionKey)
	if err != nil {
		return nil, repository.ErrAgentRunSecretsUnavailable
	}
	sealed, err := m.store.SealAgentRunSecrets(ctx, run.ID, encrypted)
	if err != nil {
		return nil, err
	}
	return m.decodeAgentRunSecrets(sealed)
}
func (m *Manager) decodeAgentRunSecrets(encrypted string) (*domain.AgentRunSecretSnapshot, error) {
	payload, err := cryptoutil.Decrypt(encrypted, m.encryptionKey)
	if err != nil {
		return nil, repository.ErrAgentRunSecretsUnavailable
	}
	snapshot, err := domain.DecodeAgentRunSecretSnapshot(payload)
	if err != nil {
		return nil, repository.ErrAgentRunSecretsUnavailable
	}
	return snapshot, nil
}

func (m *Manager) runAgent(ctx context.Context, machine *domain.DevMachine, operation *domain.DevMachineOperation) error {
	if operation.AgentRunID == nil {
		return fmt.Errorf("invalid_operation: agent operation has no run")
	}
	run, err := m.store.GetAgentRunInternal(ctx, *operation.AgentRunID)
	if err != nil || run == nil {
		return err
	}
	if terminalAgentStatus(run.Status) {
		return nil
	}
	if machine.RepositoryAffinityID != nil && run.CheckoutID == nil {
		return &terminalOperationError{code: "checkout_not_ready", message: "repository-linked agent run has no checkout"}
	}
	var checkout *domain.DevMachineCheckout
	if run.CheckoutID != nil {
		checkout, err = m.store.GetCheckoutInternal(ctx, *run.CheckoutID)
		if err != nil {
			return err
		}
		if checkout == nil || checkout.Status != "ready" {
			return fmt.Errorf("checkout_not_ready: agent checkout is unavailable")
		}
	}
	providerRecord, err := m.store.GetProvider(ctx, machine.WorkspaceID, machine.ID, run.ProviderID)
	if err != nil || providerRecord == nil {
		return fmt.Errorf("provider_not_found: %s", run.ProviderID)
	}
	provider, ok := m.agents.Get(run.ProviderID)
	if !ok {
		return fmt.Errorf("provider_not_found: %s", run.ProviderID)
	}
	currentMachine, err := m.store.GetMachineInternal(ctx, machine.ID)
	if err != nil {
		return err
	}
	if currentMachine == nil || currentMachine.Generation != operation.Generation || currentMachine.DesiredStatus != domain.DevMachineStatusRunning {
		m.cancelStaleAgentRun(ctx, run.ID)
		return nil
	}
	if err := m.store.AuthorizeAgentRun(ctx, run.ID); err != nil {
		if errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return m.cancelUnauthorizedRun(ctx, run)
		}
		if errors.Is(err, sql.ErrNoRows) {
			current, currentErr := m.store.GetAgentRunInternal(ctx, run.ID)
			if currentErr == nil && current != nil && terminalAgentStatus(current.Status) {
				return nil
			}
		}
		return err
	}
	secrets, err := m.agentRunSecrets(ctx, machine, run, checkout)
	if err != nil {
		if errors.Is(err, repository.ErrAgentRunSecretsUnavailable) {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
			cleanupErr := m.runtime.CancelAgent(cleanupCtx, run)
			cancelCleanup()
			if cleanupErr != nil {
				return fmt.Errorf("agent_recovery_cleanup_failed: %w", cleanupErr)
			}
			return &terminalOperationError{code: "agent_recovery_credentials_unavailable", message: "Native attempt cannot recover its original credentials"}
		}
		return err
	}
	if err := m.store.MarkAgentRunStarting(ctx, run.ID); err != nil {
		if errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return m.cancelUnauthorizedRun(ctx, run)
		}
		if errors.Is(err, sql.ErrNoRows) {
			current, currentErr := m.store.GetAgentRunInternal(ctx, run.ID)
			if currentErr == nil && current != nil && terminalAgentStatus(current.Status) {
				return nil
			}
		}
		return err
	}
	if run.Status == domain.DevMachineAgentRunStatusQueued {
		run.Status = domain.DevMachineAgentRunStatusStarting
	}
	if err := m.store.AuthorizeAgentRun(ctx, run.ID); err != nil {
		if errors.Is(err, repository.ErrAgentTaskUnauthorized) || errors.Is(err, sql.ErrNoRows) {
			return m.cancelUnauthorizedRun(ctx, run)
		}
		return err
	}
	m.stageAttachments(ctx, machine, run, checkout)
	m.prepareOutbox(ctx, machine, run, checkout)
	service := &domain.DevMachineService{
		ID: uuid.New(), WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, ServiceType: "agent",
		ServiceKey: "agent-" + run.ID.String(), ContainerName: "kuayle-" + machine.RoutingKey + "-agent-" + run.ID.String(),
		ImageRef: providerRecord.ImageRef, InternalHost: "agent-" + run.ID.String(), InternalPort: 8080,
		Status: "pending", HealthStatus: "unknown",
	}
	if err := m.store.CreateRuntimeService(ctx, service); err != nil {
		return err
	}
	runContext, cancel := context.WithTimeout(ctx, time.Duration(run.MaxRuntimeSeconds)*time.Second)
	defer cancel()
	output := newAgentOutputBuffer(secrets.Redactions, func(stream string, sequence int64, source string, content string, truncated bool) error {
		var events []domain.DevMachineEvent
		if stream == "stdout" && !truncated {
			for _, parsed := range provider.ParseEvents([]byte(content)) {
				if id := codexThreadID(run, parsed); id != "" {
					writeCtx, cancelWrite := context.WithTimeout(runContext, 5*time.Second)
					err := m.store.RecordNativeSession(writeCtx, run.ID, id)
					cancelWrite()
					if err != nil {
						return fmt.Errorf("agent_session_persistence_failed: %w", err)
					}
				}
				payload := parsed.Payload
				if len(payload) == 0 {
					payload, _ = json.Marshal(map[string]any{"message": parsed.Message})
				}
				eventType := parsed.Type
				if len(eventType) == 0 || len(eventType) > 128 {
					eventType = "provider.event"
				}
				events = append(events, domain.DevMachineEvent{WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, Source: "agent", EventType: eventType, Payload: payload})
			}
		}
		writeCtx, cancelWrite := context.WithTimeout(runContext, 5*time.Second)
		defer cancelWrite()
		if err := m.store.CreateAgentOutput(writeCtx, &domain.DevMachineLogChunk{
			WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, ServiceID: &service.ID,
			Stream: stream, Sequence: sequence, SourceKey: source, Content: content, Truncated: truncated,
		}, events); err != nil {
			return fmt.Errorf("agent_output_persistence_failed: %w", err)
		}
		return nil
	})
	observer := &AgentObserver{Authorize: func() error { return m.store.AuthorizeAgentRun(runContext, run.ID) }, Output: output.WriteDocker, Started: func(containerID string) error {
		if err := m.store.UpdateAgentRunStarted(runContext, run.ID); err != nil {
			return err
		}
		if run.Status != domain.DevMachineAgentRunStatusWaitingInput {
			run.Status = domain.DevMachineAgentRunStatusRunning
		}
		return m.store.UpdateServiceRuntime(runContext, service.ID, containerID, "running", "healthy", nil)
	}}
	execution, executionErr := m.runtime.RunAgent(runContext, machine, run, providerRecord, checkout, secrets.Environment, observer)
	if used := secrets.Environment[signInSecretNames[run.ProviderID]]; used != "" {
		returnCtx, cancelReturn := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if err := m.returnSignIn(returnCtx, machine, run, used); err != nil {
			log.WithField("agent_run_id", run.ID).WithError(err).Warn("refreshed agent sign-in could not be kept")
		}
		cancelReturn()
	}
	if executionErr == nil {
		executionErr = output.Finish()
	}
	timedOut := errors.Is(executionErr, context.DeadlineExceeded) || errors.Is(runContext.Err(), context.DeadlineExceeded)
	if timedOut {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr := m.runtime.CancelAgent(cleanupCtx, run)
		cleanupCancel()
		if cleanupErr != nil {
			return fmt.Errorf("agent_timeout_cleanup_failed: %w", cleanupErr)
		}
	}
	if run.Mode == "interactive" && executionErr == nil {
		return m.createEvent(ctx, machine, &run.ID, "agent", "agent_run.interactive_started", map[string]any{"provider_id": run.ProviderID})
	}
	if execution == nil {
		execution = &AgentExecution{ExitCode: -1}
	}
	services, _ := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	// Messages the agent left are recorded before the next step can deliver
	// them, without waiting for the next port scan.
	outboxCtx, cancelOutbox := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	m.collectOutbox(outboxCtx, machine, services)
	cancelOutbox()
	stdout, stderr := output.Text("stdout"), output.Text("stderr")
	result := provider.ParseResult(stdout, stderr, execution.ExitCode)
	repositoryFullName := checkoutRepositoryFullName(machine, checkout)
	if result.PullRequestURL != "" {
		if safeURL, ok := safeGitHubPullRequestURL(result.PullRequestURL, repositoryFullName); ok {
			result.PullRequestURL = safeURL
		} else {
			result.PullRequestURL = ""
			result.RiskNotes = append(result.RiskNotes, "Agent-reported pull request URL was ignored because it did not match the selected GitHub repository.")
		}
	}
	currentRun, currentErr := m.store.GetAgentRunInternal(ctx, run.ID)
	if currentErr == nil && currentRun != nil && currentRun.Status == domain.DevMachineAgentRunStatusCancelled {
		result.Status = "cancelled"
		run.Status = domain.DevMachineAgentRunStatusCancelled
	} else if timedOut {
		result.Status = "timeout"
		run.Status = domain.DevMachineAgentRunStatusTimeout
	} else if executionErr != nil || execution.ExitCode != 0 || result.Status == "failed" {
		result.Status = "failed"
		run.Status = domain.DevMachineAgentRunStatusFailed
	} else {
		run.Status = domain.DevMachineAgentRunStatusSucceeded
	}
	if gitState, gitErr := m.runtime.GitState(ctx, machine, services, checkout); gitErr == nil {
		result.Branch = gitState.Branch
		result.Commits = gitState.Commits
		result.ChangedFiles = gitState.ChangedFiles
		run.Diff, run.DiffTruncated = gitState.Diff, gitState.DiffTruncated
	}
	resultJSON, _ := json.Marshal(result)
	resultRaw := json.RawMessage(resultJSON)
	changedFiles, _ := json.Marshal(result.ChangedFiles)
	commits, _ := json.Marshal(result.Commits)
	testsRun, _ := json.Marshal(result.TestsRun)
	riskNotes, _ := json.Marshal(result.RiskNotes)
	run.Result = &resultRaw
	run.Summary = &result.Summary
	run.ChangedFiles = changedFiles
	run.Commits = commits
	run.TestsRun = testsRun
	run.TestStatus = result.TestStatus
	run.RiskNotes = riskNotes
	run.ExitCode = &execution.ExitCode
	if result.Branch != "" {
		run.Branch = &result.Branch
		_ = m.persistGitRef(ctx, machine, checkout, run, "branch", result.Branch, "", nil, "")
	}
	run.PullRequestURL = nil
	if result.PullRequestURL != "" {
		run.PullRequestURL = &result.PullRequestURL
	}
	if executionErr != nil {
		message := safeError(executionErr)
		run.ErrorMessage = &message
	}
	for _, commit := range result.Commits {
		repositoryFullName := checkoutRepositoryFullName(machine, checkout)
		_ = m.persistGitRef(ctx, machine, checkout, run, "commit", result.Branch, commit, nil, "https://github.com/"+repositoryFullName+"/commit/"+commit)
	}
	if err := m.store.CompleteAgentRun(ctx, run); err != nil {
		return err
	}
	if executionErr == nil {
		_ = m.store.UpdateServiceRuntime(ctx, service.ID, execution.ContainerID, "stopped", "stopped", nil)
	}
	m.agentRunsCompleted.Add(1)
	return m.createEvent(ctx, machine, &run.ID, "agent", "agent_run.completed", map[string]any{
		"provider_id": run.ProviderID, "status": run.Status, "exit_code": execution.ExitCode,
	})
}

func (m *Manager) reconcile(ctx context.Context) error {
	now := time.Now().UTC()
	m.lastReconcileUnix.Store(now.Unix())
	if _, err := m.store.PurgeExpiredRuntimeCredentials(ctx, now); err != nil {
		log.WithError(err).Warn("expired runtime credential purge failed")
	}
	if _, err := m.store.PurgeAccessLogs(ctx, now.Add(-gatewayAccessLogRetention), gatewayAccessLogPurgeBatch); err != nil {
		log.WithError(err).Warn("gateway access log purge failed")
	}
	timedOutRuns, err := m.store.ListTimedOutAgentRuns(ctx, 100)
	if err != nil {
		return err
	}
	for i := range timedOutRuns {
		run := &timedOutRuns[i]
		if err := m.runtime.CancelAgent(ctx, run); err != nil {
			log.WithField("agent_run_id", run.ID).WithError(err).Warn("timed out agent cleanup failed")
			continue
		}
		exitCode := -1
		run.Status = domain.DevMachineAgentRunStatusTimeout
		run.ExitCode = &exitCode
		if len(run.ChangedFiles) == 0 {
			run.ChangedFiles = json.RawMessage(`[]`)
			run.Commits = json.RawMessage(`[]`)
			run.TestsRun = json.RawMessage(`[]`)
			run.RiskNotes = json.RawMessage(`[]`)
		}
		_ = m.store.CompleteAgentRun(ctx, run)
		machine, _ := m.store.GetMachineInternal(ctx, run.MachineID)
		if machine != nil {
			services, _ := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
			for _, service := range services {
				if service.AgentRunID != nil && *service.AgentRunID == run.ID && service.ContainerID != nil {
					_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "stopped", "stopped", nil)
				}
			}
			_ = m.createEvent(ctx, machine, &run.ID, "agent", "agent_run.timeout", map[string]any{"provider_id": run.ProviderID})
		}
	}

	if _, err := m.store.ReconcileOrphanedEnvironments(ctx, 100); err != nil {
		return err
	}
	if _, err := m.store.ReconcileOrphanedCheckouts(ctx, 100); err != nil {
		return err
	}
	deleteRequestedEnvironments, err := m.store.ListDeleteRequestedEnvironments(ctx, 100)
	if err != nil {
		return err
	}
	for i := range deleteRequestedEnvironments {
		environment := &deleteRequestedEnvironments[i]
		if err := m.runtime.DeleteEnvironmentImage(ctx, environment); err != nil {
			log.WithField("environment_id", environment.ID).WithError(err).Warn("development environment image cleanup failed")
			continue
		}
		if err := m.store.DeleteEnvironment(ctx, environment.WorkspaceID, environment.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithField("environment_id", environment.ID).WithError(err).Warn("development environment record deletion failed")
		}
	}

	deleteRequestedMachines, err := m.store.ListPermanentDeleteRequests(ctx, 100)
	if err != nil {
		return err
	}
	for i := range deleteRequestedMachines {
		machine := &deleteRequestedMachines[i]
		if domain.DevMachineSafelyPurgeable(machine) {
			if err := m.store.PurgePermanentDeleteRequest(ctx, machine.WorkspaceID, machine.ID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					if _, requestErr := m.store.RequestPermanentDelete(ctx, machine.WorkspaceID, machine.ID, nil); requestErr != nil && !errors.Is(requestErr, sql.ErrNoRows) {
						log.WithField("machine_id", machine.ID).WithError(requestErr).Warn("permanent delete teardown queue failed")
					}
				} else {
					log.WithField("machine_id", machine.ID).WithError(err).Warn("permanent machine purge failed")
				}
			}
			continue
		}
		if _, err := m.store.RequestPermanentDelete(ctx, machine.WorkspaceID, machine.ID, nil); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithField("machine_id", machine.ID).WithError(err).Warn("permanent delete teardown queue failed")
		}
	}

	idleMachines, err := m.store.ListIdleMachines(ctx, 100)
	if err != nil {
		return err
	}
	for i := range idleMachines {
		machine := &idleMachines[i]
		operation := &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
			Action: domain.DevMachineOpPause, Status: domain.DevMachineOpStatusPending,
			Generation: machine.Generation + 1, IdempotencyKey: fmt.Sprintf("idle-pause:%d", machine.Generation+1), MaxAttempts: 5,
		}
		if err := m.store.SetDesiredAndEnqueue(ctx, machine.WorkspaceID, machine.ID, domain.DevMachineStatusPaused, operation); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithField("machine_id", machine.ID).WithError(err).Warn("idle pause queue failed")
		}
	}

	expired, err := m.store.ListExpiredMachines(ctx, 100)
	if err != nil {
		return err
	}
	for i := range expired {
		machine := &expired[i]
		if err := m.store.SetMachineState(ctx, machine.ID, domain.DevMachineStatusExpired, nil, nil, nil, nil); err != nil {
			continue
		}
		operation := &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
			Action: domain.DevMachineOpTeardown, Status: domain.DevMachineOpStatusPending,
			Generation: machine.Generation + 1, IdempotencyKey: fmt.Sprintf("ttl-teardown:%d", machine.Generation+1), MaxAttempts: 10,
		}
		_ = m.store.SetDesiredAndEnqueue(ctx, machine.WorkspaceID, machine.ID, domain.DevMachineStatusDestroyed, operation)
		_ = m.createEvent(ctx, machine, nil, "lifecycle", "machine.expired", nil)
	}

	hostDiskFloorBreached := m.reconcileHostDiskFloor(ctx)
	const runtimePageSize = 100
	for offset := 0; ; offset += runtimePageSize {
		machines, err := m.store.ListRuntimeMachines(ctx, runtimePageSize, offset)
		if err != nil {
			return err
		}
		for i := range machines {
			machine := &machines[i]
			services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
			if err != nil {
				continue
			}
			if hostDiskFloorBreached &&
				machine.Status == domain.DevMachineStatusRunning &&
				machine.DesiredStatus == domain.DevMachineStatusRunning {
				hostDisk := HostDiskUsage{
					TotalBytes: m.hostDiskTotalBytes.Load(),
					FreeBytes:  m.hostDiskFreeBytes.Load(),
				}
				minFreeBytes := int64(m.diskQuota.HostMinFreeGB) * 1024 * 1024 * 1024
				operation := &domain.DevMachineOperation{
					ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
					Action: domain.DevMachineOpStop, Status: domain.DevMachineOpStatusPending,
					Generation: machine.Generation + 1, IdempotencyKey: fmt.Sprintf("host-disk-floor:%d", machine.Generation), MaxAttempts: 3,
				}
				if err := m.store.SetDesiredAndEnqueue(ctx, machine.WorkspaceID, machine.ID, domain.DevMachineStatusStopped, operation); err != nil && !errors.Is(err, sql.ErrNoRows) {
					log.WithFields(log.Fields{"machine_id": machine.ID, "workspace_id": machine.WorkspaceID}).WithError(err).Warn("host disk floor stop queue failed")
				}
				_ = m.createEvent(ctx, machine, nil, "collector", "resource.host_disk_floor", map[string]any{
					"free_bytes": hostDisk.FreeBytes, "total_bytes": hostDisk.TotalBytes,
					"min_free_bytes": minFreeBytes, "min_free_percent": m.diskQuota.HostMinFreePercent,
				})
				log.WithFields(log.Fields{
					"machine_id": machine.ID, "workspace_id": machine.WorkspaceID,
					"free_bytes": hostDisk.FreeBytes, "total_bytes": hostDisk.TotalBytes,
					"min_free_bytes": minFreeBytes, "min_free_percent": m.diskQuota.HostMinFreePercent,
				}).Warn("host disk free-space floor breached; stopping dev machine")
				continue
			}
			if machine.Status != machine.DesiredStatus {
				switch machine.DesiredStatus {
				case domain.DevMachineStatusDestroyed:
					m.cancelActiveRuns(ctx, machine.ID, nil)
					_ = m.store.RevokeMachineAccess(ctx, machine.ID)
					if err := m.runtime.Teardown(ctx, machine, services); err != nil {
						log.WithField("machine_id", machine.ID).WithError(err).Warn("teardown reconciliation failed")
						continue
					}
					for _, service := range services {
						if service.ContainerID != nil {
							_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "destroyed", "stopped", nil)
						}
					}
					updated, stateErr := m.store.SetMachineStateForOperation(ctx, machine.ID, machine.Generation, true, domain.DevMachineStatusDestroyed, nil, nil, nil, nil)
					if stateErr == nil && updated {
						_ = m.createEvent(ctx, machine, nil, "lifecycle", "machine.destroyed", nil)
					}
					continue
				case domain.DevMachineStatusStopped:
					if err := m.runtime.Stop(ctx, machine, services); err != nil {
						log.WithField("machine_id", machine.ID).WithError(err).Warn("stop reconciliation failed")
						continue
					}
					for _, service := range services {
						if service.ContainerID != nil {
							_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "stopped", "stopped", nil)
						}
					}
					_, _ = m.store.SetMachineStateForOperation(ctx, machine.ID, machine.Generation, false, domain.DevMachineStatusStopped, nil, nil, nil, nil)
					continue
				case domain.DevMachineStatusPaused:
					if err := m.store.RevokeMachineAccess(ctx, machine.ID); err != nil {
						log.WithField("machine_id", machine.ID).WithError(err).Warn("pause access revocation failed")
						continue
					}
					if err := m.runtime.Pause(ctx, machine, services); err != nil {
						log.WithField("machine_id", machine.ID).WithError(err).Warn("pause reconciliation failed")
						continue
					}
					for _, service := range services {
						if service.ContainerID != nil {
							_ = m.store.UpdateServiceRuntime(ctx, service.ID, *service.ContainerID, "paused", "paused", nil)
						}
					}
					_, _ = m.store.SetMachineStateForOperation(ctx, machine.ID, machine.Generation, false, domain.DevMachineStatusPaused, nil, nil, nil, nil)
					continue
				}
			}
			if machine.DesiredStatus == domain.DevMachineStatusRunning {
				inspection, inspectErr := m.runtime.Inspect(ctx, machine, services)
				if inspectErr != nil {
					log.WithField("machine_id", machine.ID).WithError(inspectErr).Warn("runtime inspection failed")
					continue
				}
				if !runtimeReady(inspection, services) {
					runtimeServices := make([]domain.DevMachineService, 0, len(services))
					for _, service := range services {
						if service.ServiceType != "agent" {
							runtimeServices = append(runtimeServices, service)
						}
					}
					secrets, secretErr := m.serviceSecrets(ctx, machine, runtimeServices)
					if secretErr != nil {
						log.WithField("machine_id", machine.ID).WithError(secretErr).Warn("runtime recovery secrets failed")
						continue
					}
					networkName, volumeName, containers, spawnErr := m.runtime.Spawn(ctx, machine, runtimeServices, secrets)
					if spawnErr != nil {
						log.WithField("machine_id", machine.ID).WithError(spawnErr).Warn("runtime recovery failed")
						continue
					}
					for _, service := range runtimeServices {
						if containerID := containers[service.ServiceKey]; containerID != "" {
							_ = m.store.UpdateServiceRuntime(ctx, service.ID, containerID, "running", "healthy", nil)
						}
					}
					updated, stateErr := m.store.SetMachineStateForOperation(ctx, machine.ID, machine.Generation, false, domain.DevMachineStatusRunning, &networkName, &volumeName, nil, nil)
					if stateErr != nil {
						log.WithField("machine_id", machine.ID).WithError(stateErr).Warn("runtime recovery state update failed")
						continue
					}
					if updated {
						_ = m.createEvent(ctx, machine, nil, "lifecycle", "machine.recovered", nil)
					}
					continue
				}
				for _, service := range services {
					if actual, ok := inspection.Services[service.ServiceKey]; ok && actual.Exists {
						_ = m.store.UpdateServiceRuntime(ctx, service.ID, actual.ContainerID, "running", actual.HealthStatus, nil)
					}
				}
				if machine.Status != domain.DevMachineStatusRunning {
					updated, stateErr := m.store.SetMachineStateForOperation(ctx, machine.ID, machine.Generation, false, domain.DevMachineStatusRunning, &inspection.NetworkName, &inspection.VolumeName, nil, nil)
					if stateErr != nil || !updated {
						continue
					}
					machine.Status = domain.DevMachineStatusRunning
					_ = m.createEvent(ctx, machine, nil, "lifecycle", "machine.recovered", nil)
				}
			}
			if machine.Status != domain.DevMachineStatusRunning {
				continue
			}
			usage, err := m.runtime.Stats(ctx, machine, services)
			if err != nil {
				log.WithField("machine_id", machine.ID).WithError(err).Warn("resource sampling failed")
				continue
			}
			_ = m.store.CreateResourceSample(ctx, &domain.DevMachineResourceSample{
				WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, CPUPercent: usage.CPUPercent, MemoryBytes: usage.MemoryBytes,
				DiskBytes: usage.DiskBytes, Pids: usage.Pids, NetworkRxBytes: usage.NetworkRxBytes,
				NetworkTxBytes: usage.NetworkTxBytes,
			})
			_ = m.store.UpdateVolumeUsage(ctx, machine.ID, usage.DiskBytes)
			limit := int64(machine.DiskGB) * 1024 * 1024 * 1024
			if usage.DiskBytes >= limit*9/10 && usage.DiskBytes < limit && m.claimDiskQuotaWarning(machine.ID, machine.Generation) {
				_ = m.createEvent(ctx, machine, nil, "collector", "resource.disk_quota_warning", map[string]any{
					"used_bytes": usage.DiskBytes, "limit_bytes": limit, "percent": usage.DiskBytes * 100 / limit,
				})
			}
			if usage.DiskBytes >= limit {
				operation := &domain.DevMachineOperation{
					ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
					Action: domain.DevMachineOpStop, Status: domain.DevMachineOpStatusPending,
					Generation: machine.Generation + 1, IdempotencyKey: fmt.Sprintf("disk-quota:%d", machine.Generation), MaxAttempts: 3,
				}
				_ = m.store.SetDesiredAndEnqueue(ctx, machine.WorkspaceID, machine.ID, domain.DevMachineStatusStopped, operation)
				_ = m.createEvent(ctx, machine, nil, "collector", "resource.disk_quota_exceeded", map[string]any{"used_bytes": usage.DiskBytes, "limit_bytes": limit})
			}
		}
		if len(machines) < runtimePageSize {
			break
		}
	}
	return nil
}

func (m *Manager) reconcileHostDiskFloor(ctx context.Context) bool {
	if !m.diskQuota.monitored() {
		return false
	}
	usage, err := m.runtime.HostDiskUsage(ctx)
	if err != nil {
		if !errors.Is(err, ErrHostDiskUnknown) {
			log.WithError(err).Warn("host disk usage unavailable")
		}
		return m.hostDiskBreached.Load()
	}
	m.hostDiskFreeBytes.Store(usage.FreeBytes)
	m.hostDiskTotalBytes.Store(usage.TotalBytes)
	minFreeBytes := int64(m.diskQuota.HostMinFreeGB) * 1024 * 1024 * 1024
	breached := usage.FreeBytes < minFreeBytes ||
		usage.TotalBytes > 0 && usage.FreeBytes*100/usage.TotalBytes < int64(m.diskQuota.HostMinFreePercent)
	m.hostDiskBreached.Store(breached)
	return breached
}

func boolMetric(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func (m *Manager) claimDiskQuotaWarning(machineID uuid.UUID, generation int64) bool {
	m.diskQuotaWarnMu.Lock()
	defer m.diskQuotaWarnMu.Unlock()
	if warnedGeneration, warned := m.diskQuotaWarnings[machineID]; warned && warnedGeneration == generation {
		return false
	}
	m.diskQuotaWarnings[machineID] = generation
	return true
}

func runtimeReady(inspection RuntimeInspection, services []domain.DevMachineService) bool {
	if !inspection.NetworkExists || !inspection.VolumeExists || !inspection.GatewayAttached {
		return false
	}
	for _, service := range services {
		if service.ServiceType == "agent" {
			continue
		}
		actual, ok := inspection.Services[service.ServiceKey]
		if !ok || !actual.Exists || !actual.Running || !actual.OnNetwork || actual.HealthStatus != "healthy" {
			return false
		}
	}
	return true
}

func (m *Manager) setStateAndEventForOperation(ctx context.Context, machine *domain.DevMachine, operation *domain.DevMachineOperation, status domain.DevMachineStatus, eventType string, payload map[string]any, allowStale bool) error {
	var networkName, volumeName *string
	if payload != nil {
		if value, ok := payload["docker_network_name"].(string); ok {
			networkName = &value
		}
		if value, ok := payload["workspace_volume_name"].(string); ok {
			volumeName = &value
		}
	}
	updated, err := m.store.SetMachineStateForOperation(ctx, machine.ID, operation.Generation, allowStale, status, networkName, volumeName, nil, nil)
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	return m.createEvent(ctx, machine, nil, "lifecycle", eventType, payload)
}

func (m *Manager) createEvent(ctx context.Context, machine *domain.DevMachine, runID *uuid.UUID, source, eventType string, payload any) error {
	data, _ := json.Marshal(payload)
	if payload == nil {
		data = []byte(`{}`)
	}
	return m.store.CreateEvent(ctx, &domain.DevMachineEvent{
		WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: runID,
		Source: source, EventType: eventType, Payload: data, OccurredAt: time.Now().UTC(),
	})
}

func redact(value string, secrets map[string]string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func terminalAgentStatus(status domain.DevMachineAgentRunStatus) bool {
	switch status {
	case domain.DevMachineAgentRunStatusSucceeded, domain.DevMachineAgentRunStatusFailed,
		domain.DevMachineAgentRunStatusCancelled, domain.DevMachineAgentRunStatusTimeout:
		return true
	default:
		return false
	}
}

func lifecycleFailureAffectsMachine(action domain.DevMachineOperationAction) bool {
	switch action {
	case domain.DevMachineOpSpawn, domain.DevMachineOpStart, domain.DevMachineOpStop, domain.DevMachineOpPause, domain.DevMachineOpTeardown:
		return true
	default:
		return false
	}
}

func (m *Manager) failAgentRun(ctx context.Context, runID uuid.UUID, runErr error) {
	run, err := m.store.GetAgentRunInternal(ctx, runID)
	if err != nil || run == nil || terminalAgentStatus(run.Status) {
		return
	}
	exitCode := -1
	message := safeError(runErr)
	run.Status = domain.DevMachineAgentRunStatusFailed
	run.ExitCode = &exitCode
	run.ErrorMessage = &message
	run.ChangedFiles = json.RawMessage(`[]`)
	run.Commits = json.RawMessage(`[]`)
	run.TestsRun = json.RawMessage(`[]`)
	run.RiskNotes = json.RawMessage(`[]`)
	_ = m.store.CompleteAgentRun(ctx, run)
}

func (m *Manager) cancelStaleAgentRun(ctx context.Context, runID uuid.UUID) {
	run, err := m.store.GetAgentRunInternal(ctx, runID)
	if err != nil || run == nil || terminalAgentStatus(run.Status) {
		return
	}
	exitCode := -1
	run.Status = domain.DevMachineAgentRunStatusCancelled
	run.ExitCode = &exitCode
	run.ChangedFiles = json.RawMessage(`[]`)
	run.Commits = json.RawMessage(`[]`)
	run.TestsRun = json.RawMessage(`[]`)
	run.RiskNotes = json.RawMessage(`[]`)
	_ = m.store.CompleteAgentRun(ctx, run)
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if index := strings.IndexByte(message, ':'); index > 0 && index < 128 {
		code := strings.ReplaceAll(message[:index], " ", "_")
		return strings.ToLower(code)
	}
	return "runtime_error"
}

func (m *Manager) repositoryToken(ctx context.Context, machine *domain.DevMachine, repositoryFullName string) (githubAPI, string, error) {
	if repositoryFullName == "" {
		repositoryFullName = checkoutRepositoryFullName(machine, nil)
	}
	repoOwner, repoName, ok := splitRepositoryFullName(repositoryFullName)
	if !ok {
		return nil, "", sql.ErrNoRows
	}
	installationID, err := m.store.GetGitHubInstallationID(ctx, machine.WorkspaceID, repoOwner+"/"+repoName)
	if err != nil {
		return nil, "", err
	}
	client := m.github
	if client == nil {
		config, err := m.store.GetGitHubAppConfig(ctx, machine.WorkspaceID)
		if err != nil {
			return nil, "", err
		}
		if config == nil || len(m.githubKey) == 0 {
			return nil, "", sql.ErrNoRows
		}
		privateKey, err := cryptoutil.Decrypt(config.PrivateKey, m.githubKey)
		if err != nil {
			return nil, "", fmt.Errorf("github_app_decryption_failed: %w", err)
		}
		client, err = githubclient.NewClient(config.AppID, privateKey)
		if err != nil {
			return nil, "", err
		}
	}
	token, expiresAt, err := client.GetRepositoryInstallationToken(installationID, repoName)
	if err != nil {
		return nil, "", err
	}
	if token == "" {
		return nil, "", errors.New("empty GitHub installation token")
	}
	if err := m.registerRuntimeCredential(ctx, machine.ID, token, expiresAt); err != nil {
		return nil, "", fmt.Errorf("runtime_credential_registration_failed: %w", err)
	}
	return client, token, nil
}

func (m *Manager) registerRuntimeCredential(ctx context.Context, machineID uuid.UUID, token string, expiresAt time.Time) error {
	fingerprint := sha256.Sum256([]byte(token))
	encrypted, err := cryptoutil.Encrypt(token, m.encryptionKey)
	if err != nil {
		return fmt.Errorf("credential_encryption_failed: %w", err)
	}
	return m.store.UpsertRuntimeCredential(ctx, &domain.DevMachineRuntimeCredential{
		ID: uuid.New(), MachineID: machineID,
		Scope:                domain.DevMachineRuntimeCredentialScopeMachine,
		CredentialType:       domain.DevMachineRuntimeCredentialTypeGitHubToken,
		FingerprintSHA256:    hex.EncodeToString(fingerprint[:]),
		EncryptedValue:       encrypted,
		EncryptionKeyVersion: 1,
		ExpiresAt:            expiresAt.UTC(),
	})
}

func checkoutRepositoryFullName(machine *domain.DevMachine, checkout *domain.DevMachineCheckout) string {
	if checkout != nil {
		return checkout.RepositoryFullName
	}
	if machine == nil || machine.RepoOwner == "" || machine.RepoName == "" {
		return ""
	}
	return machine.RepoOwner + "/" + machine.RepoName
}

func splitRepositoryFullName(fullName string) (string, string, bool) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (m *Manager) persistGitRef(ctx context.Context, machine *domain.DevMachine, checkout *domain.DevMachineCheckout, run *domain.DevMachineAgentRun, refType, refName, commit string, pullRequestNumber *int, refURL string) error {
	repositoryFullName := checkoutRepositoryFullName(machine, checkout)
	ref := &domain.DevMachineGitRef{
		WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, IssueID: machine.IssueID,
		RefType: refType, RepositoryFullName: repositoryFullName,
		PullRequestNumber: pullRequestNumber,
	}
	if checkout != nil {
		ref.IssueID = &checkout.IssueID
	}
	if refName != "" {
		ref.RefName = &refName
	}
	if commit != "" {
		ref.CommitSHA = &commit
	}
	if refURL != "" {
		ref.URL = &refURL
	}
	return m.store.CreateGitRef(ctx, ref)
}

var _ ManagerStore = (*repository.DevMachineRepository)(nil)

func (m *Manager) cancelUnauthorizedRun(ctx context.Context, run *domain.DevMachineAgentRun) error {
	if err := m.runtime.CancelAgent(ctx, run); err != nil {
		return err
	}
	m.cancelStaleAgentRun(ctx, run.ID)
	return nil
}

// codexThreadID returns the native session a task attempt's Codex harness
// reported in its own event stream, if valid. Only task attempts are resumed.
func codexThreadID(run *domain.DevMachineAgentRun, event agent.Event) string {
	if run.TaskID == nil || run.ProviderID != "codex" || event.Type != "thread.started" {
		return ""
	}
	var started struct {
		ThreadID string `json:"thread_id"`
	}
	if json.Unmarshal(event.Payload, &started) != nil || !agent.ValidSessionID(started.ThreadID) {
		return ""
	}
	return started.ThreadID
}

// purgeTaskSession removes a finished task's harness transcripts from the
// machine's workspace volume. It works while the machine is paused or stopped;
// a destroyed volume has nothing left to remove.
func (m *Manager) purgeTaskSession(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	if op.CheckoutID == nil || machine.WorkspaceVolumeName == nil || machine.Status == domain.DevMachineStatusDestroyed {
		return nil
	}
	checkout, err := m.store.GetCheckoutInternal(ctx, *op.CheckoutID)
	if err != nil {
		return err
	}
	if checkout == nil || checkout.MachineID != machine.ID {
		return nil
	}
	taskID, err := uuid.Parse(checkout.ScopeKey)
	if err != nil {
		return &terminalOperationError{code: "session_purge_invalid", message: "Checkout is not a task checkout"}
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	image := ""
	for _, service := range services {
		if service.ServiceType == "ide" || service.ServiceType == "terminal" {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return &terminalOperationError{code: "session_purge_unavailable", message: "Developer environment image is unknown"}
	}
	return m.runtime.PurgeTaskSession(ctx, machine, image, taskID)
}
