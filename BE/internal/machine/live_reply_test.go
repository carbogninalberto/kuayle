package machine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// replyStoreFake is a store that knows which messages the attempt owes a
// reply to: owed is what each check reports (the last one repeats), settled
// what is left when the attempt ends.
type replyStoreFake struct {
	*liveStoreFake
	owed    [][]int64
	checks  int
	settled []int64
	settles int
	unread  time.Time
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

func (f *replyStoreFake) SettleLiveMessages(_ context.Context, _ uuid.UUID, unread time.Time) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settles++
	f.unread = unread
	return f.settled, nil
}

// replyRuntimeFake can continue a finished attempt, and can fail to read the
// outbox.
type replyRuntimeFake struct {
	*liveRuntimeFake
	continuations [][]string
	deadlines     []time.Time
	secrets       []map[string]string
	onContinue    func(observer *AgentObserver)
	continueErr   error
	takeErr       error
	takes         int
}

func (f *replyRuntimeFake) TakeOutbox(ctx context.Context, services []domain.DevMachineService) ([]domain.AgentOutboxFile, error) {
	f.takes++
	if f.takeErr != nil {
		return nil, f.takeErr
	}
	return f.liveRuntimeFake.TakeOutbox(ctx, services)
}

func (f *replyRuntimeFake) ContinueAgent(ctx context.Context, _ *domain.DevMachine, _ *domain.DevMachineAgentRun, _ *domain.DevMachineAgentProvider, _ *domain.DevMachineCheckout, secrets map[string]string, continuation []string, observer *AgentObserver) (*AgentExecution, error) {
	f.continuations = append(f.continuations, continuation)
	deadline, _ := ctx.Deadline()
	f.deadlines = append(f.deadlines, deadline)
	f.secrets = append(f.secrets, secrets)
	if f.onContinue != nil {
		f.onContinue(observer)
	}
	return &AgentExecution{ContainerID: "reply", ExitCode: 0}, f.continueErr
}

func replyFixture(provider string) (*replyStoreFake, *replyRuntimeFake, *Manager) {
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
	runtime := &replyRuntimeFake{liveRuntimeFake: &liveRuntimeFake{runtimeFake: &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent", ExitCode: 0}}}}
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

const replySession = "0199a213-81c0-7800-8aa1-bbab2a035a53"

// taskAttempt makes the fixture's run a task attempt of harness that is
// still recorded as running, with a native session and the argv Kuayle
// built for it.
func (f *replyStoreFake) taskAttempt(t *testing.T, harness agent.Provider) liveReplyRequest {
	t.Helper()
	task := uuid.New()
	session := replySession
	invocation, err := harness.BuildInvocation(agent.RunInput{Mode: agent.ModeAutonomous, Prompt: "Implement dark mode", Model: "model-x", WorkspacePath: "/workspace/tasks/eng-1",
		Session: &agent.SessionInput{Dir: "/workspace/.kuayle/agent-sessions/" + task.String()}})
	require.NoError(t, err)
	argv, err := json.Marshal(invocation.Argv)
	require.NoError(t, err)
	f.agentRun.TaskID, f.agentRun.NativeSessionID, f.agentRun.CommandArgv = &task, &session, argv
	f.agentRun.Prompt, f.agentRun.Status = "Implement dark mode", domain.DevMachineAgentRunStatusRunning
	return liveReplyRequest{provider: f.provider, secrets: &domain.AgentRunSecretSnapshot{Environment: map[string]string{"OTHER": "value"}}, exited: time.Now(), finished: true, deadline: time.Now().Add(time.Hour)}
}

// A missing reply never costs the work. An attempt that ends well owing a
// reply stays a success: the result notes which message went unanswered.
// OpenCode cannot continue a session, so it is not asked again here.
func TestAnAttemptThatOwesAReplyIsStillASuccess(t *testing.T) {
	store, runtime, manager := replyFixture("opencode")
	store.settled = []int64{3, 5}
	started := time.Now()
	store.runAgent(t, manager)
	require.NotNil(t, store.completedRun)
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, store.completedRun.Status)
	require.Nil(t, store.completedRun.ErrorMessage)
	var result agent.RunResult
	require.NoError(t, json.Unmarshal(*store.completedRun.Result, &result))
	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, []int64{3, 5}, result.UnansweredMessages)
	require.Contains(t, result.RiskNotes, domain.UnansweredMessageNote([]int64{3, 5}))
	require.Empty(t, runtime.continuations, "OpenCode has no session to continue")
	// What may be unread is measured from when the harness exited, not from
	// when Kuayle got round to settling.
	require.False(t, store.unread.Before(started.Add(-liveReplyGrace)))
	require.False(t, store.unread.After(time.Now().Add(-liveReplyGrace)))

	// With every message answered nothing is noted.
	store, _, manager = replyFixture("opencode")
	store.runAgent(t, manager)
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, store.completedRun.Status)
	var answered agent.RunResult
	require.NoError(t, json.Unmarshal(*store.completedRun.Result, &answered))
	require.Empty(t, answered.UnansweredMessages)
	require.Empty(t, answered.RiskNotes)
}

// An attempt that failed on its own keeps its own reason and is not asked
// again.
func TestAFailedAttemptKeepsItsOwnReason(t *testing.T) {
	store, runtime, manager := replyFixture("opencode")
	store.settled = []int64{3}
	runtime.agentExecution = &AgentExecution{ContainerID: "agent", ExitCode: 2}
	store.runAgent(t, manager)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	var result agent.RunResult
	require.NoError(t, json.Unmarshal(*store.completedRun.Result, &result))
	require.Equal(t, []int64{3}, result.UnansweredMessages)
	require.Empty(t, result.RiskNotes)
	require.Empty(t, runtime.continuations)
}

// Claude Code and Codex can continue a session: an attempt that ended well
// owing a reply is continued once, on that session, with a prompt that asks
// only for the reply. Its output is discarded, and the reply it writes is
// collected.
func TestAFinishedAttemptIsAskedOnceForTheReplyItOwes(t *testing.T) {
	for name, harness := range map[string]agent.Provider{"claude-code": agent.NewClaudeCodeProvider(""), "codex": agent.NewCodexProvider("")} {
		t.Run(name, func(t *testing.T) {
			store, runtime, manager := replyFixture(name)
			request := store.taskAttempt(t, harness)
			store.settled = []int64{3}
			// The continuation writes the reply: afterwards nothing is owed.
			store.owed = [][]int64{{}}
			runtime.onContinue = func(observer *AgentObserver) {
				require.NoError(t, observer.Authorize())
				require.NoError(t, observer.Output("stdout", []byte(time.Now().UTC().Format(time.RFC3339Nano)+" REPLY-ONLY-OUTPUT\n")))
				runtime.outboxes = [][]domain.AgentOutboxFile{{{Worktree: "/workspace/tasks/eng-1", Name: "reply.json", Data: []byte(`{"to":"orchestrator","reply_to":3,"body":"Understood."}`)}}}
			}
			settled := manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes := settled.unanswered, settled.notes
			require.Empty(t, unanswered)
			require.Empty(t, notes)
			require.Len(t, runtime.continuations, 1)
			argv := runtime.continuations[0]
			require.Equal(t, domain.AgentLiveReplyPrompt([]int64{3}), argv[len(argv)-1], "only the reply is asked for")
			require.Contains(t, argv, replySession, "on the attempt's own session")
			require.NotContains(t, argv, "Implement dark mode")
			require.Equal(t, map[string]string{"OTHER": "value"}, runtime.secrets[0])
			require.Empty(t, store.outputChunks, "its output is not the attempt's")
			require.Len(t, store.recorded, 1, "the reply it wrote is collected")
			require.Equal(t, 2, runtime.takes)
			types := []string{}
			for _, event := range store.events {
				types = append(types, event.EventType)
			}
			require.Contains(t, types, "agent_live_messages.reply_requested")

			// Still no reply: it is not asked a second time. The message is
			// noted as unanswered and the attempt stays as it ended.
			store, runtime, manager = replyFixture(name)
			request = store.taskAttempt(t, harness)
			store.settled, store.owed = []int64{3, 4}, [][]int64{{3}}
			settled = manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes = settled.unanswered, settled.notes
			require.Equal(t, []int64{3}, unanswered)
			require.Equal(t, []string{domain.UnansweredMessageNote([]int64{3})}, notes)
			require.Len(t, runtime.continuations, 1, "one continuation, never a loop")

			// A continuation that cannot run changes nothing either.
			store, runtime, manager = replyFixture(name)
			request = store.taskAttempt(t, harness)
			store.settled, store.owed = []int64{3}, [][]int64{{3}}
			runtime.continueErr = errors.New("docker is unavailable")
			settled = manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes = settled.unanswered, settled.notes
			require.Equal(t, []int64{3}, unanswered)
			require.Len(t, notes, 1)
		})
	}
}

// The continuation is only for an attempt that ended well and that nobody
// cancelled, with a session to continue; and it uses the sign-in the attempt
// may have refreshed, not the one it started with.
func TestOnlyAFinishedAttemptWithASessionIsAskedAgain(t *testing.T) {
	claude := agent.NewClaudeCodeProvider("")
	for name, change := range map[string]func(*replyStoreFake, *liveReplyRequest){
		"an attempt that did not end well": func(_ *replyStoreFake, request *liveReplyRequest) { request.finished = false },
		"a cancelled attempt": func(store *replyStoreFake, _ *liveReplyRequest) {
			store.agentRun.Status = domain.DevMachineAgentRunStatusCancelled
		},
		"an attempt without a session": func(store *replyStoreFake, _ *liveReplyRequest) { store.agentRun.NativeSessionID = nil },
		"a run that is not a task's":   func(store *replyStoreFake, _ *liveReplyRequest) { store.agentRun.TaskID = nil },
		"an argv Kuayle did not build": func(store *replyStoreFake, _ *liveReplyRequest) {
			store.agentRun.CommandArgv = json.RawMessage(`["claude","-p","x"]`)
		},
	} {
		store, runtime, manager := replyFixture("claude-code")
		request := store.taskAttempt(t, claude)
		store.settled, store.owed = []int64{3}, [][]int64{{3}}
		change(store, &request)
		unanswered := manager.settleReplies(context.Background(), store.machine, store.agentRun, request).unanswered
		require.Equal(t, []int64{3}, unanswered, name)
		require.Empty(t, runtime.continuations, name)
	}

	store, runtime, manager := replyFixture("claude-code")
	request := store.taskAttempt(t, claude)
	name := signInSecretNames["claude-code"]
	require.NotEmpty(t, name)
	request.secrets.Environment[name], request.signIn = "sign-in-at-start", "sign-in-refreshed"
	store.settled, store.owed = []int64{3}, [][]int64{{}}
	manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
	require.Len(t, runtime.secrets, 1)
	require.Equal(t, "sign-in-refreshed", runtime.secrets[0][name])
	require.Equal(t, "sign-in-at-start", request.secrets.Environment[name], "the attempt's own secrets are left alone")
}

// A reply Kuayle could not collect is not a missing reply: when the outbox
// cannot be read nothing is concluded about the attempt's messages, the
// agent is not asked again, and the result says the problem is Kuayle's.
func TestAReplyThatCouldNotBeCollectedIsNotAMissingReply(t *testing.T) {
	store, runtime, manager := replyFixture("claude-code")
	request := store.taskAttempt(t, agent.NewClaudeCodeProvider(""))
	store.settled, store.owed = []int64{3}, [][]int64{{3}}
	runtime.takeErr = errors.New("exec failed")
	settled := manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes := settled.unanswered, settled.notes
	require.Empty(t, unanswered, "nothing is said to be unanswered")
	require.True(t, settled.unknown, "whether the agent replied is not known")
	require.Equal(t, []string{liveCollectFailedNote}, notes)
	require.Contains(t, liveCollectFailedNote, "Kuayle's side")
	// A message that reached the inbox just before the harness exited is
	// still queued again: that does not depend on the outbox.
	require.Equal(t, 1, store.settles)
	require.False(t, store.unread.IsZero())
	require.Empty(t, runtime.continuations)
	types := []string{}
	for _, event := range store.events {
		types = append(types, event.EventType)
	}
	require.Contains(t, types, "agent_live_messages.collect_failed")

	// The developer container being gone is the same.
	store, _, manager = replyFixture("claude-code")
	request = store.taskAttempt(t, agent.NewClaudeCodeProvider(""))
	store.settled, store.owed = []int64{3}, [][]int64{{3}}
	store.services = nil
	settled = manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes = settled.unanswered, settled.notes
	require.Empty(t, unanswered)
	require.Equal(t, []string{liveCollectFailedNote}, notes)

	// An attempt that owed no reply has none that could be missing.
	store, runtime, manager = replyFixture("claude-code")
	request = store.taskAttempt(t, agent.NewClaudeCodeProvider(""))
	runtime.takeErr = errors.New("exec failed")
	settled = manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes = settled.unanswered, settled.notes
	require.Empty(t, unanswered)
	require.Empty(t, notes)

	// The collection after the continuation failing is said too.
	store, runtime, manager = replyFixture("claude-code")
	request = store.taskAttempt(t, agent.NewClaudeCodeProvider(""))
	store.settled, store.owed = []int64{3}, [][]int64{{3}}
	runtime.onContinue = func(*AgentObserver) { runtime.takeErr = errors.New("exec failed") }
	settled = manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
			unanswered, notes = settled.unanswered, settled.notes
	require.Empty(t, unanswered)
	require.Equal(t, []string{liveCollectFailedNote}, notes)
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

// The run is still recorded as running while it is asked for a reply, and a
// run past its time limit is killed and marked as timed out. So the
// continuation never runs past the attempt's own limit, and is skipped when
// too little of it is left: it must not change how the attempt ended.
func TestTheReplyContinuationStaysWithinTheAttemptsTimeLimit(t *testing.T) {
	claude := agent.NewClaudeCodeProvider("")
	store, runtime, manager := replyFixture("claude-code")
	request := store.taskAttempt(t, claude)
	store.settled, store.owed = []int64{3}, [][]int64{{}}
	request.deadline = time.Now().Add(2 * time.Minute)
	manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
	require.Len(t, runtime.deadlines, 1)
	require.False(t, runtime.deadlines[0].After(request.deadline.Add(-liveReplyMargin)), "it ends before the attempt's limit, with room to record the result")
	require.True(t, runtime.deadlines[0].After(time.Now()))

	// With plenty of time left it gets its own bound.
	store, runtime, manager = replyFixture("claude-code")
	request = store.taskAttempt(t, claude)
	store.settled, store.owed = []int64{3}, [][]int64{{}}
	manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
	require.WithinDuration(t, time.Now().Add(liveReplyTimeout), runtime.deadlines[0], 5*time.Second)

	for name, deadline := range map[string]time.Time{
		"almost at the limit": time.Now().Add(liveReplyMargin + liveReplyMinimum - time.Second),
		"past the limit":      time.Now().Add(-time.Second),
		"an unknown limit":    {},
	} {
		store, runtime, manager = replyFixture("claude-code")
		request = store.taskAttempt(t, claude)
		store.settled, store.owed = []int64{3}, [][]int64{{3}}
		request.deadline = deadline
		settled := manager.settleReplies(context.Background(), store.machine, store.agentRun, request)
		require.Empty(t, runtime.continuations, name)
		require.Equal(t, []int64{3}, settled.unanswered, name)
		require.Equal(t, []string{domain.UnansweredMessageNote([]int64{3})}, settled.notes, name)
	}
}
