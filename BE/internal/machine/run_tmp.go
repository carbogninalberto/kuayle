package machine

import (
	"context"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Check, custom tool and agent containers have a read-only root and only small
// tmpfs mounts for /tmp (256 MiB) and the home directory (512 MiB), where the
// images put Go's build and module caches. A Go or npm build fills them and
// dies with "no space left on device". Each run therefore gets its own
// directory on the workspace volume for temporary files and caches, created
// when the container starts and removed when the run ends.
const runTmpRoot = "/workspace/.kuayle/tmp"

// runTmpDir is the run's directory; key is a container key such as
// "validation-<run>" or "tool-<run>", never user input.
func runTmpDir(key string) string {
	return runTmpRoot + "/" + key
}

// runTmpVariables are the variables pointed at the run directory.
var runTmpVariables = []string{"TMPDIR", "TMP", "TEMP", "GOTMPDIR", "GOCACHE", "GOMODCACHE", "XDG_CACHE_HOME", "npm_config_cache", "PIP_CACHE_DIR", "YARN_CACHE_FOLDER"}

// runTmpEnvironment points temporary files and the common tool caches at dir.
func runTmpEnvironment(dir string) []string {
	return []string{
		"KUAYLE_RUN_TMP=" + dir,
		"TMPDIR=" + dir + "/tmp", "TMP=" + dir + "/tmp", "TEMP=" + dir + "/tmp", "GOTMPDIR=" + dir + "/tmp",
		"GOCACHE=" + dir + "/cache/go-build", "GOMODCACHE=" + dir + "/go-mod",
		"XDG_CACHE_HOME=" + dir + "/cache", "npm_config_cache=" + dir + "/cache/npm",
		"PIP_CACHE_DIR=" + dir + "/cache/pip", "YARN_CACHE_FOLDER=" + dir + "/cache/yarn",
	}
}

// runTmpPrepare starts a container's command line: it replaces a stale run
// directory (a retry) with fresh directories for every variable. A missing or
// unexpected KUAYLE_RUN_TMP, or a directory that cannot be created, unsets the
// variables, so the command still runs with the image's defaults. Go makes
// its module cache read-only, so removal first makes it writable. The
// container's sh is dash.
const runTmpPrepare = `kuayle_tmp_clean() { chmod -R u+w "$1" 2>/dev/null; rm -rf "$1" 2>/dev/null; }
kuayle_tmp=${KUAYLE_RUN_TMP:-}
case "$kuayle_tmp" in
  /workspace/.kuayle/tmp/*/*|*/..|*/../*) kuayle_tmp= ;;
  /workspace/.kuayle/tmp/?*) ;;
  *) kuayle_tmp= ;;
esac
if [ -n "$kuayle_tmp" ]; then
  [ ! -e "$kuayle_tmp" ] || kuayle_tmp_clean "$kuayle_tmp"
  mkdir -p "$TMPDIR" "$GOCACHE" "$GOMODCACHE" "$XDG_CACHE_HOME" "$npm_config_cache" "$PIP_CACHE_DIR" "$YARN_CACHE_FOLDER" 2>/dev/null || kuayle_tmp=
fi
if [ -z "$kuayle_tmp" ]; then
  unset KUAYLE_RUN_TMP ` + "TMPDIR TMP TEMP GOTMPDIR GOCACHE GOMODCACHE XDG_CACHE_HOME npm_config_cache PIP_CACHE_DIR YARN_CACHE_FOLDER" + `
fi
`

// checkRunner is the fixed command line of a check container. The checked
// argv arrives as positional arguments and is run as is, never interpolated
// into the script; its exit code is the container's. The run directory is
// removed afterwards.
const checkRunner = runTmpPrepare + `"$@"
kuayle_status=$?
[ -z "$kuayle_tmp" ] || kuayle_tmp_clean "$kuayle_tmp"
exit "$kuayle_status"`

// checkEntrypoint and checkCommand form a check container's argv for argv.
var checkEntrypoint = []string{"/bin/sh"}

func checkCommand(argv []string) []string {
	return append([]string{"-c", checkRunner, "kuayle-check"}, argv...)
}

// agentRunPrelude starts an autonomous attempt: it prepares the run
// directory and execs the original entrypoint, so signals still reach it.
// The manager removes the directory once the attempt ends.
const agentRunPrelude = runTmpPrepare + `exec "$@"`

func agentEntrypoint(entrypoint []string) []string {
	return append([]string{"/bin/sh", "-c", agentRunPrelude, "kuayle-run"}, entrypoint...)
}

// agentRunTmpKey names an autonomous attempt's run directory.
func agentRunTmpKey(run *domain.DevMachineAgentRun) string {
	return "agent-" + run.ID.String()
}

// runTmpRemoveScript removes one run directory from the developer container.
const runTmpRemoveScript = `set -u
case "$1" in /workspace/.kuayle/tmp/*/*|*/..|*/../*) exit 2 ;; /workspace/.kuayle/tmp/?*) ;; *) exit 2 ;; esac
[ ! -L "$1" ] || { rm -f "$1"; exit 0; }
[ -e "$1" ] || exit 0
chmod -R u+w "$1" 2>/dev/null
rm -rf "$1"`

// runTmpRuntime is implemented by runtimes that can remove an ended run's
// directory.
type runTmpRuntime interface {
	RemoveRunTmp(ctx context.Context, services []domain.DevMachineService, key string) error
}

// RemoveRunTmp removes a run directory as the developer container's user.
func (r *DockerRuntime) RemoveRunTmp(ctx context.Context, services []domain.DevMachineService, key string) error {
	developer := developerContainerID(services)
	if developer == "" || key == "" || strings.ContainsAny(key, "/.") {
		return nil
	}
	_, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", runTmpRemoveScript, "kuayle-run-tmp", runTmpDir(key)})
	return err
}

var _ runTmpRuntime = (*DockerRuntime)(nil)
