package machine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// previewScript replaces the machine's previous preview: it stops the old
// process group, then starts the command in its own session so it outlives
// this exec. Output goes to a bounded log file in the container's /tmp.
const previewScript = `set -eu
pidfile=/tmp/kuayle-preview.pid
if [ -f "$pidfile" ]; then kill -TERM -- "-$(cat "$pidfile")" 2>/dev/null || true; fi
setsid "$@" >/tmp/kuayle-preview.log 2>&1 </dev/null &
echo $! >"$pidfile"`

// StartPreview runs a project's preview command in the task worktree on the
// developer container, listening on port.
func (r *DockerRuntime) StartPreview(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, argv []string, port int) error {
	developer := developerContainerID(services)
	if developer == "" || checkout == nil {
		return fmt.Errorf("developer container is unavailable")
	}
	if len(argv) == 0 || domain.ValidateTestCommand(argv) != nil || port < 1024 || port > 65535 {
		return &terminalOperationError{code: "preview_invalid", message: "Preview command or port is invalid"}
	}
	created, err := r.client.ExecCreate(ctx, developer, client.ExecCreateOptions{
		User: "1000:1000", WorkingDir: checkout.WorkspacePath, AttachStdout: true, AttachStderr: true,
		Env: []string{"PORT=" + strconv.Itoa(port), "HOST=0.0.0.0"},
		Cmd: append([]string{"/bin/sh", "-c", previewScript, "kuayle-preview"}, argv...),
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
