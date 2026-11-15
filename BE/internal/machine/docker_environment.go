package machine

import (
	"context"
	"errors"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// packageIndexProbeScript prints "absent" when the developer container has no
// apt Packages list, or "present <mtime>" with the newest one's modification
// time in Unix seconds. Only Packages files count: a failed update leaves
// apt's lock file and partial/ directory behind.
const packageIndexProbeScript = `f=$(ls -t /var/lib/apt/lists/*Packages* 2>/dev/null | head -n 1)
if [ -z "$f" ]; then echo absent; else echo present $(stat -c %Y "$f"); fi`

// packageIndexRefreshScript refreshes the index with sudo, which fails at
// once instead of asking for a password (-n). timeout ends apt in the
// container before the manager's own deadline.
const packageIndexRefreshScript = `timeout -k 5 110 sudo -n apt-get update 2>&1`

// refreshOutputCapture bounds the output kept while a refresh runs; only its
// last lines are recorded.
const refreshOutputCapture = 256 * 1024

// ProbePackageIndex reads the running developer container's package index,
// as the container's user.
func (r *DockerRuntime) ProbePackageIndex(ctx context.Context, services []domain.DevMachineService) (PackageIndexProbe, error) {
	developer := runningDeveloperContainerID(services)
	if developer == "" {
		return PackageIndexProbe{}, errDeveloperNotRunning
	}
	output, err := r.execOutputAs(ctx, developer, []string{"/bin/sh", "-c", packageIndexProbeScript, "kuayle-package-index"}, "1000:1000")
	if err != nil {
		return PackageIndexProbe{}, err
	}
	return parsePackageIndexProbe(output)
}

// RefreshPackageIndex runs "sudo apt-get update" in the running developer
// container and returns its exit code and output; a non-zero exit is not an
// error.
func (r *DockerRuntime) RefreshPackageIndex(ctx context.Context, services []domain.DevMachineService) (int, string, error) {
	developer := runningDeveloperContainerID(services)
	if developer == "" {
		return 0, "", errDeveloperNotRunning
	}
	return r.execCombined(ctx, developer, []string{"/bin/sh", "-c", packageIndexRefreshScript, "kuayle-package-index"}, "1000:1000")
}

var errDeveloperNotRunning = errors.New("the developer container is not running")

// execCombined runs command and returns its exit code and the end of its
// interleaved stdout and stderr. Unlike execOutputAs, a non-zero exit is a
// result, not an error.
func (r *DockerRuntime) execCombined(ctx context.Context, containerID string, command []string, user string) (int, string, error) {
	created, err := r.client.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		User: user, AttachStdout: true, AttachStderr: true, Cmd: command,
	})
	if err != nil {
		return 0, "", err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return 0, "", err
	}
	defer attached.Close()
	// A hijacked exec stream ignores ctx; closing it unblocks the read.
	defer context.AfterFunc(ctx, attached.Close)()
	output := &tailBuffer{limit: refreshOutputCapture}
	if _, err := stdcopy.StdCopy(output, output, attached.Reader); err != nil {
		if ctx.Err() != nil {
			return 0, output.String(), fmt.Errorf("command timed out: %w", ctx.Err())
		}
		return 0, output.String(), err
	}
	inspection, err := r.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return 0, output.String(), err
	}
	return inspection.ExitCode, output.String(), nil
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	limit int
	data  []byte
}

func (b *tailBuffer) Write(value []byte) (int, error) {
	b.data = append(b.data, value...)
	if extra := len(b.data) - b.limit; extra > 0 {
		b.data = append(b.data[:0], b.data[extra:]...)
	}
	return len(value), nil
}

func (b *tailBuffer) String() string { return string(b.data) }
