package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func TestAgentTaskCheckoutsAreIsolatedAndRecoverable(t *testing.T) {
	ws, user, machineID, issueID, repoID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	manual := domain.DevMachineCheckout{ID: uuid.New(), WorkspaceID: ws, MachineID: machineID, IssueID: issueID, GitHubRepoID: repoID, Status: "ready", WorkingBranch: "kuayle/eng-7", WorkspacePath: "/workspace/tasks/eng-7"}
	store := &devMachineStoreFake{
		policy: testPolicy(ws), machine: &domain.DevMachine{ID: machineID, WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		issues:        map[uuid.UUID]*domain.Issue{issueID: {ID: issueID, WorkspaceID: ws, TeamID: uuid.New(), Identifier: "ENG-7"}},
		reposByID:     map[uuid.UUID]*domain.GitHubRepoModel{repoID: {ID: repoID, WorkspaceID: ws, FullName: "kuayle/api", DefaultBranch: "main", IsActive: true}},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{scopeKey(nil, nil, &issueID): {WorkspaceID: ws, GitHubRepoID: &repoID}},
		checkouts:     []domain.DevMachineCheckout{manual},
	}
	svc := newTestDevMachineService(store)
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, IssueID: issueID, MachineID: &machineID, CheckoutID: uuid.New()}
	first, err := svc.checkoutTask(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, task.CheckoutID, first.ID)
	require.NotEqual(t, manual.WorkspacePath, first.WorkspacePath)
	require.NotEqual(t, manual.WorkingBranch, first.WorkingBranch)
	recovered, err := svc.checkoutTask(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, first.ID, recovered.ID)
	require.Len(t, store.checkouts, 2)
	second := task
	second.ID = uuid.New()
	second.CheckoutID = uuid.New()
	other, err := svc.checkoutTask(context.Background(), second)
	require.NoError(t, err)
	require.NotEqual(t, first.WorkspacePath, other.WorkspacePath)
	require.NotEqual(t, first.WorkingBranch, other.WorkingBranch)
	reopened, err := svc.CheckoutIssue(context.Background(), ws, machineID, user, dto.CheckoutIssueRequest{IssueID: issueID.String()})
	require.NoError(t, err)
	require.Equal(t, manual.ID, reopened.ID)
	// Task recovery never silently retries an exhausted checkout operation.
	store.checkouts[1].Status = "failed"
	store.createCheckoutCalled = false
	failed, err := svc.checkoutTask(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.False(t, store.createCheckoutCalled)
	task.Generation = 2
	retried, err := svc.checkoutTask(context.Background(), task)
	require.NoError(t, err)
	require.True(t, store.createCheckoutCalled)
	require.Equal(t, first.WorkspacePath, retried.WorkspacePath)
	require.Equal(t, first.WorkingBranch, retried.WorkingBranch)
	// Exhausting this explicitly requested generation does not create a retry loop.
	store.checkouts[1].Status = "failed"
	store.createCheckoutCalled = false
	store.queuedOperation = store.checkoutOperation
	store.queuedOperation.Status = domain.DevMachineOpStatusFailed
	exhausted, err := svc.checkoutTask(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, "failed", exhausted.Status)
	require.False(t, store.createCheckoutCalled)
}

type taskStoreFake struct {
	startedIssue *domain.Issue
	startErr     error
	results      []bool
	releases     int
	taskLifecycleStore
	task        domain.AgentTask
	accessErr   error
	transitions []string
	revoked     bool
	finished    bool
}

func (f *taskStoreFake) Get(context.Context, uuid.UUID, uuid.UUID) (*domain.AgentTask, error) {
	return &f.task, f.accessErr
}
func (f *taskStoreFake) Transition(_ context.Context, _ domain.AgentTask, to, _ string) error {
	if !domain.ValidAgentTaskTransition(f.task.State, to) {
		return repository.ErrAgentTaskConflict
	}
	f.task.State = to
	f.transitions = append(f.transitions, to)
	return nil
}
func (f *taskStoreFake) Start(ctx context.Context, task domain.AgentTask) (*domain.Issue, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	return f.startedIssue, f.Transition(ctx, task, "running", "Native coding harness started")
}
func (f *taskStoreFake) Revoke(context.Context, domain.AgentTask) error { f.revoked = true; return nil }
func (f *taskStoreFake) FinishCancellation(context.Context, domain.AgentTask, string) error {
	f.finished = true
	return nil
}

type taskRuntimeFake struct {
	ready                       bool
	prepareErr                  error
	run                         *domain.DevMachineAgentRun
	prepares, attempts, cancels int
	cancelErr                   error
}

func (f *taskRuntimeFake) Prepare(context.Context, *domain.AgentTask) (bool, error) {
	f.prepares++
	return f.ready, f.prepareErr
}
func (f *taskRuntimeFake) Attempt(context.Context, domain.AgentTask) (*domain.DevMachineAgentRun, error) {
	f.attempts++
	return f.run, nil
}
func (f *taskRuntimeFake) Cancel(context.Context, domain.AgentTask) error {
	f.cancels++
	return f.cancelErr
}

func TestAgentTaskWorkerUsesNativeExecutionState(t *testing.T) {
	store := &taskStoreFake{task: domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New(), State: "queued"}}
	runtime := &taskRuntimeFake{ready: false}
	svc := &AgentTaskService{store: store, runtime: runtime}
	ctx := context.Background()
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, "preparing", store.task.State)
	require.Zero(t, runtime.prepares)
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, "preparing", store.task.State)
	require.Zero(t, runtime.attempts)
	runtime.ready = true
	runtime.run = &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusQueued}
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, "preparing", store.task.State)
	runtime.run.Status = domain.DevMachineAgentRunStatusRunning
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, "running", store.task.State)
	runtime.run.Status = domain.DevMachineAgentRunStatusWaitingInput
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, "waiting_input", store.task.State)
	runtime.run.Status = domain.DevMachineAgentRunStatusSucceeded
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, "validating", store.task.State)
	require.Equal(t, []string{"preparing", "running", "waiting_input", "running", "validating"}, store.transitions)
	require.NotContains(t, store.transitions, "completed", "native process success never accepts the task")
	// Missing/unknown execution state must not manufacture a running milestone.
	store.task.State = "preparing"
	store.transitions = nil
	runtime.run.Status = ""
	require.Error(t, svc.step(ctx, store.task))
	require.Empty(t, store.transitions)
}

func TestAgentTaskWorkerRevocationCancellationAndQuota(t *testing.T) {
	for _, scenario := range []string{"revoked", "quota", "cancelled", "cancel pending"} {
		t.Run(scenario, func(t *testing.T) {
			store := &taskStoreFake{task: domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New(), State: "preparing"}}
			runtime := &taskRuntimeFake{ready: true}
			svc := &AgentTaskService{store: store, runtime: runtime}
			switch scenario {
			case "revoked":
				store.accessErr = repository.ErrConversationNotFound
			case "quota":
				runtime.prepareErr = ErrMachineQuota
			case "cancelled", "cancel pending":
				store.task.State = "cancelling"
				store.accessErr = repository.ErrConversationNotFound
			}
			if scenario == "cancel pending" {
				runtime.cancelErr = errors.New("runtime still stopping")
			}
			err := svc.step(context.Background(), store.task)
			if scenario == "cancel pending" {
				require.Error(t, err)
				require.False(t, store.finished)
			} else {
				require.NoError(t, err)
			}
			if scenario == "revoked" {
				require.True(t, store.revoked)
				require.Zero(t, runtime.prepares)
			}
			if scenario == "quota" {
				require.Equal(t, "preparing", store.task.State)
				require.Zero(t, runtime.attempts)
			}
			if scenario == "cancelled" {
				require.True(t, store.finished)
				require.Equal(t, 1, runtime.cancels)
			}
		})
	}
}

func (f *taskStoreFake) Fail(_ context.Context, _ domain.AgentTask, _ string) error {
	f.task.State = "cancelling"
	f.task.FailurePending = true
	return nil
}
func TestAgentTaskFailureWaitsForPhysicalCleanup(t *testing.T) {
	store := &taskStoreFake{task: domain.AgentTask{State: "running"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, "cancelling", store.task.State)
	require.True(t, store.task.FailurePending)
	require.False(t, store.finished)
	runtime.cancelErr = errors.New("cleanup pending")
	require.Error(t, svc.step(context.Background(), store.task))
	require.False(t, store.finished)
	runtime.cancelErr = nil
	require.NoError(t, svc.step(context.Background(), store.task))
	require.True(t, store.finished)
}

func (f *taskStoreFake) RecordStepResult(_ context.Context, _ domain.AgentTask, failed bool) error {
	f.results = append(f.results, failed)
	return nil
}
func (f *taskStoreFake) Release(context.Context, domain.AgentTask) error { f.releases++; return nil }
func TestAgentTaskWorkerRecordsFailuresButNotCapacityOrLeaseWaits(t *testing.T) {
	for _, scenario := range []string{"transient", "quota", "stale", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			store := &taskStoreFake{task: domain.AgentTask{State: "preparing"}}
			runtime := &taskRuntimeFake{}
			switch scenario {
			case "transient":
				runtime.prepareErr = errors.New("temporary unavailable")
			case "quota":
				runtime.prepareErr = ErrMachineQuota
			case "stale":
				runtime.prepareErr = repository.ErrAgentTaskConflict
			case "deadline":
				runtime.prepareErr = context.DeadlineExceeded
			}
			svc := &AgentTaskService{store: store, runtime: runtime}
			svc.process(context.Background(), store.task)
			require.Equal(t, 1, store.releases)
			if scenario == "stale" {
				require.Empty(t, store.results)
			} else {
				require.Equal(t, []bool{scenario != "quota"}, store.results)
			}
		})
	}
}

type taskPublisherFake struct{ events []realtime.Event }

func (f *taskPublisherFake) Broadcast(_ uuid.UUID, event realtime.Event) {
	f.events = append(f.events, event)
}

func TestAgentTaskWorkerPublishesOnlyCommittedIssueStarts(t *testing.T) {
	for _, scenario := range []string{"changed", "unchanged", "error", "fast_failure", "fast_timeout"} {
		t.Run(scenario, func(t *testing.T) {
			store := &taskStoreFake{task: domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New(), State: "preparing"}}
			store.startedIssue = &domain.Issue{ID: uuid.New(), WorkspaceID: store.task.WorkspaceID, Status: domain.IssueStatusInProgress}
			runtime := &taskRuntimeFake{ready: true, run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusRunning}}
			if scenario == "unchanged" {
				store.startedIssue = nil
			}
			if scenario == "error" {
				store.startErr = errors.New("transaction rolled back")
			}
			if scenario == "fast_failure" || scenario == "fast_timeout" {
				now := time.Now()
				runtime.run.StartedAt = &now
				runtime.run.Status = domain.DevMachineAgentRunStatusFailed
				if scenario == "fast_timeout" {
					runtime.run.Status = domain.DevMachineAgentRunStatusTimeout
				}
			}
			publisher := &taskPublisherFake{}
			svc := &AgentTaskService{store: store, runtime: runtime, events: publisher}
			err := svc.step(context.Background(), store.task)
			if scenario == "error" {
				require.ErrorIs(t, err, store.startErr)
				require.Empty(t, publisher.events)
				require.Equal(t, "preparing", store.task.State)
				return
			}
			require.NoError(t, err)
			if scenario == "unchanged" {
				require.Empty(t, publisher.events)
			} else {
				require.Len(t, publisher.events, 1)
				require.Equal(t, "issue.updated", publisher.events[0].Type)
				require.Equal(t, store.startedIssue, publisher.events[0].Payload)
			}
			if scenario == "fast_failure" || scenario == "fast_timeout" {
				require.Equal(t, "cancelling", store.task.State)
				require.Contains(t, store.transitions, "running")
			} else {
				require.Equal(t, "running", store.task.State)
			}
		})
	}
}

type taskPolicyStore struct {
	*devMachineStoreFake
	run *domain.DevMachineAgentRun
}

func (f *taskPolicyStore) GetAgentRun(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineAgentRun, error) {
	return f.run, nil
}

func TestAgentTaskAttemptRechecksDisabledWorkspacePolicy(t *testing.T) {
	ws := uuid.New()
	store := &taskPolicyStore{devMachineStoreFake: &devMachineStoreFake{policy: testPolicy(ws)}, run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusRunning}}
	runtime := &devMachineTaskRuntime{machines: newTestDevMachineService(store)}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, State: "running"}
	run, err := runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, store.run, run)
	store.policy.Enabled = false
	_, err = runtime.Attempt(context.Background(), task)
	require.ErrorIs(t, err, ErrDevMachinesDisabled)
}

func TestAgentTaskPreparationPolicyFailureCleansAllocatedRuntime(t *testing.T) {
	for _, allocated := range []bool{false, true} {
		t.Run(fmt.Sprint(allocated), func(t *testing.T) {
			store := &taskStoreFake{task: domain.AgentTask{State: "preparing"}}
			if allocated {
				id := uuid.New()
				store.task.MachineID = &id
			}
			runtime := &taskRuntimeFake{prepareErr: ErrDevMachinesDisabled}
			svc := &AgentTaskService{store: store, runtime: runtime}
			require.NoError(t, svc.step(context.Background(), store.task))
			if !allocated {
				require.Equal(t, "blocked", store.task.State)
				return
			}
			require.Equal(t, "cancelling", store.task.State, "a native attempt may already have started before the task worker observes it")
			require.True(t, store.task.FailurePending)
			require.False(t, store.finished)
			require.NoError(t, svc.step(context.Background(), store.task))
			require.Equal(t, 1, runtime.cancels)
			require.True(t, store.finished)
		})
	}
}
