package machine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// replyStoreFake is a store that knows which messages the attempt owes a
// reply to: owed is what each check during the run reports (the last one
// repeats), settled what is left when the attempt ends.
type replyStoreFake struct {
	*liveStoreFake
	owed    [][]int64
	checks  int
	settled []int64
	grace   time.Duration
}

func (f *replyStoreFake) UnansweredLiveMessages(context.Context, uuid.UUID) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if len(f.owed) == 0 {
		return nil, nil
	}
	seqs := f.owed[0]
	if len(f.owed) > 1 {
		f.owed = f.owed[1:]
	}
	return seqs, nil
}

func (f *replyStoreFake) SettleLiveMessages(_ context.Context, _ uuid.UUID, grace time.Duration) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grace = grace
	return f.settled, nil
}

func replyFixture(provider string) (*replyStoreFake, *liveRuntimeFake, *Manager) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	developer := "developer"
	store := &replyStoreFake{liveStoreFake: &liveStoreFake{managerStoreFake: &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		services: []domain.DevMachineService{{ID: uuid.New(), ServiceType: "terminal", ServiceKey: "terminal", Status: "running", ContainerID: &developer}},
		agentRun: &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: provider, Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30},
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: provider, ImageRef: "kuayle/" + provider + ":test", Enabled: true},
	}}}
	runtime := &liveRuntimeFake{runtimeFake: &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent", ExitCode: 0}}}
	registry := agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test"))
	manager := NewManager(store, runtime, registry, nil, make([]byte, 32), make([]byte, 32), "test")
	manager.liveInterval = 10 * time.Millisecond
	return store, runtime, manager
}

func (f *replyStoreFake) runAgent(t *testing.T, manager *Manager) {
	t.Helper()
	require.NoError(t, manager.runAgent(context.Background(), f.machine, &domain.DevMachineOperation{
		MachineID: f.machine.ID, WorkspaceID: f.machine.WorkspaceID, AgentRunID: &f.agentRun.ID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}))
}

// Codex and OpenCode have no hook that keeps them from stopping, so the rule
// is enforced when the attempt ends, by the manager and so for every harness
// alike: an attempt that exits normally with a delivered message unanswered
// is not a success, and says which message.
func TestAnAttemptThatEndsWithAnUnansweredMessageIsNotASuccess(t *testing.T) {
	for _, provider := range []string{"opencode"} {
		t.Run(provider, func(t *testing.T) {
			store, _, manager := replyFixture(provider)
			store.settled = []int64{3, 5}
			store.runAgent(t, manager)
			require.NotNil(t, store.completedRun)
			require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
			require.NotNil(t, store.completedRun.ErrorMessage)
			require.Equal(t, domain.UnansweredMessageReason([]int64{3, 5}), *store.completedRun.ErrorMessage)
			var result agent.RunResult
			require.NoError(t, json.Unmarshal(*store.completedRun.Result, &result))
			require.Equal(t, "failed", result.Status)
			require.Equal(t, []int64{3, 5}, result.UnansweredMessages)
			require.False(t, result.NoFinalMessage)
			require.Equal(t, liveReplyGrace, store.grace, "a message written in the attempt's last seconds is queued again, not counted")

			// With every message answered the same attempt succeeds.
			store, _, manager = replyFixture(provider)
			store.runAgent(t, manager)
			require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, store.completedRun.Status)
			require.Nil(t, store.completedRun.ErrorMessage)
		})
	}
}

// An attempt that failed on its own keeps its own reason.
func TestAFailedAttemptKeepsItsOwnReason(t *testing.T) {
	store, runtime, manager := replyFixture("opencode")
	store.settled = []int64{3}
	runtime.agentExecution = &AgentExecution{ContainerID: "agent", ExitCode: 2}
	store.runAgent(t, manager)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	if store.completedRun.ErrorMessage != nil {
		require.False(t, domain.IsUnansweredMessageReason(*store.completedRun.ErrorMessage))
	}
	var result agent.RunResult
	require.NoError(t, json.Unmarshal(*store.completedRun.Result, &result))
	require.Empty(t, result.UnansweredMessages)
}

// While the attempt runs its inbox says which messages still owe a reply
// (Claude Code's Stop hook reads it): written when one is owed, rewritten
// when that changes, and not at all for an attempt that never owed one.
func TestTheInboxListsTheMessagesStillOwedAReply(t *testing.T) {
	store, runtime, manager := replyFixture("opencode")
	store.owed = [][]int64{nil, {3}, {3}, {3, 4}, {}}
	lists := func() []string {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		written := []string{}
		for _, file := range runtime.written {
			require.Equal(t, domain.AgentLiveUnansweredFile, file.Name)
			written = append(written, string(file.Data))
		}
		return written
	}
	runtime.onAgentOutput = func(AgentOutput) error {
		require.Eventually(t, func() bool { return len(lists()) >= 3 }, 5*time.Second, 5*time.Millisecond)
		return nil
	}
	store.runAgent(t, manager)
	require.Equal(t, []string{`{"unanswered":[3]}`, `{"unanswered":[3,4]}`, `{"unanswered":[]}`}, lists()[:3])

	store, runtime, manager = replyFixture("opencode")
	runtime.onAgentOutput = func(AgentOutput) error {
		require.Eventually(t, func() bool {
			store.mu.Lock()
			defer store.mu.Unlock()
			return store.checks >= 3
		}, 5*time.Second, 5*time.Millisecond)
		return nil
	}
	store.runAgent(t, manager)
	require.Empty(t, lists())
}
