package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

// demoAccess is the public demo rule with one instance administrator.
func demoAccess(sysadmin uuid.UUID) *MachineAccess {
	return NewMachineAccess(func(user uuid.UUID) bool { return user == sysadmin })
}

// refusedBeforeStorage runs a call on a service that has no storage: the demo
// rule must refuse before anything is read or queued, so reaching storage
// (a nil dereference here) or any other answer fails the test.
func refusedBeforeStorage(t *testing.T, name string, call func() error) {
	t.Helper()
	err := func() (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("the call went past the demo rule to storage: %v", recovered)
			}
		}()
		return call()
	}()
	require.ErrorIs(t, err, ErrDemoMachineForbidden, name)
}

func TestMachineAccessRule(t *testing.T) {
	sysadmin, member := uuid.New(), uuid.New()
	var off *MachineAccess
	require.NoError(t, off.Require(member), "no rule is configured")
	require.NoError(t, off.RequireActor(context.Background()))
	require.NoError(t, NewMachineAccess(func(uuid.UUID) bool { return true }).RequireActor(context.Background()), "demo mode is off")

	demo := demoAccess(sysadmin)
	require.NoError(t, demo.Require(sysadmin))
	require.ErrorIs(t, demo.Require(member), ErrDemoMachineForbidden)
	require.ErrorIs(t, demo.Require(uuid.Nil), ErrDemoMachineForbidden)
	require.ErrorIs(t, demo.RequireActor(context.Background()), ErrDemoMachineForbidden, "no caller")
	require.NoError(t, demo.RequireActor(domain.WithAccess(context.Background(), domain.Access{UserID: sysadmin})))
	require.ErrorIs(t, demo.RequireActor(domain.WithAccess(context.Background(), domain.Access{UserID: member})), ErrDemoMachineForbidden)
	require.ErrorIs(t, NewMachineAccess(func(uuid.UUID) bool { return false }).Require(sysadmin), ErrDemoMachineForbidden, "demo mode without SYSADMINS admits no one")
}

// In demo mode a member who is not an instance administrator cannot start or
// steer work on a machine through the task service, whichever caller (route,
// approval, automation, follow-up, GitHub mention) reached it.
func TestDemoModeRefusesTaskWorkForNonSysadmins(t *testing.T) {
	sysadmin, member, ws, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machines := NewDevMachineService(nil, nil, true, "machines.test.invalid", make([]byte, 32), time.Minute, DevMachineImages{})
	machines.SetMachineAccess(demoAccess(sysadmin))
	tasks := NewAgentTaskService(nil, machines, nil)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: member})
	for name, call := range map[string]func() error{
		"create": func() error {
			_, err := tasks.Create(ctx, ws, id, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
			return err
		},
		"retry and follow-up": func() error {
			_, err := tasks.Retry(ctx, ws, id, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Prompt: "Again"})
			return err
		},
		"review": func() error {
			_, _, err := tasks.RequestReview(ctx, ws, id, id, domain.AgentReviewRequest{RequestID: uuid.New(), Provider: "codex"})
			return err
		},
		"run check": func() error { _, err := tasks.RunCheck(ctx, ws, id, nil); return err },
		"start preview": func() error {
			_, err := tasks.RequestPreview(ctx, ws, id, true)
			return err
		},
		"live message": func() error {
			_, err := tasks.QueueLiveMessage(ctx, ws, id, domain.AgentLiveSenderUser, "Use the staging API")
			return err
		},
		"continue after sign-in": func() error {
			_, err := tasks.ContinueAfterSignIn(ctx, ws, id, uuid.New())
			return err
		},
		"open in terminal": func() error { _, err := tasks.OpenInClaudeCode(ctx, ws, id); return err },
	} {
		refusedBeforeStorage(t, name, call)
	}
}

// The Dev Machine service refuses the same members directly, so the worker
// and the assistant's port tools cannot use a machine for them either.
func TestDemoModeRefusesMachineUseForNonSysadmins(t *testing.T) {
	sysadmin, member, ws, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machines := NewDevMachineService(nil, nil, true, "machines.test.invalid", make([]byte, 32), time.Minute, DevMachineImages{})
	machines.SetMachineAccess(demoAccess(sysadmin))
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"create": func() error {
			_, _, err := machines.Create(ctx, ws, member, dto.CreateDevMachineRequest{Name: "demo"})
			return err
		},
		"start": func() error {
			_, err := machines.Lifecycle(ctx, ws, id, member, domain.DevMachineOpStart, "")
			return err
		},
		"checkout": func() error {
			_, err := machines.CheckoutIssue(ctx, ws, id, member, dto.CheckoutIssueRequest{IssueID: id.String()})
			return err
		},
		"agent run": func() error {
			_, err := machines.createAgentRun(ctx, ws, id, member, dto.CreateAgentRunRequest{Provider: "codex", Mode: "autonomous", Prompt: "Implement"}, uuid.New())
			return err
		},
		"launch service": func() error {
			_, err := machines.LaunchService(ctx, ws, id, member, "terminal", nil)
			return err
		},
		"terminal session": func() error {
			_, err := machines.CreateTerminalSession(ctx, ws, id, member, dto.CreateTerminalSessionRequest{})
			return err
		},
		"task terminal": func() error {
			_, err := machines.OpenTaskTerminal(ctx, ws, id, member, id, "claude-code", id, "")
			return err
		},
		"expose port": func() error { _, err := machines.CreatePort(ctx, ws, id, member, 3000, nil); return err },
		"open port":   func() error { _, err := machines.LaunchPort(ctx, ws, id, id, member); return err },
		"snapshot environment": func() error {
			_, err := machines.SnapshotEnvironment(ctx, ws, member, dto.CreateDevMachineEnvironmentRequest{})
			return err
		},
	} {
		refusedBeforeStorage(t, name, call)
	}
}

// The task worker acts as the task's creator. A task of a member the demo
// rule excludes never reaches a machine and fails with the reason.
func TestDemoModeStopsTaskWorkerForNonSysadminCreators(t *testing.T) {
	sysadmin, member := uuid.New(), uuid.New()
	machines := NewDevMachineService(nil, nil, true, "machines.test.invalid", make([]byte, 32), time.Minute, DevMachineImages{})
	machines.SetMachineAccess(demoAccess(sysadmin))
	runtime := &devMachineTaskRuntime{machines: machines}
	machine := uuid.New()
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), IssueID: uuid.New(), CreatorID: member, MachineID: &machine, State: "preparing", Provider: "codex"}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"prepare":  func() error { _, err := runtime.Prepare(ctx, &task); return err },
		"attempt":  func() error { _, err := runtime.Attempt(ctx, task); return err },
		"validate": func() error { _, err := runtime.Validate(ctx, task); return err },
		"publish":  func() error { _, err := runtime.Publish(ctx, task); return err },
	} {
		refusedBeforeStorage(t, name, call)
	}

	store := &taskStoreFake{task: task}
	svc := &AgentTaskService{store: store, runtime: &taskRuntimeFake{prepareErr: ErrDemoMachineForbidden}}
	require.NoError(t, svc.step(ctx, store.task))
	require.Equal(t, demoMachineRefusal, store.failReason, "the task fails instead of being retried forever")
}
