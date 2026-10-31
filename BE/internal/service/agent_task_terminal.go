package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	// ErrTaskTerminalBusy means the task's agent, check or publication may be
	// using its session and worktree, so the user cannot open it now.
	ErrTaskTerminalBusy = errors.New("the task's agent is working; open it in Claude Code when it stops")
	// ErrTaskTerminalUnsupported means the task's harness is not Claude Code.
	ErrTaskTerminalUnsupported = errors.New("only Claude Code tasks open in Claude Code")
)

// taskTerminalStates are the task states in which nothing else uses the
// task's Claude Code session or worktree: the harness has exited and the
// task waits for the user (awaiting_review, blocked) or stopped (failed, whose
// transcripts are kept for a retry). A completed or cancelled task's
// transcripts are purged, and any other state has an attempt, check or
// publication in progress.
var taskTerminalStates = map[string]bool{"awaiting_review": true, "blocked": true, "failed": true}

// taskTerminalSessionPrefix names a task's Claude Code terminal session:
// ktask-<8 hex>-<task>[-<native session>]. kuayle-terminal-session runs
// kuayle-task-claude for it instead of a shell.
const taskTerminalSessionPrefix = "ktask-"

// OpenInClaudeCode opens a terminal session on the task's machine, in its
// worktree, running Claude Code on the task's own session: `claude --resume`
// with the task's native session when its transcript is on that machine, else
// a new session. Only the task's creator, who owns its machine, may open it,
// and only while no attempt, check or publication runs.
func (s *AgentTaskService) OpenInClaudeCode(ctx context.Context, ws, id uuid.UUID) (*dto.TerminalSessionLaunchResponse, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || s.machines == nil {
		return nil, repository.ErrAgentTaskUnauthorized
	}
	task, err := s.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	sessions, err := s.Repo.TaskSessions(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	native, err := taskTerminalSession(task, access.UserID, sessions)
	if err != nil {
		return nil, err
	}
	return s.machines.OpenTaskTerminal(ctx, ws, *task.MachineID, access.UserID, task.CheckoutID, task.Provider, task.ID, native)
}

// taskTerminalSession checks that the user may open the task in Claude Code
// now and returns the native session to resume: the task's latest one, when
// it was recorded on the task's current machine, where its transcript lives.
func taskTerminalSession(task *domain.AgentTask, userID uuid.UUID, sessions *domain.AgentTaskSessions) (string, error) {
	if task.CreatorID != userID {
		return "", repository.ErrAgentTaskUnauthorized
	}
	if task.Provider != "claude-code" {
		return "", ErrTaskTerminalUnsupported
	}
	if !taskTerminalStates[task.State] || task.MachineID == nil {
		return "", ErrTaskTerminalBusy
	}
	if sessions == nil {
		return "", nil
	}
	for _, session := range sessions.Sessions {
		if session.TaskID != task.ID || session.NativeSessionID == nil || session.MachineID == nil || *session.MachineID != *task.MachineID {
			continue
		}
		if _, err := uuid.Parse(*session.NativeSessionID); err == nil && strings.ToLower(*session.NativeSessionID) == *session.NativeSessionID && len(*session.NativeSessionID) == 36 {
			return *session.NativeSessionID, nil
		}
	}
	return "", nil
}

// OpenTaskTerminal opens, or reopens, the owner's Claude Code terminal session
// for a task on their machine, in the task's checkout. The workspace policy
// must still allow the harness. An open session for the same task and native
// session gets a new ticket, so reopening attaches to the same Claude Code
// instead of starting a second one on the same transcript.
func (s *DevMachineService) OpenTaskTerminal(ctx context.Context, workspaceID, machineID, userID, checkoutID uuid.UUID, provider string, taskID uuid.UUID, native string) (*dto.TerminalSessionLaunchResponse, error) {
	policy, err := s.enabledPolicy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !providerAllowed(policy.AllowedProviders, provider) {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotAllowed, provider)
	}
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if s.domain == "" || s.frontendOrigin == "" {
		return nil, fmt.Errorf("dev machine terminal is not configured")
	}
	if pending, handled, err := s.pendingLaunch(ctx, workspaceID, machine, userID); handled || err != nil {
		if err != nil {
			return nil, err
		}
		return &dto.TerminalSessionLaunchResponse{Status: pending.Status, Operation: pending.Operation, RetryAfterSeconds: pending.RetryAfterSeconds}, nil
	}
	checkout, err := s.store.GetTerminalCheckout(ctx, workspaceID, machineID, checkoutID)
	if err != nil {
		return nil, err
	}
	if checkout == nil {
		return nil, ErrCheckoutNotFound
	}
	if checkout.Status != "ready" {
		return nil, fmt.Errorf("%w: the task's checkout must be ready", ErrCheckoutNotReady)
	}
	terminalService, err := s.store.GetService(ctx, workspaceID, machineID, "terminal")
	if err != nil {
		return nil, err
	}
	if terminalService == nil || terminalService.MachineID != machineID || terminalService.ServiceType != "terminal" || terminalService.Status != "running" {
		return nil, ErrServiceNotAvailable
	}
	suffix := "-" + taskID.String()
	if native != "" {
		suffix += "-" + native
	}
	existing, err := s.store.ListTerminalSessions(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	for index := range existing {
		session := &existing[index]
		name := session.RuntimeSessionName
		if session.Status == "active" && strings.HasPrefix(name, taskTerminalSessionPrefix) && len(name) == len(taskTerminalSessionPrefix)+8+len(suffix) && strings.HasSuffix(name, suffix) {
			return s.launchTerminalSession(ctx, machine, terminalService, checkout, session)
		}
	}
	nonce, err := randomHex(4)
	if err != nil {
		return nil, err
	}
	session := &domain.DevMachineTerminalSession{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, CheckoutID: &checkoutID,
		UserID: userID, Name: "Claude Code", RuntimeSessionName: taskTerminalSessionPrefix + nonce + suffix, Status: "active",
	}
	if err := s.store.CreateTerminalSession(ctx, session); err != nil {
		return nil, err
	}
	return s.launchTerminalSession(ctx, machine, terminalService, checkout, session)
}
