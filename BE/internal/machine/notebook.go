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
// agents and shown to people. When an attempt that must update the notebook
// ends, the manager checks that it left a valid update: the harness cannot
// finish a step without one, whatever its prompt said.

// notebookSecretStore lists the sealed credential snapshots of a machine's
// attempts, for redaction.
type notebookSecretStore interface {
	ListAgentRunSecretSnapshots(ctx context.Context, workspaceID, machineID uuid.UUID) ([]string, error)
}

// notebookStore reports the notebook updates an attempt left.
type notebookStore interface {
	NotebookUpdateState(ctx context.Context, runID uuid.UUID) (int, string, error)
}

// redactNotebookFiles redacts the string values of the notebook updates
// among the outbox files. An update that cannot be checked for secrets is
// not recorded: it keeps its name, loses its data and says why.
func (m *Manager) redactNotebookFiles(ctx context.Context, machine *domain.DevMachine, files []domain.AgentOutboxFile) []domain.AgentOutboxFile {
	var replacer *strings.Replacer
	failed := ""
	for i := range files {
		file := &files[i]
		if !domain.IsNotebookOutboxFile(file.Name) || file.Oversize {
			continue
		}
		if replacer == nil && failed == "" {
			values, err := m.machineRedactions(ctx, machine)
			if err != nil {
				failed = "Kuayle could not check the update for secrets, so it was not recorded; write it again"
				log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "agent_notebook.redact"}).WithError(err).Warn("notebook update could not be redacted")
			} else {
				replacer = sanitize.SecretReplacer(values)
			}
		}
		if failed != "" {
			file.Data, file.Unreadable = nil, failed
			continue
		}
		file.Data = redactJSONStrings(file.Data, replacer)
	}
	return files
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

// missingNotebookUpdate reports why a finished attempt that must update the
// task notebook has not: the returned reason is the run's error. It is empty
// when the attempt left a valid update last, or does not have to.
func (m *Manager) missingNotebookUpdate(ctx context.Context, run *domain.DevMachineAgentRun) (string, error) {
	store, ok := m.store.(notebookStore)
	if !ok || !run.NotebookRequired {
		return "", nil
	}
	updates, problem, err := store.NotebookUpdateState(ctx, run.ID)
	if err != nil {
		return "", fmt.Errorf("agent_notebook_state_failed: %w", err)
	}
	if updates > 0 && problem == "" {
		return "", nil
	}
	return domain.NotebookUpdateReason(problem), nil
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
