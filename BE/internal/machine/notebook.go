package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"
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
//     continue one: the closing continuation (closing_continuation.go).
//  2. If there is still no valid update, Kuayle records the attempt's final
//     message itself, marked as its own, and notes on the run that the agent
//     did not update the notebook. The run stays succeeded.
//
// A collection that failed for a reason of Kuayle's (the outbox could not be
// read, recorded or checked for secrets) is not the agent's omission: the
// agent is not asked again and nothing is recorded in its name; the update
// stays in the outbox for the next collection.
//
// An update counts for the attempt that wrote it: a file an earlier attempt
// left behind is recorded and removed when the next one starts, and an
// update that changes nothing counts only for an attempt that changed
// nothing itself.

// notebookSecretStore lists the sealed credential snapshots of a machine's
// attempts, for redaction.
type notebookSecretStore interface {
	ListAgentRunSecretSnapshots(ctx context.Context, workspaceID, machineID uuid.UUID) ([]string, error)
}

// notebookStore reports and settles the notebook updates of an attempt.
type notebookStore interface {
	NotebookUpdateState(ctx context.Context, runID uuid.UUID) (domain.NotebookRunState, error)
	RecordNotebookFallback(ctx context.Context, runID uuid.UUID, summary, problem string) (string, error)
	NoteNotebookNotCollected(ctx context.Context, runID uuid.UUID) error
	ResetNotebookState(ctx context.Context, runID uuid.UUID) error
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

// notebookMissing reports whether the attempt still lacks a notebook update
// that counts, and why. An update that changed the notebook counts. One
// that changed nothing counts only when the attempt itself changed nothing
// (unchangedRun): otherwise there is something to record and it did not.
func (m *Manager) notebookMissing(ctx context.Context, run *domain.DevMachineAgentRun, unchangedRun func() bool) (missing bool, problem string, err error) {
	store, ok := m.store.(notebookStore)
	if !ok || !run.NotebookRequired {
		return false, "", nil
	}
	state, err := store.NotebookUpdateState(ctx, run.ID)
	switch {
	case err != nil:
		return false, "", err
	case state.Problem != "":
		return true, state.Problem, nil
	case state.Updates > 0:
		return false, "", nil
	case state.Unchanged && unchangedRun != nil && unchangedRun():
		return false, "", nil
	case state.Unchanged:
		return true, domain.NotebookUnchangedProblem, nil
	}
	return true, "", nil
}

// notebookAsk runs after the outbox of a finished attempt was collected
// (collectErr is that collection's error). It returns what the closing
// continuation should ask for when the attempt concluded but left no update
// that counts, and reports whether Kuayle failed to collect, in which case
// the missing update is not the agent's and nothing is asked.
func (m *Manager) notebookAsk(ctx context.Context, run *domain.DevMachineAgentRun, collectErr error, concluded bool, unchangedRun func() bool) (ask *domain.ClosingAsk, notCollected bool) {
	if _, ok := m.store.(notebookStore); !ok || !run.NotebookRequired {
		return nil, false
	}
	if collectErr != nil {
		return nil, true
	}
	missing, problem, err := m.notebookMissing(ctx, run, unchangedRun)
	if err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("the attempt's notebook updates could not be read")
		return nil, true
	}
	if !missing || !concluded {
		return nil, false
	}
	asked := domain.NotebookClosingAsk(problem)
	return &asked, false
}

// closeNotebook settles the notebook of an attempt that succeeded. With an
// update that counts there is nothing to do. Without one, Kuayle records the
// attempt's final message itself and returns the note that says so; when
// Kuayle failed to collect the outbox it returns that instead and records
// nothing in the agent's name. The attempt never fails here.
func (m *Manager) closeNotebook(ctx context.Context, run *domain.DevMachineAgentRun, summary string, notCollected bool, unchangedRun func() bool) string {
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
	missing, problem, err := m.notebookMissing(ctx, run, unchangedRun)
	if err != nil || !missing {
		return ""
	}
	note, err := store.RecordNotebookFallback(ctx, run.ID, summary, problem)
	if err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("Kuayle could not record the attempt in the task notebook")
		return ""
	}
	return note
}

// clearNotebookLeftovers runs when an attempt starts for the first time: a
// notebook file an earlier attempt left in the outbox is recorded if it is
// valid (the collection does that), removed either way, and never credited
// to the attempt that is starting.
func (m *Manager) clearNotebookLeftovers(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) {
	store, ok := m.store.(notebookStore)
	if !ok || !run.NotebookRequired || checkout == nil {
		return
	}
	_ = m.collectOutbox(ctx, machine, services)
	if runtime, ok := m.runtime.(notebookOutboxRuntime); ok {
		if err := runtime.ClearNotebookOutbox(ctx, services, checkout); err != nil {
			log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("leftover notebook updates could not be removed")
		}
	}
	if err := store.ResetNotebookState(ctx, run.ID); err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("the attempt's notebook state could not be reset")
	}
}

// notebookOutboxRuntime removes the notebook files of a worktree's outbox.
type notebookOutboxRuntime interface {
	ClearNotebookOutbox(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) error
}

// clearNotebookOutboxScript removes the notebook updates of worktree $1's
// outbox, never through symlinks.
const clearNotebookOutboxScript = `set -u
[ -d "$1/.kuayle" ] && [ ! -L "$1/.kuayle" ] && [ -d "$1/.kuayle/outbox" ] && [ ! -L "$1/.kuayle/outbox" ] || exit 0
rm -f "$1/.kuayle/outbox/notebook.json" "$1/.kuayle/outbox"/notebook-*.json`

// ClearNotebookOutbox removes the notebook updates left in a task worktree.
func (r *DockerRuntime) ClearNotebookOutbox(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) error {
	developer := developerContainerID(services)
	if developer == "" || checkout == nil || !strings.HasPrefix(checkout.WorkspacePath, taskWorktreeRoot+"/") {
		return nil
	}
	_, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", clearNotebookOutboxScript, "kuayle-outbox", checkout.WorkspacePath})
	return err
}

var _ notebookOutboxRuntime = (*DockerRuntime)(nil)

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
