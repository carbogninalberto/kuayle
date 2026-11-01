package service

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// Only the task's creator opens a Claude Code task, and only while nothing
// else uses its session: the task's native session is resumed when it was
// recorded on the task's current machine.
func TestTaskTerminalSessionRules(t *testing.T) {
	creator, machine, other := uuid.New(), uuid.New(), uuid.New()
	native := uuid.NewString()
	task := &domain.AgentTask{ID: uuid.New(), CreatorID: creator, Provider: "claude-code", State: "awaiting_review", MachineID: &machine}
	sessions := &domain.AgentTaskSessions{Sessions: []domain.AgentSessionView{
		{AgentSession: domain.AgentSession{TaskID: uuid.New(), MachineID: &machine, NativeSessionID: ptrString(uuid.NewString())}},
		{AgentSession: domain.AgentSession{TaskID: task.ID, MachineID: &machine, NativeSessionID: &native}},
	}}

	got, err := taskTerminalSession(task, creator, sessions)
	require.NoError(t, err)
	require.Equal(t, native, got, "the task's own session, not a review's")

	_, err = taskTerminalSession(task, other, sessions)
	require.ErrorIs(t, err, repository.ErrAgentTaskUnauthorized, "another member cannot open the creator's task")

	for _, state := range []string{"queued", "preparing", "running", "waiting_input", "validating", "publishing", "cancelling", "completed", "cancelled"} {
		busy := *task
		busy.State = state
		_, err = taskTerminalSession(&busy, creator, sessions)
		require.ErrorIs(t, err, ErrTaskTerminalBusy, state)
	}
	for _, state := range []string{"blocked", "failed"} {
		stopped := *task
		stopped.State = state
		_, err = taskTerminalSession(&stopped, creator, sessions)
		require.NoError(t, err, state)
	}

	codex := *task
	codex.Provider = "codex"
	_, err = taskTerminalSession(&codex, creator, sessions)
	require.ErrorIs(t, err, ErrTaskTerminalUnsupported)

	elsewhere := uuid.New()
	moved := *task
	moved.MachineID = &elsewhere
	got, err = taskTerminalSession(&moved, creator, sessions)
	require.NoError(t, err)
	require.Empty(t, got, "a session recorded on another machine has no transcript here: start a new one")

	sessions.Sessions[1].NativeSessionID = ptrString("--dangerous")
	got, err = taskTerminalSession(task, creator, sessions)
	require.NoError(t, err)
	require.Empty(t, got)
}

func ptrString(value string) *string { return &value }

func taskTerminalFixture(t *testing.T) (*devMachineStoreFake, *DevMachineService, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	workspaceID, userID, machineID, checkoutID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	policy := testPolicy(workspaceID)
	policy.AllowedProviders = json.RawMessage(`["claude-code"]`)
	store := &devMachineStoreFake{
		policy: policy,
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID,
			RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning,
		},
		service:   &domain.DevMachineService{ID: uuid.New(), MachineID: machineID, ServiceKey: "terminal", ServiceType: "terminal", Status: "running"},
		checkouts: []domain.DevMachineCheckout{{ID: checkoutID, WorkspaceID: workspaceID, MachineID: machineID, WorkspacePath: "/workspace/tasks/rec-1", Status: "ready"}},
	}
	svc := NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{}, "https://app.example.test")
	return store, svc, workspaceID, userID, checkoutID
}

// The terminal opens in the task's checkout with a session named for the
// task and its native session; reopening attaches to the same session.
func TestOpenTaskTerminalRunsClaudeCodeInTheTasksCheckout(t *testing.T) {
	store, svc, workspaceID, userID, checkoutID := taskTerminalFixture(t)
	taskID, native := uuid.New(), uuid.NewString()

	launch, err := svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, userID, checkoutID, "claude-code", taskID, native)
	require.NoError(t, err)
	require.Equal(t, "ready", launch.Status)
	require.Len(t, store.terminalSessions, 1)
	session := store.terminalSessions[0]
	require.Equal(t, userID, session.UserID)
	require.Equal(t, checkoutID, *session.CheckoutID)
	require.True(t, strings.HasPrefix(session.RuntimeSessionName, "ktask-"), session.RuntimeSessionName)
	require.True(t, strings.HasSuffix(session.RuntimeSessionName, "-"+taskID.String()+"-"+native), session.RuntimeSessionName)
	require.LessOrEqual(t, len(session.RuntimeSessionName), 128)
	parsed, err := url.Parse(launch.WebSocketURL)
	require.NoError(t, err)
	require.Equal(t, "/workspace/tasks/rec-1", parsed.Query().Get("cwd"))
	require.Equal(t, session.RuntimeSessionName, parsed.Query().Get("session"))
	require.Equal(t, session.ID, *store.createdTicket.TerminalSessionID)

	again, err := svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, userID, checkoutID, "claude-code", taskID, native)
	require.NoError(t, err)
	require.Len(t, store.terminalSessions, 1, "reopening attaches to the same Claude Code")
	require.Equal(t, launch.Session.ID, again.Session.ID)

	store.terminalSessions[0].Status = "closed"
	_, err = svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, userID, checkoutID, "claude-code", taskID, native)
	require.NoError(t, err)
	require.Len(t, store.terminalSessions, 2, "a closed session is not reused")
	require.NotEqual(t, store.terminalSessions[0].RuntimeSessionName, store.terminalSessions[1].RuntimeSessionName)
}

// Only the machine's owner opens it, and only while the policy allows the
// harness and the checkout is ready.
func TestOpenTaskTerminalChecksOwnerPolicyAndCheckout(t *testing.T) {
	store, svc, workspaceID, userID, checkoutID := taskTerminalFixture(t)
	taskID := uuid.New()

	_, err := svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, uuid.New(), checkoutID, "claude-code", taskID, "")
	require.ErrorIs(t, err, ErrMachineNotFound)

	store.policy.AllowedProviders = json.RawMessage(`["codex"]`)
	_, err = svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, userID, checkoutID, "claude-code", taskID, "")
	require.ErrorIs(t, err, ErrProviderNotAllowed)
	store.policy.AllowedProviders = json.RawMessage(`["claude-code"]`)

	store.checkouts[0].Status = "preparing"
	_, err = svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, userID, checkoutID, "claude-code", taskID, "")
	require.ErrorIs(t, err, ErrCheckoutNotReady)
	require.Empty(t, store.terminalSessions)

	store.checkouts[0].Status = "ready"
	launch, err := svc.OpenTaskTerminal(context.Background(), workspaceID, store.machine.ID, userID, checkoutID, "claude-code", taskID, "")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(store.terminalSessions[0].RuntimeSessionName, "-"+taskID.String()), "no native session: a new one")
	require.NotNil(t, launch.Session)
}
