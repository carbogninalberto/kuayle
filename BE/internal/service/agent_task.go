package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

type taskLifecycleStore interface {
	Start(context.Context, domain.AgentTask) (*domain.Issue, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (*domain.AgentTask, error)
	Claim(context.Context) (*domain.AgentTask, error)
	Transition(context.Context, domain.AgentTask, string, string) error
	Revoke(context.Context, domain.AgentTask) error
	FinishCancellation(context.Context, domain.AgentTask, string) error
	Release(context.Context, domain.AgentTask) error
	RecordStepResult(context.Context, domain.AgentTask, bool) error
	Fail(context.Context, domain.AgentTask, string) error
}
type taskExecutionRuntime interface {
	Prepare(context.Context, *domain.AgentTask) (bool, error)
	Attempt(context.Context, domain.AgentTask) (*domain.DevMachineAgentRun, error)
	Cancel(context.Context, domain.AgentTask) error
}
type taskEventPublisher interface {
	Broadcast(uuid.UUID, realtime.Event)
}
type AgentTaskService struct {
	Repo     *repository.AgentTaskRepository
	machines *DevMachineService
	events   taskEventPublisher
	store    taskLifecycleStore
	runtime  taskExecutionRuntime
}

func NewAgentTaskService(repo *repository.AgentTaskRepository, machines *DevMachineService, hub *realtime.Hub) *AgentTaskService {
	service := &AgentTaskService{Repo: repo, machines: machines, store: repo, runtime: &devMachineTaskRuntime{tasks: repo, machines: machines}}
	if hub != nil {
		service.events = hub
	}
	return service
}

// Task workers queue existing durable infrastructure operations; they never
// perform Docker or model calls themselves. Reconnects do not invoke this loop.
func (s *AgentTaskService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		task, err := s.store.Claim(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Error("Could not claim an agent task")
			}
			continue
		}
		if task == nil {
			continue
		}
		s.process(ctx, *task)
	}
}
func (s *AgentTaskService) process(ctx context.Context, task domain.AgentTask) {
	stepCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err := s.step(stepCtx, task)
	cancel()
	finalCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finish()
	if ctx.Err() == nil && !errors.Is(err, repository.ErrAgentTaskConflict) {
		if recordErr := s.store.RecordStepResult(finalCtx, task, err != nil); recordErr != nil && !errors.Is(recordErr, repository.ErrAgentTaskConflict) {
			log.WithField("task_id", task.ID).Error("Could not record agent task recovery state")
		}
	}
	if releaseErr := s.store.Release(finalCtx, task); releaseErr != nil && !errors.Is(releaseErr, repository.ErrAgentTaskConflict) {
		log.WithField("task_id", task.ID).Error("Could not release agent task lease")
	}
}

func (s *AgentTaskService) step(ctx context.Context, task domain.AgentTask) error {
	if task.State == "cancelling" {
		if err := s.runtime.Cancel(ctx, task); err != nil {
			return err
		}
		return s.store.FinishCancellation(ctx, task, "Runtime cancellation confirmed")
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: task.CreatorID})
	if _, err := s.store.Get(ctx, task.WorkspaceID, task.ID); err != nil {
		if errors.Is(err, repository.ErrConversationNotFound) {
			return s.store.Revoke(ctx, task)
		}
		return err
	}
	ctx = domain.WithTaskExecution(ctx, task)
	if task.State == "queued" {
		return s.store.Transition(ctx, task, "preparing", "Resolving repository and execution environment")
	}
	if task.State == "preparing" {
		ready, err := s.runtime.Prepare(ctx, &task)
		if err != nil {
			return s.preparationError(ctx, task, err)
		}
		if !ready {
			return nil
		}
	}
	if task.State != "preparing" && task.State != "running" && task.State != "waiting_input" {
		return nil
	}
	run, err := s.runtime.Attempt(ctx, task)
	if err != nil {
		return s.preparationError(ctx, task, err)
	}
	if run == nil {
		return nil
	}
	// A short-lived process may finish before the worker observes its running
	// state. Persist its real start before failure cleanup, but never invent one.
	if task.State == "preparing" && run.StartedAt != nil && (run.Status == domain.DevMachineAgentRunStatusFailed || run.Status == domain.DevMachineAgentRunStatusTimeout) {
		if err = s.recordStart(ctx, &task); err != nil {
			return err
		}
	}
	switch run.Status {
	case domain.DevMachineAgentRunStatusQueued, domain.DevMachineAgentRunStatusStarting:
		return nil
	case domain.DevMachineAgentRunStatusFailed, domain.DevMachineAgentRunStatusTimeout:
		return s.store.Fail(ctx, task, "Native coding attempt failed; stopping execution before retry")
	case domain.DevMachineAgentRunStatusCancelled:
		return s.store.Transition(ctx, task, "cancelling", "Native coding attempt was cancelled")
	case domain.DevMachineAgentRunStatusRunning, domain.DevMachineAgentRunStatusWaitingInput, domain.DevMachineAgentRunStatusSucceeded:
		// These states are reported by the native execution record.
	default:
		return ErrInvalidOperation
	}
	if task.State == "preparing" {
		if err = s.recordStart(ctx, &task); err != nil {
			return err
		}
	}

	if run.Status == domain.DevMachineAgentRunStatusWaitingInput && task.State == "running" {
		return s.store.Transition(ctx, task, "waiting_input", "Native harness requires input")
	}
	if run.Status == domain.DevMachineAgentRunStatusRunning && task.State == "waiting_input" {
		return s.store.Transition(ctx, task, "running", "Native harness resumed")
	}
	if run.Status == domain.DevMachineAgentRunStatusSucceeded {
		if task.State == "waiting_input" {
			if err = s.store.Transition(ctx, task, "running", "Native harness resumed"); err != nil {
				return err
			}
		}
		// Process success is not evidence that independent validation passed.
		return s.store.Transition(ctx, task, "validating", "Implementation attempt finished; independent validation is pending")
	}
	return nil
}
func (s *AgentTaskService) recordStart(ctx context.Context, task *domain.AgentTask) error {
	issue, err := s.store.Start(ctx, *task)
	if err != nil {
		return err
	}
	if issue != nil && s.events != nil {
		s.events.Broadcast(task.WorkspaceID, realtime.Event{Type: "issue.updated", Payload: issue})
	}
	task.State = "running"
	return nil
}

func (s *AgentTaskService) preparationError(ctx context.Context, task domain.AgentTask, err error) error {
	if errors.Is(err, repository.ErrAgentTaskConflict) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrMachineQuota) || errors.Is(err, repository.ErrMachineQuota) {
		return nil
	}
	if errors.Is(err, repository.ErrConversationNotFound) {
		return s.store.Revoke(ctx, task)
	}
	if errors.Is(err, ErrCheckoutNotEligible) || errors.Is(err, ErrRepositoryNotAllowed) || errors.Is(err, ErrDevMachinesDisabled) || errors.Is(err, ErrProviderNotAllowed) || errors.Is(err, ErrInvalidMachineInput) || errors.Is(err, ErrInvalidOperation) {
		// A preparing task may already have queued or started native execution.
		// Once an environment is reserved, settle its operations before stopping.
		if task.MachineID != nil || task.State == "running" || task.State == "waiting_input" {
			return s.store.Fail(ctx, task, "Execution configuration became unavailable; stopping the native attempt")
		}
		return s.store.Transition(ctx, task, "blocked", "Execution configuration is unavailable; check the issue repository, environment, provider and workspace policy")
	}
	return err
}

type devMachineTaskRuntime struct {
	tasks    *repository.AgentTaskRepository
	machines *DevMachineService
}

func (r *devMachineTaskRuntime) Prepare(ctx context.Context, task *domain.AgentTask) (bool, error) {
	s := r.machines
	rawIssue := task.IssueID.String()
	issue, project, _, _, err := s.resolveRequestedContext(ctx, task.WorkspaceID, &rawIssue, nil)
	if err != nil {
		return false, err
	}
	setting, err := s.resolveDevelopmentSetting(ctx, task.WorkspaceID, issue, project)
	if err != nil {
		return false, err
	}
	if setting.GitHubRepoID == nil {
		return false, ErrCheckoutNotEligible
	}
	if task.MachineID == nil {
		id, err := r.tasks.ReserveMachine(ctx, *task, *setting.GitHubRepoID, setting.EnvironmentID)
		if err != nil {
			return false, err
		}
		task.MachineID = &id
	}
	machine, err := s.store.GetMachineForUser(ctx, task.WorkspaceID, *task.MachineID, task.CreatorID)
	if err != nil {
		return false, err
	}
	if machine == nil {
		if *task.MachineID != task.ReservedMachineID {
			return false, ErrInvalidOperation
		}
		profile := domain.DevMachineProfileHeadless
		if setting.EnvironmentID != nil {
			profile = domain.DevMachineProfileStandard
		}
		machine, _, err = s.create(ctx, task.WorkspaceID, task.CreatorID, dto.CreateDevMachineRequest{Name: "task-" + task.ID.String(), IssueID: &rawIssue, Size: "small", Profile: profile, Agents: []dto.DevMachineAgentInput{{Provider: task.Provider, Mode: "autonomous"}}}, task.ReservedMachineID)
		if err != nil {
			return false, err
		}
	}
	switch machine.Status {
	case domain.DevMachineStatusQueued, domain.DevMachineStatusSpawning, domain.DevMachineStatusConfiguring:
		return false, nil
	case domain.DevMachineStatusRunning:
	case domain.DevMachineStatusPaused, domain.DevMachineStatusStopped, domain.DevMachineStatusFailed:
		if machine.ID != task.ReservedMachineID {
			return false, ErrInvalidOperation
		}
		op, err := s.Lifecycle(ctx, task.WorkspaceID, machine.ID, task.CreatorID, domain.DevMachineOpStart, fmt.Sprintf("task-start:%s:%d", task.ID, task.Generation))
		if err != nil {
			return false, err
		}
		if op != nil && op.Status == domain.DevMachineOpStatusFailed {
			return false, ErrInvalidOperation
		}
		return false, nil
	default:
		return false, ErrInvalidOperation
	}
	checkout, err := s.checkoutTask(ctx, *task)
	if err != nil {
		return false, err
	}
	if checkout.Status == "failed" {
		return false, fmt.Errorf("%w: worktree preparation failed", ErrInvalidOperation)
	}
	return checkout.Status == "ready", nil
}
func (r *devMachineTaskRuntime) Attempt(ctx context.Context, task domain.AgentTask) (*domain.DevMachineAgentRun, error) {
	s := r.machines
	if _, err := s.enabledPolicy(ctx, task.WorkspaceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetAgentRun(ctx, task.WorkspaceID, task.RunID)
	if err != nil || run != nil {
		return run, err
	}
	if task.State != "preparing" || task.MachineID == nil {
		return nil, ErrInvalidOperation
	}
	machine, err := s.GetForUser(ctx, task.WorkspaceID, *task.MachineID, task.CreatorID)
	if err != nil {
		return nil, err
	}
	timeout := min(3600, int(time.Until(machine.ExpiresAt).Seconds())-5, machine.MaxRuntimeMinutes*60)
	if timeout < 30 {
		return nil, ErrInvalidOperation
	}
	var command []string
	if len(task.ValidationCommand) > 0 && json.Unmarshal(task.ValidationCommand, &command) != nil {
		return nil, ErrInvalidMachineInput
	}
	if domain.ValidateTestCommand(command) != nil {
		return nil, ErrInvalidMachineInput
	}
	push := false
	checkout := task.CheckoutID.String()
	return s.createAgentRun(ctx, task.WorkspaceID, *task.MachineID, task.CreatorID, dto.CreateAgentRunRequest{Provider: task.Provider, Mode: "autonomous", Prompt: task.Prompt, TestCommand: command, CheckoutID: &checkout, Model: task.Model, Thinking: task.Thinking, PushBranch: &push, MaxRuntimeSeconds: timeout}, task.RunID)
}
func (r *devMachineTaskRuntime) Cancel(ctx context.Context, task domain.AgentTask) error {
	return r.tasks.CancelAttempt(ctx, task)
}
