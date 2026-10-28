package machine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// previewScript replaces one task's previous preview: it stops that task's old
// process group only, then starts the command in its own session so it
// outlives this exec. Output is appended to a log in the container's /tmp that
// a watcher truncates beyond 8 MiB.
const previewScript = `set -eu
task=$1
shift
pidfile=/tmp/kuayle-preview-$task.pid
log=/tmp/kuayle-preview-$task.log
if [ -f "$pidfile" ]; then
  old=$(cat "$pidfile")
  case "$old" in ''|*[!0-9]*) ;; *) kill -TERM "-$old" 2>/dev/null || true ;; esac
fi
: >"$log"
setsid "$@" >>"$log" 2>&1 </dev/null &
pid=$!
echo "$pid" >"$pidfile"
setsid sh -c 'while kill -0 "$1" 2>/dev/null; do sleep 15; size=$(wc -c <"$2" 2>/dev/null || echo 0); [ "$size" -le 8388608 ] || : >"$2"; done' kuayle-preview-log "$pid" "$log" >/dev/null 2>&1 </dev/null &`

// stopPreviewScript stops one task's preview process group, forcefully after
// five seconds. The container's sh is dash, whose kill rejects "--", so process
// groups are named as a plain negative PID.
const stopPreviewScript = `set -u
pidfile=/tmp/kuayle-preview-$1.pid
[ -f "$pidfile" ] || exit 0
pid=$(cat "$pidfile")
rm -f "$pidfile"
case "$pid" in ''|*[!0-9]*) exit 0 ;; esac
kill -TERM "-$pid" 2>/dev/null || exit 0
i=0
while kill -0 "-$pid" 2>/dev/null && [ "$i" -lt 10 ]; do sleep 0.5; i=$((i + 1)); done
kill -KILL "-$pid" 2>/dev/null || true`

// portScanScript prints the LISTEN sockets of /proc/net/tcp{,6}, then takes the
// agents' request files: regular files only (never symlinks), at most 20 per
// scan and 1 KiB each, base64-encoded. Every non-hidden entry is removed.
const portScanScript = `set -u
grep -hE '^ *[0-9]+: [0-9A-Fa-f]+:[0-9A-Fa-f]{4} [0-9A-Fa-f]+:[0-9A-Fa-f]{4} 0A ' /proc/net/tcp /proc/net/tcp6 2>/dev/null | head -n 1024
dir="$HOME/.kuayle/ports/requests"
[ -d "$dir" ] && [ ! -L "$dir" ] || exit 0
n=0
for f in "$dir"/*; do
  [ -e "$f" ] || [ -L "$f" ] || continue
  name=${f##*/}
  if [ "$n" -lt 20 ] && [ -f "$f" ] && [ ! -L "$f" ]; then
    case "$name" in
      expose-[0-9]*.json|unexpose-[0-9]*)
        printf 'R %s %s\n' "$name" "$(head -c 1024 "$f" | base64 | tr -d '\n')"
        n=$((n + 1)) ;;
    esac
  fi
  rm -f -- "$f"
done
exit 0`

// writePortStateScript atomically replaces ~/.kuayle/ports/state.json.
const writePortStateScript = `set -eu
dir="$HOME/.kuayle/ports"
mkdir -p "$dir/requests"
[ ! -L "$dir/state.json" ] || rm -f "$dir/state.json"
tmp="$dir/.state.json.$$"
head -c 65536 >"$tmp"
mv -f "$tmp" "$dir/state.json"`

// StartPreview runs a task's preview command in its worktree on the developer
// container, listening on port.
func (r *DockerRuntime) StartPreview(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, taskID uuid.UUID, argv []string, port int) error {
	developer := developerContainerID(services)
	if developer == "" || checkout == nil {
		return fmt.Errorf("developer container is unavailable")
	}
	if len(argv) == 0 || domain.ValidateTestCommand(argv) != nil || !domain.ValidAppPort(port) || taskID == uuid.Nil {
		return &terminalOperationError{code: "preview_invalid", message: "Preview command or port is invalid"}
	}
	created, err := r.client.ExecCreate(ctx, developer, client.ExecCreateOptions{
		User: "1000:1000", WorkingDir: checkout.WorkspacePath, AttachStdout: true, AttachStderr: true,
		Env: []string{"PORT=" + strconv.Itoa(port), "HOST=0.0.0.0"},
		Cmd: append([]string{"/bin/sh", "-c", previewScript, "kuayle-preview", taskID.String()}, argv...),
	})
	if err != nil {
		return err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attached.Close()
	stderr := newCappedBuffer(4096)
	stdout := newCappedBuffer(4096)
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); err != nil {
		return err
	}
	inspection, err := r.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if inspection.ExitCode != 0 {
		return fmt.Errorf("preview start failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// StopPreview stops one task's preview process group.
func (r *DockerRuntime) StopPreview(ctx context.Context, services []domain.DevMachineService, taskID uuid.UUID) error {
	developer := developerContainerID(services)
	if developer == "" {
		return nil
	}
	_, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", stopPreviewScript, "kuayle-preview-stop", taskID.String()})
	return err
}

// ScanPorts reads the developer container's listening ports and agent port
// requests, as the container's user.
func (r *DockerRuntime) ScanPorts(ctx context.Context, services []domain.DevMachineService) (PortScan, error) {
	developer := developerContainerID(services)
	if developer == "" {
		return PortScan{}, nil
	}
	output, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", portScanScript, "kuayle-port-scan"})
	if err != nil {
		return PortScan{}, err
	}
	scan := parsePortScan(output)
	// Published ports of the daemon's containers listen in the docker service
	// container's network namespace. A failed daemon scan leaves them unknown.
	if daemon := dockerContainerID(services); daemon != "" {
		if output, err := r.execOutput(ctx, daemon, []string{"/bin/sh", "-c", dockerPortScanScript, "kuayle-port-scan"}); err == nil {
			scan.Docker = parsePortScan(output).Listening
		}
	}
	return scan, nil
}

// dockerPortScanScript prints the LISTEN sockets of the docker service
// container: the daemon's API port and the ports its containers publish.
const dockerPortScanScript = `grep -hE '^ *[0-9]+: [0-9A-Fa-f]+:[0-9A-Fa-f]{4} [0-9A-Fa-f]+:[0-9A-Fa-f]{4} 0A ' /proc/net/tcp /proc/net/tcp6 2>/dev/null | head -n 1024
exit 0`

// WritePortState publishes the app port state to kuayle-ports.
func (r *DockerRuntime) WritePortState(ctx context.Context, services []domain.DevMachineService, state []byte) error {
	developer := developerContainerID(services)
	if developer == "" {
		return nil
	}
	return r.execWithInput(ctx, developer, []string{"/bin/sh", "-c", writePortStateScript, "kuayle-port-state"}, string(state))
}
