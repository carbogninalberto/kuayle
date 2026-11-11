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

// replyRuntimeFake is the live runtime fake under the name the fixture uses.
type replyRuntimeFake struct {
	*liveRuntimeFake
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

// A missing reply never costs the work. An attempt that ends well owing a
// reply stays a success: the result notes which message went unanswered.
// OpenCode cannot continue a session, so it is not asked again here.
func TestAnAttemptThatOwesAReplyIsStillASuccess(t *testing.T) {
	store, runtime, manager := replyFixture("opencode")
	store.settled, store.owed = []int64{3, 5}, [][]int64{{3, 5}}
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
	require.Equal(t, 1, runtime.agentRuns, "OpenCode has no session to continue: it is not asked again")
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
	require.Equal(t, 1, runtime.agentRuns)
}

const claudeConclusion = `{"type":"result","subtype":"success","is_error":false,"result":"Implemented the toggle and ran the tests."}`

// closingPrompt is the prompt of the attempt's closing continuation.
func closingPrompt(t *testing.T, runtime *notebookRuntimeFake) string {
	t.Helper()
	require.Len(t, runtime.runs, 2, "the attempt and one continuation")
	require.Equal(t, closingSuffix, runtime.runs[1].ContainerSuffix)
	var argv []string
	require.NoError(t, json.Unmarshal(runtime.runs[1].CommandArgv, &argv))
	require.Equal(t, []string{"harness", "resume", "0c9a6c1e-session", "-p"}, argv[:4], "it continues the attempt's own session")
	return argv[4]
}

func runResult(t *testing.T, run *domain.DevMachineAgentRun) agent.RunResult {
	t.Helper()
	var result agent.RunResult
	require.NoError(t, json.Unmarshal(*run.Result, &result))
	return result
}

// An attempt gets one extra turn, whatever is missing. One that ended well
// without replying to a message and without a notebook update is asked for
// both in the same closing continuation: one container, one prompt, one mark.
func TestOneClosingContinuationAsksForTheReplyAndTheNotebook(t *testing.T) {
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout: claudeConclusion, unanswered: []int64{3},
		afterAsk: func(store *notebookStoreFake) { store.updates, store.unanswered = 1, nil }})
	prompt := closingPrompt(t, runtime)
	require.Contains(t, prompt, "2 things are missing before this step can close:")
	require.Contains(t, prompt, "1. you did not reply to message 3 from the person who owns this task")
	require.Contains(t, prompt, `{"to":"orchestrator","reply_to":<message number>,"body":"..."}`)
	require.Contains(t, prompt, "2. you did not write `.kuayle/outbox/notebook.json`")
	require.Contains(t, prompt, "Do nothing else")
	require.Equal(t, 1, store.asked, "one mark for both")
	require.Equal(t, []string{closingSuffix}, runtime.removed)
	require.False(t, agentHasLiveInbox(&runtime.runs[1]), "nothing is delivered to the continuation")
	// The outbox is collected after it, and both are then settled.
	require.Equal(t, []string{"take", "take"}, runtime.steps)
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	result := runResult(t, completed)
	require.Empty(t, result.UnansweredMessages)
	require.Empty(t, result.RiskNotes)
	require.Empty(t, store.fallbacks)
	require.Equal(t, "Implemented the toggle and ran the tests.", *completed.Summary, "the continuation's reply is not the attempt's summary")

	// The agent does only one of the two: the other is noted, and the
	// attempt is not asked again.
	completed, store, runtime = notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout: claudeConclusion, unanswered: []int64{3, 4},
		afterAsk: func(store *notebookStoreFake) { store.updates, store.unanswered = 1, []int64{4} }})
	require.Len(t, runtime.runs, 2, "never a second continuation")
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	result = runResult(t, completed)
	require.Equal(t, []int64{4}, result.UnansweredMessages)
	require.Equal(t, []string{domain.UnansweredMessageNote([]int64{4})}, result.RiskNotes)
	require.Empty(t, store.fallbacks, "the notebook was written")
}

// Only a reply is missing: the continuation asks for that alone, with or
// without a notebook to keep.
func TestTheClosingContinuationAsksForAMissingReplyAlone(t *testing.T) {
	for name, required := range map[string]bool{"no notebook": false, "a notebook that was updated": true} {
		completed, store, runtime := notebookRun(t, notebookScenario{provider: "codex", required: required, updates: 1, session: "0c9a6c1e-session", continuation: resumeCommand,
			stdout:     `{"type":"item.completed","item":{"type":"agent_message","text":"Implemented the toggle and ran the tests."}}`,
			unanswered: []int64{3}, afterAsk: func(store *notebookStoreFake) { store.unanswered = nil }})
		prompt := closingPrompt(t, runtime)
		require.Contains(t, prompt, "One thing is missing before this step can close: you did not reply to message 3", name)
		require.NotContains(t, prompt, "notebook.json", name)
		require.Equal(t, 1, store.asked, name)
		require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status, name)
		result := runResult(t, completed)
		require.Empty(t, result.UnansweredMessages, name)
		require.Empty(t, result.RiskNotes, name)
	}
}

// Only the notebook is missing: nothing is said about replies. And with
// nothing missing there is no continuation at all.
func TestTheClosingContinuationAsksOnlyForWhatIsMissing(t *testing.T) {
	_, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout: claudeConclusion, afterAsk: func(store *notebookStoreFake) { store.updates = 1 }})
	prompt := closingPrompt(t, runtime)
	require.Contains(t, prompt, "One thing is missing before this step can close: you did not write `.kuayle/outbox/notebook.json`")
	require.NotContains(t, prompt, "reply_to")
	require.Equal(t, 1, store.asked)

	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, updates: 1, session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion})
	require.Len(t, runtime.runs, 1)
	require.Zero(t, store.asked)
	require.Empty(t, runtime.removed)
	require.Empty(t, runResult(t, completed).RiskNotes)
}

// A missing reply never changes how the attempt ended. Asked and still
// unanswered, without a session to continue, or already asked once: the
// attempt stays a success and the result notes the message.
func TestAMissingReplyNeverChangesTheOutcome(t *testing.T) {
	for name, scenario := range map[string]notebookScenario{
		"still unanswered after being asked": {provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion},
		"a harness without a session":        {provider: "opencode"},
		"an attempt already asked":           {provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion, alreadyAsked: true},
		"a continuation that cannot start":   {provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion, failCall: 2},
	} {
		scenario.unanswered = []int64{3}
		completed, _, runtime := notebookRun(t, scenario)
		require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status, name)
		require.Nil(t, completed.ErrorMessage, name)
		result := runResult(t, completed)
		require.Equal(t, []int64{3}, result.UnansweredMessages, name)
		require.Equal(t, []string{domain.UnansweredMessageNote([]int64{3})}, result.RiskNotes, name)
		require.LessOrEqual(t, len(runtime.runs), 2, name)
	}
	// An attempt that did not end well is not asked: its own reason stands.
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion, exit: 2, unanswered: []int64{3}})
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, completed.Status)
	require.Len(t, runtime.runs, 1)
	require.Zero(t, store.asked)
	require.Empty(t, runResult(t, completed).RiskNotes)
}

// A reply Kuayle could not collect is not a missing reply: when the outbox
// cannot be read nothing is concluded about the attempt's messages, the
// agent is not asked, and the result says the problem is Kuayle's. A message
// that reached the inbox just before the harness exited is queued again all
// the same.
func TestAReplyThatCouldNotBeCollectedIsNotAMissingReply(t *testing.T) {
	started := time.Now()
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion,
		unanswered: []int64{3}, takeErr: errors.New("exec failed")})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	result := runResult(t, completed)
	require.Empty(t, result.UnansweredMessages, "nothing is said to be unanswered")
	require.True(t, result.RepliesUnknown)
	require.Equal(t, []string{liveCollectFailedNote}, result.RiskNotes)
	require.Contains(t, liveCollectFailedNote, "Kuayle's side")
	require.Len(t, runtime.runs, 1, "the agent is not asked for what Kuayle failed to read")
	require.Zero(t, store.asked)
	require.Equal(t, 1, store.settles, "late inbox messages are still queued again")
	// What may be unread is measured from when the harness exited.
	require.False(t, store.unread.Before(started.Add(-liveReplyGrace)))
	require.False(t, store.unread.After(time.Now().Add(-liveReplyGrace)))

	// An attempt that owed no reply has none that could be missing.
	completed, _, _ = notebookRun(t, notebookScenario{provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion, takeErr: errors.New("exec failed")})
	result = runResult(t, completed)
	require.False(t, result.RepliesUnknown)
	require.Empty(t, result.RiskNotes)

	// The collection after the continuation failing is said too.
	var asked *notebookRuntimeFake
	scenario := notebookScenario{provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claudeConclusion, unanswered: []int64{3}}
	scenario.afterAsk = func(*notebookStoreFake) { asked.takeErr = errors.New("exec failed") }
	completed, _, asked = notebookRunWith(t, scenario, func(runtime *notebookRuntimeFake) { asked = runtime })
	require.Len(t, asked.runs, 2)
	result = runResult(t, completed)
	require.True(t, result.RepliesUnknown)
	require.Empty(t, result.UnansweredMessages)
	require.Equal(t, []string{liveCollectFailedNote}, result.RiskNotes)
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

// The run is still recorded as running during its closing continuation, and
// a run past its time limit is killed and marked as timed out. So the
// continuation, whatever it asks for, never runs past the attempt's own
// limit, and is skipped (without using up the attempt's one ask) when too
// little is left: it must not change how the attempt ended.
func TestTheClosingContinuationStaysWithinTheAttemptsTimeLimit(t *testing.T) {
	limit := time.Now().Add(2 * time.Minute)
	end, enough := closingEnd(limit)
	require.True(t, enough)
	require.False(t, end.After(limit.Add(-closingMargin)), "it ends before the attempt's limit, with room to record the result")
	end, enough = closingEnd(time.Now().Add(time.Hour))
	require.True(t, enough)
	require.WithinDuration(t, time.Now().Add(closingTimeout), end, 5*time.Second, "with plenty of time left it has its own bound")
	for name, deadline := range map[string]time.Time{
		"almost at the limit": time.Now().Add(closingMargin + closingMinimum - time.Second),
		"past the limit":      time.Now().Add(-time.Second),
		"an unknown limit":    {},
	} {
		_, enough = closingEnd(deadline)
		require.False(t, enough, name)
	}

	// An attempt that finished close to its limit is not asked, for a reply
	// or for the notebook: it stays a success, and Kuayle notes both.
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout: claudeConclusion, unanswered: []int64{3}, maxRuntime: 60})
	require.Len(t, runtime.runs, 1)
	require.Zero(t, store.asked, "its one ask is not used up either")
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	result := runResult(t, completed)
	require.Equal(t, []int64{3}, result.UnansweredMessages)
	require.Contains(t, result.RiskNotes, domain.UnansweredMessageNote([]int64{3}))
	require.Len(t, store.fallbacks, 1, "Kuayle records the attempt in the notebook itself")
}
