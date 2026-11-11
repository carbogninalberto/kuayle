package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/moby/moby/api/types/mount"
	log "github.com/sirupsen/logrus"
)

// An agent updates its task's notebook by writing
// <worktree>/.kuayle/outbox/notebook.json (see the kuayle-agents skill). The
// file travels with the other outbox files; before it is recorded its
// strings are redacted with the credentials of the machine's attempts,
// exactly as run output is, because the notebook is later given to other
// agents and shown to people.
//
// When an attempt that should update the notebook concludes without a valid
// update, the work is never lost over it:
//
//  1. Kuayle asks once more, in the same session, where the harness can
//     continue one (Claude Code with --resume, Codex with "exec resume"): a
//     second container of the run with a short prompt that says what is
//     missing and to write the file and nothing else. One continuation per
//     attempt, also across a manager restart.
//  2. If there is still no valid update, Kuayle records the attempt's final
//     message itself, marked as its own, and notes on the run that the agent
//     did not update the notebook. The run stays succeeded.
//
// A collection that failed for a reason of Kuayle's (the outbox could not be
// read, recorded or checked for secrets) is not the agent's omission: the
// agent is not asked again and nothing is recorded in its name; the update
// stays in the outbox for the next collection.

// notebookAskTimeout bounds the one continuation.
const notebookAskTimeout = 5 * time.Minute

// notebookSecretStore lists the sealed credential snapshots of a machine's
// attempts, for redaction.
type notebookSecretStore interface {
	ListAgentRunSecretSnapshots(ctx context.Context, workspaceID, machineID uuid.UUID) ([]string, error)
}

// notebookStore reports and settles the notebook updates of an attempt.
type notebookStore interface {
	NotebookUpdateState(ctx context.Context, runID uuid.UUID) (int, string, error)
	MarkNotebookAsked(ctx context.Context, runID uuid.UUID) (bool, error)
	RecordNotebookFallback(ctx context.Context, runID uuid.UUID, summary string) (string, error)
	NoteNotebookNotCollected(ctx context.Context, runID uuid.UUID) error
}

// redactNotebookFiles redacts the string values of the notebook updates
// among the outbox files. When they cannot be checked for secrets the
// updates are held back (left in their outbox, not recorded) and the error
// says so; the other files are returned as they are.
func (m *Manager) redactNotebookFiles(ctx context.Context, machine *domain.DevMachine, files []domain.AgentOutboxFile) ([]domain.AgentOutboxFile, error) {
	updates := false
	for _, file := range files {
		updates = updates || domain.IsNotebookOutboxFile(file.Name)
	}
	if !updates {
		return files, nil
	}
	values, err := m.machineRedactions(ctx, machine)
	if err != nil {
		kept := files[:0:0]
		for _, file := range files {
			if !domain.IsNotebookOutboxFile(file.Name) {
				kept = append(kept, file)
			}
		}
		return kept, fmt.Errorf("notebook updates could not be checked for secrets and were left in the outbox: %w", err)
	}
	replacer := sanitize.SecretReplacer(values)
	for i := range files {
		if domain.IsNotebookOutboxFile(files[i].Name) && !files[i].Oversize {
			files[i].Data = redactJSONStrings(files[i].Data, replacer)
		}
	}
	return files, nil
}

// machineRedactions are the credential values of every attempt of the
// machine, from their sealed snapshots.
func (m *Manager) machineRedactions(ctx context.Context, machine *domain.DevMachine) ([]string, error) {
	store, ok := m.store.(notebookSecretStore)
	if !ok {
		return nil, nil
	}
	snapshots, err := store.ListAgentRunSecretSnapshots(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return nil, err
	}
	var values []string
	for _, sealed := range snapshots {
		payload, err := cryptoutil.Decrypt(sealed, m.encryptionKey)
		if err != nil {
			return nil, err
		}
		snapshot, err := domain.DecodeAgentRunSecretSnapshot(payload)
		if err != nil {
			return nil, err
		}
		for _, value := range snapshot.Redactions {
			values = append(values, value)
		}
	}
	return values, nil
}

// redactJSONStrings redacts every string (keys included) of a JSON document
// and returns it re-encoded. Data that is not JSON is redacted as text; the
// schema check refuses it later either way.
func redactJSONStrings(data []byte, replacer *strings.Replacer) []byte {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return []byte(replacer.Replace(string(data)))
	}
	encoded, err := json.Marshal(mapEventStrings(value, replacer.Replace))
	if err != nil {
		return []byte(replacer.Replace(string(data)))
	}
	return encoded
}

// notebookMissing reports whether the attempt still lacks a valid notebook
// update, and why its last one was refused.
func (m *Manager) notebookMissing(ctx context.Context, run *domain.DevMachineAgentRun) (missing bool, problem string, err error) {
	store, ok := m.store.(notebookStore)
	if !ok || !run.NotebookRequired {
		return false, "", nil
	}
	updates, problem, err := store.NotebookUpdateState(ctx, run.ID)
	if err != nil {
		return false, "", err
	}
	return updates == 0 || problem != "", problem, nil
}

// settleNotebook runs after the outbox of a finished attempt was collected
// (collectErr is that collection's error). When the attempt concluded but
// left no valid update, it asks once more in the same session and collects
// again. It reports whether Kuayle failed to collect, in which case the
// missing update is not the agent's.
func (m *Manager) settleNotebook(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, checkout *domain.DevMachineCheckout, secrets *domain.AgentRunSecretSnapshot, services []domain.DevMachineService, collectErr error, concluded bool) bool {
	if _, ok := m.store.(notebookStore); !ok || !run.NotebookRequired {
		return false
	}
	if collectErr != nil {
		return true
	}
	missing, problem, err := m.notebookMissing(ctx, run)
	if err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("the attempt's notebook updates could not be read")
		return true
	}
	if !missing || !concluded || !m.askForNotebook(ctx, machine, run, provider, checkout, secrets, problem) {
		return false
	}
	collectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return m.collectOutbox(collectCtx, machine, services) != nil
}

// askForNotebook continues the attempt's own session once, with a prompt
// that says what is missing and to write the notebook update and nothing
// else. It reports whether the continuation ran. Harnesses whose session
// Kuayle cannot continue have no continuation command and are not asked.
func (m *Manager) askForNotebook(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, checkout *domain.DevMachineCheckout, secrets *domain.AgentRunSecretSnapshot, problem string) bool {
	store, ok := m.store.(notebookStore)
	if !ok || run.Mode == "interactive" || len(run.NotebookContinuation) == 0 || secrets == nil {
		return false
	}
	current, err := m.store.GetAgentRunInternal(ctx, run.ID)
	if err != nil || current == nil || current.NativeSessionID == nil || !agent.ValidSessionID(*current.NativeSessionID) || current.Status == domain.DevMachineAgentRunStatusCancelled {
		return false
	}
	var argv []string
	if json.Unmarshal(run.NotebookContinuation, &argv) != nil {
		return false
	}
	session, prompt := false, false
	for i, argument := range argv {
		switch argument {
		case domain.NotebookSessionPlaceholder:
			argv[i], session = *current.NativeSessionID, true
		case domain.NotebookPromptPlaceholder:
			argv[i], prompt = domain.NotebookReminder(problem), true
		}
	}
	command, err := json.Marshal(argv)
	if err != nil || !session || !prompt {
		return false
	}
	// The sign-in the attempt may have refreshed is read again, so the
	// continuation never presents a rotated credential.
	environment := make(map[string]string, len(secrets.Environment))
	for name, value := range secrets.Environment {
		environment[name] = value
	}
	used := environment[signInSecretNames[run.ProviderID]]
	if used != "" {
		if err := m.agentSignInSecrets(ctx, run, environment, map[string]string{}); err != nil {
			return false
		}
		used = environment[signInSecretNames[run.ProviderID]]
	}
	// One continuation per attempt: the mark is taken before it starts.
	if first, err := store.MarkNotebookAsked(ctx, run.ID); err != nil || !first {
		return false
	}
	continuation := *run
	continuation.CommandArgv, continuation.ContainerSuffix = command, "-notebook"
	askCtx, cancel := context.WithTimeout(ctx, notebookAskTimeout)
	observer := &AgentObserver{
		Authorize: func() error { return m.store.AuthorizeAgentRun(askCtx, run.ID) },
		// The continuation's output is not the attempt's: its summary and
		// logs stay those of the work itself.
		Output: func(string, []byte) error { return nil },
	}
	_, runErr := m.runtime.RunAgent(askCtx, machine, &continuation, provider, checkout, environment, observer)
	timedOut := errors.Is(askCtx.Err(), context.DeadlineExceeded)
	cancel()
	if runErr != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(runErr).Warn("the notebook continuation did not finish")
		if timedOut {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			_ = m.runtime.CancelAgent(cleanupCtx, run)
			cancelCleanup()
		}
	}
	if used != "" {
		returnCtx, cancelReturn := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if err := m.returnSignIn(returnCtx, machine, run, used); err != nil {
			log.WithField("agent_run_id", run.ID).WithError(err).Warn("refreshed agent sign-in could not be kept")
		}
		cancelReturn()
	}
	_ = m.createEvent(ctx, machine, &run.ID, "agent", "agent_notebook.asked", map[string]any{"provider_id": run.ProviderID, "finished": runErr == nil})
	return true
}

// closeNotebook settles the notebook of an attempt that succeeded. With a
// valid update there is nothing to do. Without one, Kuayle records the
// attempt's final message itself and returns the note that says so; when
// Kuayle failed to collect the outbox it returns that instead and records
// nothing in the agent's name. The attempt never fails here.
func (m *Manager) closeNotebook(ctx context.Context, run *domain.DevMachineAgentRun, summary string, notCollected bool) string {
	store, ok := m.store.(notebookStore)
	if !ok || !run.NotebookRequired {
		return ""
	}
	if notCollected {
		if err := store.NoteNotebookNotCollected(ctx, run.ID); err != nil {
			log.WithField("agent_run_id", run.ID).WithError(err).Warn("the uncollected notebook update could not be noted")
		}
		return domain.NotebookNotCollected
	}
	if missing, _, err := m.notebookMissing(ctx, run); err != nil || !missing {
		return ""
	}
	note, err := store.RecordNotebookFallback(ctx, run.ID, summary)
	if err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("Kuayle could not record the attempt in the task notebook")
		return ""
	}
	return note
}

// A planning step cannot change code: its agent container mounts the task's
// worktree read-only over the writable workspace volume, so the kernel
// refuses every write, whatever the prompt said. Only the worktree's .kuayle
// directory (the outbox, where the agent leaves messages and its notebook
// update) is mounted writable on top of it. The harness's session directory,
// its home, /tmp and the run directory are outside the worktree and stay
// writable. Git's index and refs of a worktree live in the repository's
// common directory, outside this mount, so a commit or a staged change is
// still possible: the comparison after the run (worktree_state.go) remains
// the second guard, and the only one where this mount is unavailable.

// volumeSubpathAPI is the Docker API version that mounts a volume's subpath.
const volumeSubpathAPI = "1.45"

// apiAtLeast compares Docker API versions ("1.45").
func apiAtLeast(version, minimum string) bool {
	parse := func(value string) (int, int, bool) {
		major, minor, ok := strings.Cut(strings.TrimSpace(value), ".")
		a, errA := strconv.Atoi(major)
		b, errB := strconv.Atoi(minor)
		return a, b, ok && errA == nil && errB == nil
	}
	major, minor, ok := parse(version)
	wantMajor, wantMinor, _ := parse(minimum)
	return ok && (major > wantMajor || major == wantMajor && minor >= wantMinor)
}

// readOnlyWorktreeMounts are the mounts that make the run's worktree
// read-only and keep its .kuayle directory writable, or nil when the run
// does not ask for it or its checkout is not a task worktree.
func readOnlyWorktreeMounts(volumeName string, run *domain.DevMachineAgentRun, checkout *domain.DevMachineCheckout) []mount.Mount {
	if run == nil || !run.WorktreeReadOnly || run.Mode == "interactive" || checkout == nil {
		return nil
	}
	worktree := checkout.WorkspacePath
	name, ok := strings.CutPrefix(worktree, taskWorktreeRoot+"/")
	if !ok || name == "" || worktree != path.Clean(worktree) || strings.ContainsAny(name, "/\x00") || strings.HasPrefix(name, ".") {
		return nil
	}
	subpath := strings.TrimPrefix(worktree, "/workspace/")
	volume := func(sub, target string, readOnly bool) mount.Mount {
		return mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: target, ReadOnly: readOnly,
			VolumeOptions: &mount.VolumeOptions{NoCopy: true, Subpath: sub}}
	}
	return []mount.Mount{
		volume(subpath, worktree, true),
		volume(subpath+"/.kuayle", worktree+"/.kuayle", false),
	}
}
