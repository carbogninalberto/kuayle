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
	LeaseOperations(context.Context, string, int, time.Duration) ([]domain.DevMachineOperation, error)
	RenewOperationLease(context.Context, uuid.UUID, string, time.Duration) error
	CompleteOperation(context.Context, uuid.UUID, string) error
	FailOperation(context.Context, uuid.UUID, string, string, string, bool) (bool, error)
	GetMachineInternal(context.Context, uuid.UUID) (*domain.DevMachine, error)
	GetProvider(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineAgentProvider, error)
	GetAgentRunInternal(context.Context, uuid.UUID) (*domain.DevMachineAgentRun, error)
	ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error)
	ListEnvVarsInternal(context.Context, uuid.UUID, *string, string) ([]domain.DevMachineEnvVar, error)
	SetMachineState(context.Context, uuid.UUID, domain.DevMachineStatus, *string, *string, *string, *string) error
	SetMachineStateForOperation(context.Context, uuid.UUID, int64, bool, domain.DevMachineStatus, *string, *string, *string, *string) (bool, error)
	UpdateServiceRuntime(context.Context, uuid.UUID, string, string, string, *string) error
	CreateRuntimeService(context.Context, *domain.DevMachineService) error
	UpdateAgentRunStarted(context.Context, uuid.UUID) error
	CompleteAgentRun(context.Context, *domain.DevMachineAgentRun) error
	CreateEvent(context.Context, *domain.DevMachineEvent) error
	CreateLogChunk(context.Context, *domain.DevMachineLogChunk) error
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
	GetEnvironment(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineEnvironment, error)
	UpdateEnvironmentState(context.Context, uuid.UUID, string, string, *string) error
	ReconcileOrphanedEnvironments(context.Context, int) (int, error)
	ReconcileOrphanedCheckouts(context.Context, int) (int, error)
	ListDeleteRequestedEnvironments(context.Context, int) ([]domain.DevMachineEnvironment, error)
	DeleteEnvironment(context.Context, uuid.UUID, uuid.UUID) error
	ListIdleMachines(context.Context, int) ([]domain.DevMachine, error)
	UpsertRuntimeCredential(context.Context, *domain.DevMachineRuntimeCredential) error
	PurgeExpiredRuntimeCredentials(context.Context, time.Time) (int, error)
}

type githubAPI interface {
	GetRepositoryInstallationToken(int64, string) (string, time.Time, error)
	CreatePullRequest(string, string, string, string, string, string, string) (*githubclient.PullRequest, error)
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

	operationsCompleted atomic.Uint64
	operationsFailed    atomic.Uint64
	agentRunsCompleted  atomic.Uint64
	lastReconcileUnix   atomic.Int64
	operationSlots      chan struct{}
	operations          sync.WaitGroup
	activeRunsMu        sync.Mutex
	activeRuns          map[uuid.UUID]map[uuid.UUID]context.CancelFunc
}

type terminalOperationError struct {
	code    string
	message string
}

func (e *terminalOperationError) Error() string {
	return e.code + ": " + e.message
}

func NewManager(store ManagerStore, runtime Runtime, agents *agent.Registry, github githubAPI, encryptionKey, githubKey []byte, owner string) *Manager {
	return &Manager{
		store: store, runtime: runtime, agents: agents, github: github, encryptionKey: encryptionKey,
		githubKey: githubKey, owner: owner, pollInterval: time.Second, operationSlots: make(chan struct{}, 8),
		activeRuns: make(map[uuid.UUID]map[uuid.UUID]context.CancelFunc),
	}
}

func (m *Manager) Run(ctx context.Context) error {
	operationTicker := time.NewTicker(m.pollInterval)
	reconcileTicker := time.NewTicker(30 * time.Second)
	defer operationTicker.Stop()
	defer reconcileTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.operations.Wait()
			return nil
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
	if operation.Action == domain.DevMachineOpRunAgent && operation.AgentRunID != nil {
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
		case domain.DevMachineOpTeardown, domain.DevMachineOpCancelAgent:
			// Destructive cleanup must still converge after a newer generation.
		case domain.DevMachineOpCheckoutIssue:
			if operation.CheckoutID != nil {
				message := "machine state changed before checkout preparation; try again"
				return m.store.UpdateCheckoutState(ctx, *operation.CheckoutID, "failed", &message)
			}
			return nil
		case domain.DevMachineOpRunAgent:
			if operation.AgentRunID != nil {
				m.cancelStaleAgentRun(ctx, *operation.AgentRunID)
			}
			return nil
		default:
			return nil
		}
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
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
		return m.setStateAndEventForOperation(ctx, machine, operation, domain.DevMachineStatusDestroyed, "machine.destroyed", nil, true)
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
	for _, service := range services {
		if service.ServiceType == "agent" {
			continue
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
	_, hasIDE := serviceSecrets["ide"]
	if hasIDE {
		_, githubToken, err := m.repositoryToken(ctx, machine, "")
		if err == nil && githubToken != "" {
			serviceSecrets["ide"]["GITHUB_TOKEN"] = githubToken
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("github_token_failed: %w", err)
		}
	}
	return serviceSecrets, nil
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
	if err := m.store.UpdateAgentRunStarted(ctx, run.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			current, currentErr := m.store.GetAgentRunInternal(ctx, run.ID)
			if currentErr == nil && current != nil && terminalAgentStatus(current.Status) {
				return nil
			}
		}
		return err
	}
	run.Status = domain.DevMachineAgentRunStatusRunning
	envVars, err := m.store.ListEnvVarsInternal(ctx, machine.ID, &run.ProviderID, "agent")
	if err != nil {
		return err
	}
	secrets := make(map[string]string, len(envVars))
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
			return fmt.Errorf("secret_decryption_failed: %w", err)
		}
		secrets[envVar.Name] = value
	}
	githubToken := ""
	if run.PushBranch || run.OpenPullRequest {
		_, githubToken, err = m.repositoryToken(ctx, machine, checkoutRepositoryFullName(machine, checkout))
		if err != nil || githubToken == "" {
			if err == nil {
				err = errors.New("no linked GitHub App installation")
			}
			return fmt.Errorf("github_token_failed: repository-scoped GitHub App token is unavailable: %w", err)
		}
		secrets["GITHUB_TOKEN"] = githubToken
	}
	service := &domain.DevMachineService{
		ID: uuid.New(), MachineID: machine.ID, AgentRunID: &run.ID, ServiceType: "agent",
		ServiceKey: "agent-" + run.ID.String(), ContainerName: "kuayle-" + machine.RoutingKey + "-agent-" + run.ID.String(),
		ImageRef: providerRecord.ImageRef, InternalHost: "agent-" + run.ID.String(), InternalPort: 8080,
		Status: "pending", HealthStatus: "unknown",
	}
	if err := m.store.CreateRuntimeService(ctx, service); err != nil {
		return err
	}
	runContext, cancel := context.WithTimeout(ctx, time.Duration(run.MaxRuntimeSeconds)*time.Second)
	defer cancel()
	execution, executionErr := m.runtime.RunAgent(runContext, machine, run, providerRecord, checkout, secrets)
	timedOut := errors.Is(executionErr, context.DeadlineExceeded) || errors.Is(runContext.Err(), context.DeadlineExceeded)
	if timedOut {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr := m.runtime.CancelAgent(cleanupCtx, run)
		cleanupCancel()
		if cleanupErr != nil {
			return fmt.Errorf("agent_timeout_cleanup_failed: %w", cleanupErr)
		}
	}
	if execution != nil {
		_ = m.store.UpdateServiceRuntime(ctx, service.ID, execution.ContainerID, "running", "starting", nil)
	}
	if run.Mode == "interactive" && executionErr == nil {
		return m.createEvent(ctx, machine, &run.ID, "agent", "agent_run.interactive_started", map[string]any{"provider_id": run.ProviderID})
	}
	if execution == nil {
		execution = &AgentExecution{ExitCode: -1}
	}
	stdout, stderr := redact(execution.Stdout, secrets), redact(execution.Stderr, secrets)
	if stdout != "" {
		_ = m.store.CreateLogChunk(ctx, &domain.DevMachineLogChunk{
			WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, ServiceID: &service.ID,
			Stream: "stdout", Sequence: 1, Content: stdout,
		})
	}
	if stderr != "" {
		_ = m.store.CreateLogChunk(ctx, &domain.DevMachineLogChunk{
			WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID, ServiceID: &service.ID,
			Stream: "stderr", Sequence: 1, Content: stderr,
		})
	}
	result := provider.ParseResult(stdout, stderr, execution.ExitCode)
	for _, providerEvent := range provider.ParseEvents([]byte(stdout)) {
		payload := providerEvent.Payload
		if len(payload) == 0 {
			payload, _ = json.Marshal(map[string]any{"message": providerEvent.Message})
		}
		_ = m.store.CreateEvent(ctx, &domain.DevMachineEvent{
			WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: &run.ID,
			Source: "agent", EventType: providerEvent.Type, Payload: payload, OccurredAt: time.Now().UTC(),
		})
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
	services, _ := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if gitState, gitErr := m.runtime.GitState(ctx, machine, services, checkout); gitErr == nil {
		result.Branch = gitState.Branch
		result.Commits = gitState.Commits
		result.ChangedFiles = gitState.ChangedFiles
	}
	if run.Status == domain.DevMachineAgentRunStatusSucceeded && run.OpenPullRequest && result.Branch != "" {
		githubAPI, freshToken, prErr := m.repositoryToken(ctx, machine, checkoutRepositoryFullName(machine, checkout))
		var pullRequest *githubclient.PullRequest
		if prErr == nil {
			repoOwner, repoName, baseBranch, workingBranch := machine.RepoOwner, machine.RepoName, machine.BaseBranch, machine.WorkingBranch
			if checkout != nil {
				parts := strings.SplitN(checkout.RepositoryFullName, "/", 2)
				if len(parts) == 2 {
					repoOwner, repoName = parts[0], parts[1]
				}
				baseBranch, workingBranch = checkout.BaseBranch, checkout.WorkingBranch
			}
			pullRequest, prErr = githubAPI.CreatePullRequest(freshToken, repoOwner, repoName,
				fmt.Sprintf("Kuayle agent run for %s", workingBranch), result.Branch, baseBranch, result.Summary)
		}
		if prErr != nil {
			result.RiskNotes = append(result.RiskNotes, "Pull request creation failed: "+safeError(prErr))
		} else {
			result.PullRequestURL = pullRequest.HTMLURL
			run.PullRequestURL = &pullRequest.HTMLURL
			_ = m.persistGitRef(ctx, machine, checkout, run, "pull_request", result.Branch, "", &pullRequest.Number, pullRequest.HTMLURL)
		}
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
	_ = m.store.UpdateServiceRuntime(ctx, service.ID, execution.ContainerID, "stopped", "stopped", nil)
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
				MachineID: machine.ID, CPUPercent: usage.CPUPercent, MemoryBytes: usage.MemoryBytes,
				DiskBytes: usage.DiskBytes, Pids: usage.Pids, NetworkRxBytes: usage.NetworkRxBytes,
				NetworkTxBytes: usage.NetworkTxBytes,
			})
			_ = m.store.UpdateVolumeUsage(ctx, machine.ID, usage.DiskBytes)
			limit := int64(machine.DiskGB) * 1024 * 1024 * 1024
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
