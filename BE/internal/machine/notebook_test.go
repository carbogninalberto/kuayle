package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

type notebookStoreFake struct {
	*managerStoreFake
	updates   int
	problem   string
	stateErr  error
	snapshots []string
	listErr   error
	recorded  []domain.AgentOutboxFile
	recordErr error
	// asked counts continuations; afterAsk is what the agent leaves when
	// asked (its update count and the validator's message).
	asked           int
	alreadyAsked    bool
	fallbacks       []string
	notCollected    int
	clearedReadOnly int
	fallbackProblem string
	unchanged       bool
	resets          int
	closingSeconds  *int
}

func (f *notebookStoreFake) NotebookUpdateState(context.Context, uuid.UUID) (domain.NotebookRunState, error) {
	return domain.NotebookRunState{Updates: f.updates, Unchanged: f.unchanged, Problem: f.problem}, f.stateErr
}

func (f *notebookStoreFake) ResetNotebookState(context.Context, uuid.UUID) error {
	f.resets++
	return nil
}

func (f *notebookStoreFake) RecordClosingSeconds(_ context.Context, _ uuid.UUID, seconds int) error {
	f.closingSeconds = &seconds
	return nil
}

func (f *notebookStoreFake) MarkClosingAsked(context.Context, uuid.UUID) (bool, error) {
	if f.alreadyAsked {
		return false, nil
	}
	f.alreadyAsked = true
	f.asked++
	return true, nil
}

func (f *notebookStoreFake) RecordNotebookFallback(_ context.Context, _ uuid.UUID, summary, problem string) (string, error) {
	f.fallbacks = append(f.fallbacks, summary)
	f.fallbackProblem = problem
	return domain.NotebookNotUpdated(problem), nil
}

func (f *notebookStoreFake) NoteNotebookNotCollected(context.Context, uuid.UUID) error {
	f.notCollected++
	return nil
}

func (f *notebookStoreFake) ClearWorktreeReadOnly(context.Context, uuid.UUID) error {
	f.clearedReadOnly++
	return nil
}

func (f *notebookStoreFake) ListAgentRunSecretSnapshots(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
	return f.snapshots, f.listErr
}

func (f *notebookStoreFake) RecordAgentOutbox(_ context.Context, _ uuid.UUID, files []domain.AgentOutboxFile) (int, error) {
	if f.recordErr != nil {
		return 0, f.recordErr
	}
	f.recorded = append(f.recorded, files...)
	return 0, nil
}

// notebookRuntimeFake records every container the manager runs for the
// attempt and plays an outbox.
type notebookRuntimeFake struct {
	*runtimeFake
	runs       []domain.DevMachineAgentRun
	onRun      func(call int)
	outbox     []domain.AgentOutboxFile
	takeErr    error
	prepareErr error
	acked      []domain.AgentOutboxFile
	// failCall makes that RunAgent call fail (2: the closing continuation).
	failCall int
	// removed are the suffixes of the containers removed by name; steps is
	// the order of what happened to the outbox.
	removed []string
	steps   []string
	// worktreeChanged is what Kuayle's comparison of the worktree finds.
	worktreeChanged bool
	states          int
}

func (f *notebookRuntimeFake) RemoveClosingContainer(_ context.Context, _ *domain.DevMachine, _ *domain.DevMachineAgentRun, suffix string) error {
	f.removed = append(f.removed, suffix)
	return nil
}

func (f *notebookRuntimeFake) ClearNotebookOutbox(context.Context, []domain.DevMachineService, *domain.DevMachineCheckout) error {
	f.steps = append(f.steps, "clear")
	return nil
}

func (f *notebookRuntimeFake) WorktreeState(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout) (string, error) {
	f.states++
	tree := strings.Repeat("b", 40)
	if f.states > 1 && f.worktreeChanged {
		tree = strings.Repeat("d", 40)
	}
	return strings.Repeat("a", 40) + " " + tree + " " + strings.Repeat("c", 40), nil
}

func (f *notebookRuntimeFake) WorktreeChangedFiles(context.Context, []domain.DevMachineService, *domain.DevMachineCheckout, string, string) ([]string, error) {
	return []string{"ui/theme.ts"}, nil
}

func (f *notebookRuntimeFake) RunAgent(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, checkout *domain.DevMachineCheckout, secrets map[string]string, observer *AgentObserver) (*AgentExecution, error) {
	f.runs = append(f.runs, *run)
	if f.onRun != nil {
		f.onRun(len(f.runs))
	}
	if f.failCall == len(f.runs) {
		return nil, errors.New("container exited while starting")
	}
	return f.runtimeFake.RunAgent(ctx, machine, run, provider, checkout, secrets, observer)
}

func (f *notebookRuntimeFake) TakeOutbox(context.Context, []domain.DevMachineService) ([]domain.AgentOutboxFile, error) {
	f.steps = append(f.steps, "take")
	return f.outbox, f.takeErr
}

func (f *notebookRuntimeFake) PrepareOutbox(context.Context, []domain.DevMachineService, *domain.DevMachineCheckout) error {
	return f.prepareErr
}

func (f *notebookRuntimeFake) AckOutbox(_ context.Context, _ []domain.DevMachineService, files []domain.AgentOutboxFile) error {
	f.acked = append(f.acked, files...)
	return nil
}

type notebookScenario struct {
	provider     string
	stdout       string
	exit         int
	required     bool
	updates      int
	problem      string
	session      string
	continuation []string
	alreadyAsked bool
	// afterAsk changes what the store reports once the agent was asked.
	afterAsk   func(store *notebookStoreFake)
	takeErr    error
	readOnly   bool
	prepareErr error
	// unchanged: the attempt left a valid update that changed nothing;
	// worktreeChanged: it changed the worktree; checkout: it has one.
	unchanged       bool
	worktreeChanged bool
	failCall        int
	checkout        bool
}

const openCodeConclusion = `{"type":"text","part":{"type":"text","text":"Implemented the toggle and ran the tests."}}`

// notebookRun runs one agent attempt and returns how the manager completed
// it, with the store and the runtime it used.
func notebookRun(t *testing.T, scenario notebookScenario) (*domain.DevMachineAgentRun, *notebookStoreFake, *notebookRuntimeFake) {
	t.Helper()
	machineID, workspaceID, runID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if scenario.provider == "" {
		scenario.provider = "opencode"
	}
	if scenario.stdout == "" {
		scenario.stdout = openCodeConclusion
	}
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID, TaskID: &taskID, ProviderID: scenario.provider, Mode: "autonomous",
		Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30, NotebookRequired: scenario.required, WorktreeReadOnly: scenario.readOnly,
		CommandArgv: json.RawMessage(`["harness","the original prompt"]`),
	}
	if scenario.session != "" {
		run.NativeSessionID = &scenario.session
	}
	if scenario.continuation != nil {
		run.ClosingContinuation, _ = json.Marshal(scenario.continuation)
	}
	// The harnesses run with an API key, so no sign-in is involved.
	run.AllowedSecrets = json.RawMessage(`["OPENAI_API_KEY","ANTHROPIC_API_KEY"]`)
	sealed, err := cryptoutil.Encrypt("fixture-api-key", make([]byte, 32))
	require.NoError(t, err)
	container := "developer"
	store := &notebookStoreFake{updates: scenario.updates, problem: scenario.problem, unchanged: scenario.unchanged, alreadyAsked: scenario.alreadyAsked, managerStoreFake: &managerStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1},
		agentRun: run,
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: scenario.provider, ImageRef: "kuayle/agent:test", Enabled: true},
		services: []domain.DevMachineService{{ServiceType: "ide", Status: "running", ContainerID: &container}},
		envVars:  []domain.DevMachineEnvVar{{Name: "OPENAI_API_KEY", EncryptedValue: sealed}, {Name: "ANTHROPIC_API_KEY", EncryptedValue: sealed}},
	}}
	runtime := &notebookRuntimeFake{takeErr: scenario.takeErr, prepareErr: scenario.prepareErr, failCall: scenario.failCall, worktreeChanged: scenario.worktreeChanged, runtimeFake: &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: scenario.exit, Stdout: scenario.stdout}}}
	runtime.onRun = func(call int) {
		if call == 2 && scenario.afterAsk != nil {
			scenario.afterAsk(store)
		}
	}
	registry := agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/agent:test"), agent.NewClaudeCodeProvider("kuayle/agent:test"), agent.NewCodexProvider("kuayle/agent:test"), agent.NewCustomCLIProvider("kuayle/agent:test"))
	manager := NewManager(store, runtime, registry, nil, make([]byte, 32), make([]byte, 32), "test")
	var checkout *domain.DevMachineCheckout
	if scenario.readOnly || scenario.checkout {
		checkoutID := uuid.New()
		run.CheckoutID = &checkoutID
		checkout = &domain.DevMachineCheckout{ID: checkoutID, MachineID: machineID, WorkspaceID: workspaceID, Status: "ready", WorkspacePath: "/workspace/tasks/eng-9"}
		store.checkout = checkout
	}
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1}))
	require.NotNil(t, store.completedRun)
	return store.completedRun, store, runtime
}

var resumeCommand = []string{"harness", "resume", domain.ClosingSessionPlaceholder, "-p", domain.ClosingPromptPlaceholder}

// An attempt that concluded without a valid notebook update is asked once
// more in its own session; the continuation says exactly what is missing and
// to do nothing else. When the agent then writes it, nothing is flagged.
func TestAttemptWithoutANotebookUpdateIsAskedOnceInTheSameSession(t *testing.T) {
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout:   `{"type":"result","subtype":"success","is_error":false,"result":"Implemented the toggle and ran the tests."}`,
		problem:  `"instructions" is the people's instructions, which an agent cannot write; remove it from the update`,
		afterAsk: func(store *notebookStoreFake) { store.updates, store.problem = 1, "" }})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	require.Len(t, runtime.runs, 2, "the attempt and one continuation")
	require.Equal(t, 1, store.asked)
	continuation := runtime.runs[1]
	require.Equal(t, closingSuffix, continuation.ContainerSuffix, "a second container of the same run")
	require.Equal(t, completed.ID, continuation.ID)
	var argv []string
	require.NoError(t, json.Unmarshal(continuation.CommandArgv, &argv))
	require.Equal(t, []string{"harness", "resume", "0c9a6c1e-session", "-p"}, argv[:4], "it continues the attempt's own session")
	require.Contains(t, argv[4], "cannot write", "the validator's message")
	require.Contains(t, argv[4], ".kuayle/outbox/notebook.json")
	require.Contains(t, argv[4], "Do nothing else")
	require.Empty(t, store.fallbacks, "the agent wrote it when asked")
	// The continuation gets no live inbox, its container is removed by name
	// and the attempt's own container is left alone; that it ran, and for
	// how long, stays on the run.
	require.False(t, agentHasLiveInbox(&continuation))
	require.True(t, agentHasLiveInbox(&runtime.runs[0]))
	require.Equal(t, []string{closingSuffix}, runtime.removed)
	require.Zero(t, runtime.agentCancellations)
	require.NotNil(t, store.closingSeconds)
	asked := false
	for _, event := range store.events {
		asked = asked || event.EventType == "agent_run.closing_continuation"
	}
	require.True(t, asked)
	require.NotContains(t, string(completed.RiskNotes), "notebook")
	require.Equal(t, "Implemented the toggle and ran the tests.", *completed.Summary, "the continuation's reply is not the attempt's summary")
}

// The work is never lost over a missing notebook: when asking did not help,
// or the harness cannot continue a session, Kuayle records the attempt's
// final message itself, the run stays succeeded and carries the note.
func TestKuayleRecordsTheAttemptItselfAndNeverFailsIt(t *testing.T) {
	for name, scenario := range map[string]notebookScenario{
		"still nothing after being asked":          {provider: "codex", required: true, session: "0c9a6c1e-session", continuation: resumeCommand, stdout: `{"type":"item.completed","item":{"type":"agent_message","text":"Implemented the toggle and ran the tests."}}`},
		"a harness that cannot continue a session": {provider: "opencode", required: true},
		"a session Kuayle does not know":           {provider: "codex", required: true, continuation: resumeCommand, stdout: `{"type":"item.completed","item":{"type":"agent_message","text":"Implemented the toggle and ran the tests."}}`},
		"an invalid update and no way to ask":      {provider: "opencode", required: true, problem: "done is not a list of texts"},
		"a valid update followed by a refused one": {provider: "opencode", required: true, updates: 1, problem: "plan is not an object with steps"},
		"already asked before a manager restart":   {provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand, alreadyAsked: true, stdout: `{"type":"result","subtype":"success","is_error":false,"result":"Implemented the toggle and ran the tests."}`},
	} {
		t.Run(name, func(t *testing.T) {
			completed, store, runtime := notebookRun(t, scenario)
			require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
			require.Nil(t, completed.ErrorMessage)
			require.Equal(t, []string{"Implemented the toggle and ran the tests."}, store.fallbacks, "Kuayle records the final message")
			require.Contains(t, string(completed.RiskNotes), "did not update the task notebook")
			require.LessOrEqual(t, len(runtime.runs), 2, "never more than one continuation")
			if name == "still nothing after being asked" {
				require.Len(t, runtime.runs, 2)
			} else {
				require.Len(t, runtime.runs, 1)
			}
		})
	}
}

// The closing continuation's container never outlives it: it is removed by
// its exact name when the continuation fails, and when a restarted manager
// finds the attempt already asked (a crash left it running) it is removed
// and not run again. The attempt's own container is never removed here.
func TestClosingContainerIsAlwaysRemovedAndNeverTheAttemptsOwn(t *testing.T) {
	claude := `{"type":"result","subtype":"success","is_error":false,"result":"Implemented the toggle and ran the tests."}`
	_, _, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claude, failCall: 2})
	require.Len(t, runtime.runs, 2)
	require.Equal(t, []string{closingSuffix}, runtime.removed, "removed although it failed")
	require.Zero(t, runtime.agentCancellations, "the attempt's own container is not cancelled")

	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claude, alreadyAsked: true})
	require.Len(t, runtime.runs, 1, "not run again after a restart")
	require.Equal(t, []string{closingSuffix}, runtime.removed, "what the crash left is removed")
	require.Zero(t, runtime.agentCancellations)
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	require.Len(t, store.fallbacks, 1)

	// Nothing to ask: nothing to remove.
	_, _, runtime = notebookRun(t, notebookScenario{required: true, updates: 1})
	require.Empty(t, runtime.removed)
}

// An update that changes nothing in the notebook is enough only for an
// attempt that changed nothing itself. One that changed the worktree is
// asked like an attempt with no update, and told why.
func TestAnUpdateThatChangesNothingCountsOnlyForAnAttemptThatChangedNothing(t *testing.T) {
	claude := `{"type":"result","subtype":"success","is_error":false,"result":"Implemented the toggle and ran the tests."}`
	idle, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, unchanged: true, checkout: true, session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claude})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, idle.Status)
	require.Len(t, runtime.runs, 1, "nothing changed, nothing to record")
	require.Empty(t, store.fallbacks)
	require.NotContains(t, string(idle.RiskNotes), "notebook")

	worked, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, unchanged: true, checkout: true, worktreeChanged: true, session: "0c9a6c1e-session", continuation: resumeCommand, stdout: claude})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, worked.Status)
	require.Len(t, runtime.runs, 2, "asked like a missing update")
	var argv []string
	require.NoError(t, json.Unmarshal(runtime.runs[1].CommandArgv, &argv))
	require.Contains(t, argv[4], "changed nothing in the notebook")
	require.Len(t, store.fallbacks, 1)
	require.Equal(t, domain.NotebookUnchangedProblem, store.fallbackProblem)
	require.Contains(t, string(worked.RiskNotes), "did not update the task notebook")

	// An update that did change the notebook counts whatever else happened.
	_, store, runtime = notebookRun(t, notebookScenario{required: true, updates: 1, unchanged: true, checkout: true, worktreeChanged: true})
	require.Len(t, runtime.runs, 1)
	require.Empty(t, store.fallbacks)
}

// A notebook file an earlier attempt left in the outbox never counts for the
// attempt that starts: it is collected (and so recorded if valid), removed,
// and the starting attempt's count is reset, before its agent runs.
func TestLeftoverNotebookFilesAreNotCreditedToTheNextAttempt(t *testing.T) {
	_, store, runtime := notebookRun(t, notebookScenario{required: true, updates: 1, checkout: true})
	require.Equal(t, 1, store.resets)
	require.Equal(t, []string{"take", "clear"}, runtime.steps[:2], "recorded first, then removed")
	// A run that needs no notebook leaves the outbox alone.
	_, store, runtime = notebookRun(t, notebookScenario{checkout: true})
	require.Zero(t, store.resets)
	require.NotContains(t, runtime.steps, "clear")

	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell")
	}
	worktree := filepath.Join(t.TempDir(), "eng-9")
	outbox := filepath.Join(worktree, ".kuayle", "outbox")
	require.NoError(t, os.MkdirAll(outbox, 0o755))
	for _, name := range []string{"notebook.json", "notebook-2.json", "1.json"} {
		require.NoError(t, os.WriteFile(filepath.Join(outbox, name), []byte(`{"done":["x"]}`), 0o600))
	}
	output, err := exec.Command(shell, "-c", clearNotebookOutboxScript, "kuayle-outbox", worktree).CombinedOutput()
	require.NoError(t, err, string(output))
	require.NoFileExists(t, filepath.Join(outbox, "notebook.json"))
	require.NoFileExists(t, filepath.Join(outbox, "notebook-2.json"))
	require.FileExists(t, filepath.Join(outbox, "1.json"), "messages are not touched")
}

// A custom, non-LLM harness run as a task cannot write a notebook. It is
// not asked (it has no session) and it does not fail.
func TestCustomHarnessTaskDoesNotFailOverTheNotebook(t *testing.T) {
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "custom", required: true, stdout: "migrated 3 files\n"})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	require.Nil(t, completed.ErrorMessage)
	require.Len(t, runtime.runs, 1)
	require.Zero(t, store.asked)
	require.Len(t, store.fallbacks, 1)
	require.Contains(t, store.fallbacks[0], "migrated 3 files")
}

// Nothing is asked or recorded for an attempt that has its update, needs
// none, or did not conclude: a failed attempt fails for its own reason.
func TestNotebookIsLeftAloneWhenThereIsNothingToSettle(t *testing.T) {
	for name, scenario := range map[string]notebookScenario{
		"a valid update":        {required: true, updates: 1},
		"a run that needs none": {required: false},
	} {
		completed, store, runtime := notebookRun(t, scenario)
		require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status, name)
		require.Len(t, runtime.runs, 1, name)
		require.Empty(t, store.fallbacks, name)
		require.NotContains(t, string(completed.RiskNotes), "notebook", name)
	}
	failed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, exit: 1, session: "0c9a6c1e-session", continuation: resumeCommand})
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, failed.Status)
	require.Len(t, runtime.runs, 1, "a failed attempt is not asked")
	require.Empty(t, store.fallbacks)
}

// A collection that failed is Kuayle's failure, not the agent's omission:
// the agent is not asked again, nothing is recorded in its name, the run
// succeeds and says that Kuayle could not collect the update.
func TestAFailedCollectionIsNotBlamedOnTheAgent(t *testing.T) {
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout:  `{"type":"result","subtype":"success","is_error":false,"result":"Implemented the toggle and ran the tests."}`,
		takeErr: errors.New("exec failed: container is restarting")})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	require.Len(t, runtime.runs, 1)
	require.Zero(t, store.asked)
	require.Empty(t, store.fallbacks)
	require.Equal(t, 1, store.notCollected)
	require.Contains(t, string(completed.RiskNotes), "Kuayle could not collect")
	require.NotContains(t, string(completed.RiskNotes), "did not update")
}

// An update is removed from the outbox only once it is recorded: a failed
// record leaves it there for the next collection, which records it once.
func TestNotebookUpdateStaysInTheOutboxUntilItIsRecorded(t *testing.T) {
	container := "developer"
	services := []domain.DevMachineService{{ServiceType: "ide", Status: "running", ContainerID: &container}}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	update := domain.AgentOutboxFile{Worktree: "/workspace/tasks/eng-9", Name: "notebook.json", Data: []byte(`{"done":["x"]}`), Ack: "eyJkb25lIjpbIngiXX0="}
	store := &notebookStoreFake{managerStoreFake: &managerStoreFake{machine: machine}, recordErr: errors.New("database is down")}
	runtime := &notebookRuntimeFake{runtimeFake: &runtimeFake{}, outbox: []domain.AgentOutboxFile{update, {Worktree: "/workspace/tasks/eng-9", Name: "1.json", Data: []byte(`{}`)}}}
	manager := &Manager{store: store, runtime: runtime, encryptionKey: make([]byte, 32)}
	require.Error(t, manager.collectOutbox(context.Background(), machine, services))
	require.Empty(t, runtime.acked, "not recorded, so not removed")

	store.recordErr = nil
	require.NoError(t, manager.collectOutbox(context.Background(), machine, services))
	require.Len(t, store.recorded, 2)
	require.Len(t, runtime.acked, 1, "only the notebook update needs removing; messages were taken")
	require.Equal(t, "notebook.json", runtime.acked[0].Name)

	// An update that cannot be checked for secrets is held back, not lost.
	store.recorded, runtime.acked, store.listErr = nil, nil, errors.New("database is down")
	require.Error(t, manager.collectOutbox(context.Background(), machine, services))
	require.Len(t, store.recorded, 1, "the message is still recorded")
	require.Equal(t, "1.json", store.recorded[0].Name)
	require.Empty(t, runtime.acked)
}

// The take script always reads notebook.json, however many other files the
// outbox holds, and leaves it in place; the acknowledgement removes exactly
// the content that was recorded.
func TestOutboxScriptsAlwaysTakeTheNotebookAndRemoveItOnlyWhenRecorded(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell")
	}
	root := t.TempDir()
	outbox := filepath.Join(root, "eng-9", ".kuayle", "outbox")
	require.NoError(t, os.MkdirAll(outbox, 0o755))
	for i := range 14 {
		require.NoError(t, os.WriteFile(filepath.Join(outbox, fmt.Sprintf("a%02d.json", i)), []byte(`{"to":"ENG-2","body":"x"}`), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(outbox, "notebook.json"), []byte(`{"done":["x"]}`), 0o600))
	output, err := exec.Command(shell, "-c", outboxTakeScript, "kuayle-outbox", root).CombinedOutput()
	require.NoError(t, err, string(output))
	files := parseOutboxTake(output, root)
	var update *domain.AgentOutboxFile
	messages := 0
	for i := range files {
		if files[i].Name == "notebook.json" {
			update = &files[i]
		} else {
			messages++
		}
	}
	require.Equal(t, outboxFilesPerWorktree, messages)
	require.NotNil(t, update, "more than ten other files do not hide the notebook update")
	require.Equal(t, `{"done":["x"]}`, string(update.Data))
	require.NotEmpty(t, update.Ack)
	require.FileExists(t, filepath.Join(outbox, "notebook.json"), "it stays until it is recorded")

	// The agent rewrote it meanwhile: the stale acknowledgement removes nothing.
	require.NoError(t, os.WriteFile(filepath.Join(outbox, "notebook.json"), []byte(`{"done":["x","y"]}`), 0o600))
	ack := func(token string) {
		out, ackErr := exec.Command(shell, "-c", outboxAckScript, "kuayle-outbox", filepath.Join(root, "eng-9"), "notebook.json", token).CombinedOutput()
		require.NoError(t, ackErr, string(out))
	}
	ack(update.Ack)
	require.FileExists(t, filepath.Join(outbox, "notebook.json"))
	output, err = exec.Command(shell, "-c", outboxTakeScript, "kuayle-outbox", root).CombinedOutput()
	require.NoError(t, err, string(output))
	for _, file := range parseOutboxTake(output, root) {
		if file.Name == "notebook.json" {
			ack(file.Ack)
		}
	}
	require.NoFileExists(t, filepath.Join(outbox, "notebook.json"))
}

// When the outbox of a planning step cannot be prepared, the read-only
// mount would keep the container from starting: the step runs with a
// writable worktree instead, and the comparison after the run guards it.
func TestPlanningStepFallsBackToAWritableWorktreeWhenItsOutboxCannotBePrepared(t *testing.T) {
	_, store, runtime := notebookRun(t, notebookScenario{readOnly: true, prepareErr: errors.New("exec failed")})
	require.False(t, runtime.runs[0].WorktreeReadOnly)
	require.Equal(t, 1, store.clearedReadOnly)
	_, store, runtime = notebookRun(t, notebookScenario{readOnly: true})
	require.True(t, runtime.runs[0].WorktreeReadOnly)
	require.Zero(t, store.clearedReadOnly)
}

// Notebook updates are redacted like run output before they are recorded:
// the notebook is given to later agents and shown to people.
func TestNotebookUpdatesAreRedactedBeforeTheyAreRecorded(t *testing.T) {
	key := make([]byte, 32)
	payload, err := json.Marshal(domain.AgentRunSecretSnapshot{Version: 1, Environment: map[string]string{"GITHUB_TOKEN": "ghp_s3cr3t\"quoted"}, Redactions: map[string]string{"GITHUB_TOKEN": "ghp_s3cr3t\"quoted"}})
	require.NoError(t, err)
	sealed, err := cryptoutil.Encrypt(string(payload), key)
	require.NoError(t, err)
	store := &notebookStoreFake{managerStoreFake: &managerStoreFake{}, snapshots: []string{sealed}}
	manager := &Manager{store: store, encryptionKey: key}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	update, err := json.Marshal(map[string]any{"done": []string{"Pushed with ghp_s3cr3t\"quoted to origin"}})
	require.NoError(t, err)
	files, err := manager.redactNotebookFiles(context.Background(), machine, []domain.AgentOutboxFile{
		{Name: "notebook.json", Data: update},
		{Name: "1.json", Data: []byte(`{"to":"ENG-2","body":"ghp_s3cr3t"}`)},
		{Name: "notebook-2.json", Data: []byte("not json ghp_s3cr3t\"quoted")},
	})
	require.NoError(t, err)
	require.NotContains(t, string(files[0].Data), "ghp_s3cr3t")
	parsed, _, err := domain.ParseNotebookUpdate(files[0].Data, false)
	require.NoError(t, err, "a redacted update is still valid JSON")
	require.Contains(t, parsed.Done[0], "Pushed with")
	require.Contains(t, string(files[1].Data), "ghp_s3cr3t", "messages keep their own handling")
	require.NotContains(t, string(files[2].Data), "ghp_s3cr3t")
}

// A planning step's agent container mounts its worktree read-only and only
// the worktree's .kuayle directory (the outbox) writable; a step that
// changes code gets no such mount. The spec is the same for every harness:
// which harnesses ask for it is domain.ReadOnlyWorktreeSupported.
func TestPlanningStepMountsItsWorktreeReadOnlyWithAWritableOutbox(t *testing.T) {
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/eng-9-0f0e"}
	for _, harness := range []string{"claude-code", "codex", "opencode"} {
		planning := readOnlyWorktreeMounts("workspace-volume", &domain.DevMachineAgentRun{ProviderID: harness, Mode: "autonomous", WorktreeReadOnly: true}, checkout)
		require.Len(t, planning, 2, harness)
		worktree, outbox := planning[0], planning[1]
		require.Equal(t, mount.TypeVolume, worktree.Type)
		require.Equal(t, "workspace-volume", worktree.Source)
		require.Equal(t, "/workspace/tasks/eng-9-0f0e", worktree.Target)
		require.True(t, worktree.ReadOnly)
		require.Equal(t, "tasks/eng-9-0f0e", worktree.VolumeOptions.Subpath)
		require.True(t, worktree.VolumeOptions.NoCopy)
		require.Equal(t, "/workspace/tasks/eng-9-0f0e/.kuayle", outbox.Target)
		require.False(t, outbox.ReadOnly, "the outbox stays writable")
		require.Equal(t, "tasks/eng-9-0f0e/.kuayle", outbox.VolumeOptions.Subpath)

		require.Empty(t, readOnlyWorktreeMounts("workspace-volume", &domain.DevMachineAgentRun{ProviderID: harness, Mode: "autonomous"}, checkout), "a step that changes code")
	}
	readOnly := &domain.DevMachineAgentRun{Mode: "autonomous", WorktreeReadOnly: true}
	require.Empty(t, readOnlyWorktreeMounts("workspace-volume", readOnly, nil))
	for _, path := range []string{"/workspace", "/workspace/tasks", "/workspace/tasks/", "/workspace/tasks/../repos", "/workspace/tasks/a/b", "/workspace/tasks/.hidden", "/elsewhere/tasks/a"} {
		require.Empty(t, readOnlyWorktreeMounts("workspace-volume", readOnly, &domain.DevMachineCheckout{WorkspacePath: path}), path)
	}
	require.Empty(t, readOnlyWorktreeMounts("workspace-volume", &domain.DevMachineAgentRun{Mode: "interactive", WorktreeReadOnly: true}, checkout))

	// The workspace volume itself stays writable for the agent: its session
	// directory and run directory live there, outside the worktree.
	runtime := &DockerRuntime{}
	host := runtime.secureHostConfig(&domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512}, "agent", "machine-network", "workspace-volume", false)
	require.Equal(t, "/workspace", host.Mounts[0].Target)
	require.False(t, host.Mounts[0].ReadOnly)
	require.Contains(t, host.Tmpfs, "/tmp")
	require.Contains(t, host.Tmpfs, "/home/kuayle")
}

func TestVolumeSubpathNeedsDockerAPI145(t *testing.T) {
	require.True(t, apiAtLeast("1.45", volumeSubpathAPI))
	require.True(t, apiAtLeast("1.54", volumeSubpathAPI))
	require.True(t, apiAtLeast("2.0", volumeSubpathAPI))
	require.False(t, apiAtLeast("1.44", volumeSubpathAPI))
	require.False(t, apiAtLeast("1.9", volumeSubpathAPI))
	require.False(t, apiAtLeast("", volumeSubpathAPI))
}
