package machine

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// Agents message the agents of related tasks by leaving JSON files in their
// worktree's .kuayle/outbox/ (see the kuayle-agents skill). The manager takes
// them during the port scan and when an attempt ends; the API validates and
// records them. Files are untrusted input: regular files only, bounded in
// count and size, and removed once taken.
const (
	outboxFilesPerWorktree = 10
	outboxFilesPerScan     = 50
)

// outboxRuntime is implemented by runtimes that can reach task worktrees.
type outboxRuntime interface {
	TakeOutbox(ctx context.Context, services []domain.DevMachineService) ([]domain.AgentOutboxFile, error)
	PrepareOutbox(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) error
}

type outboxStore interface {
	RecordAgentOutbox(ctx context.Context, machineID uuid.UUID, files []domain.AgentOutboxFile) (int, error)
}

// excludeKuayleScript adds /.kuayle/ once to the repository's info/exclude
// (shared by its worktrees), never to a tracked .gitignore, so Kuayle's files
// in a worktree cannot be committed or published.
const excludeKuayleScript = `set -eu
root=$1
cd "$root"
exclude=$(git rev-parse --git-path info/exclude)
case "$exclude" in /*) ;; *) exclude="$root/$exclude" ;; esac
mkdir -p "$(dirname "$exclude")"
grep -qxF '/.kuayle/' "$exclude" 2>/dev/null || printf '\n/.kuayle/\n' >> "$exclude"
`

// prepareOutboxScript excludes .kuayle/ and creates .kuayle/outbox/,
// replacing symlinks with directories, and removes an earlier attempt's
// validation file so each attempt declares its own tests.
const prepareOutboxScript = excludeKuayleScript + `[ ! -L "$root/.kuayle" ] || rm -f "$root/.kuayle"
mkdir -p "$root/.kuayle"
[ ! -L "$root/.kuayle/outbox" ] || rm -f "$root/.kuayle/outbox"
mkdir -p "$root/.kuayle/outbox"
rm -f "$root/.kuayle/validation.json"`

// outboxTakeScript prints "M <worktree> <file> <base64>" for each regular
// *.json file in <root>/<worktree>/.kuayle/outbox/ (names of letters, digits,
// dots, dashes and underscores; never through symlinks), at most ten per
// worktree and fifty per scan, each cut at one byte over the limit, and
// removes what it took. Other entries are removed too, except hidden ones,
// so an agent can write a hidden file and rename it into place. The
// container's sh is dash.
const outboxTakeScript = `set -u
root=$1
[ -d "$root" ] && [ ! -L "$root" ] || exit 0
total=0
for wt in "$root"/*; do
  [ -d "$wt" ] && [ ! -L "$wt" ] || continue
  task=${wt##*/}
  case "$task" in ''|.*|*[!A-Za-z0-9._-]*) continue ;; esac
  dir="$wt/.kuayle/outbox"
  [ -d "$wt/.kuayle" ] && [ ! -L "$wt/.kuayle" ] && [ -d "$dir" ] && [ ! -L "$dir" ] || continue
  n=0
  for f in "$dir"/*; do
    [ -e "$f" ] || [ -L "$f" ] || continue
    name=${f##*/}
    valid=1
    if [ -L "$f" ] || [ ! -f "$f" ]; then valid=0; fi
    case "$name" in *[!A-Za-z0-9._-]*) valid=0 ;; *.json) ;; *) valid=0 ;; esac
    if [ "$valid" -eq 0 ]; then
      rm -f "$f" 2>/dev/null || true
      continue
    fi
    [ "$n" -lt 10 ] && [ "$total" -lt 50 ] || continue
    printf 'M %s %s %s\n' "$task" "$name" "$(head -c 16385 "$f" | base64 | tr -d '\n')"
    rm -f "$f"
    n=$((n + 1))
    total=$((total + 1))
  done
done
exit 0`

// parseOutboxTake reads the take script's output for worktrees under root.
func parseOutboxTake(output []byte, root string) []domain.AgentOutboxFile {
	files := []domain.AgentOutboxFile{}
	for _, line := range bytes.Split(output, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) < 3 || len(fields) > 4 || fields[0] != "M" || len(files) >= outboxFilesPerScan {
			continue
		}
		var data []byte
		if len(fields) == 4 {
			decoded, err := base64.StdEncoding.DecodeString(fields[3])
			if err != nil {
				continue
			}
			data = decoded
		}
		file := domain.AgentOutboxFile{Worktree: strings.TrimRight(root, "/") + "/" + fields[1], Name: fields[2], Data: data}
		if len(file.Data) > domain.AgentOutboxFileMaxBytes {
			file.Data, file.Oversize = file.Data[:domain.AgentOutboxFileMaxBytes], true
		}
		files = append(files, file)
	}
	return files
}

// TakeOutbox takes the agents' outbox files from every task worktree, as the
// developer container's user.
func (r *DockerRuntime) TakeOutbox(ctx context.Context, services []domain.DevMachineService) ([]domain.AgentOutboxFile, error) {
	developer := developerContainerID(services)
	if developer == "" {
		return nil, nil
	}
	output, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", outboxTakeScript, "kuayle-outbox", taskWorktreeRoot})
	if err != nil {
		return nil, err
	}
	return parseOutboxTake([]byte(output), taskWorktreeRoot), nil
}

// PrepareOutbox creates a worktree's outbox and excludes it from commits.
func (r *DockerRuntime) PrepareOutbox(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) error {
	developer := developerContainerID(services)
	if developer == "" || checkout == nil {
		return nil
	}
	_, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", prepareOutboxScript, "kuayle-outbox", checkout.WorkspacePath})
	return err
}

const taskWorktreeRoot = "/workspace/tasks"

// collectOutbox takes the machine's outbox files and records them. Failures
// are logged; agent messages never block execution.
func (m *Manager) collectOutbox(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) {
	runtime, ok := m.runtime.(outboxRuntime)
	store, recording := m.store.(outboxStore)
	if !ok || !recording || !developerRunning(services) {
		return
	}
	files, err := runtime.TakeOutbox(ctx, services)
	if err == nil && len(files) > 0 {
		var recorded int
		if recorded, err = store.RecordAgentOutbox(ctx, machine.ID, files); recorded > 0 {
			_ = m.createEvent(ctx, machine, nil, "agent", "agent_messages.recorded", map[string]any{"count": recorded})
		}
	}
	if err != nil && ctx.Err() == nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "agent_messages.collect"}).WithError(err).Warn("agent messages could not be collected")
	}
}

// prepareOutbox readies a run's worktree for messages before the agent starts.
func (m *Manager) prepareOutbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, checkout *domain.DevMachineCheckout) {
	runtime, ok := m.runtime.(outboxRuntime)
	if !ok || checkout == nil {
		return
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err == nil {
		err = runtime.PrepareOutbox(ctx, services, checkout)
	}
	if err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("agent outbox could not be prepared")
	}
}

var (
	_ outboxRuntime = (*DockerRuntime)(nil)
	_ outboxStore   = (*repository.DevMachineRepository)(nil)
)
