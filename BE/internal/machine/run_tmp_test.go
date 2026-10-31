package machine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// runLocally runs a container script with the local shell, its fixed
// /workspace/.kuayle/tmp root moved under a temporary directory.
func runLocally(t *testing.T, script, root string, env []string, args ...string) (string, int) {
	t.Helper()
	script = strings.ReplaceAll(script, runTmpRoot, root)
	for i, item := range env {
		env[i] = strings.ReplaceAll(item, runTmpRoot, root)
	}
	cmd := exec.Command("sh", append([]string{"-c", script, "kuayle-check"}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + cmd.Dir}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return string(out), code
}

func TestCheckRunnerUsesAndRemovesItsRunDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tmp")
	dir := runTmpDir("validation-fixture")
	local := strings.ReplaceAll(dir, runTmpRoot, root)
	// A stale directory from an earlier try, with Go's read-only module cache.
	require.NoError(t, os.MkdirAll(filepath.Join(local, "go-mod", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(local, "go-mod", "pkg", "stale"), []byte("x"), 0o444))
	require.NoError(t, os.Chmod(filepath.Join(local, "go-mod", "pkg"), 0o555))

	probe := `[ ! -e "$GOMODCACHE/pkg/stale" ] || exit 9
for d in "$TMPDIR" "$GOCACHE" "$GOMODCACHE" "$XDG_CACHE_HOME" "$npm_config_cache" "$PIP_CACHE_DIR" "$YARN_CACHE_FOLDER"; do [ -d "$d" ] || exit 8; done
mkdir -p "$GOMODCACHE/m" && touch "$GOMODCACHE/m/f" && chmod 0555 "$GOMODCACHE/m"
printf '%s|' "$TMPDIR" "$1"; exit 3`
	out, code := runLocally(t, checkRunner, root, runTmpEnvironment(dir), "sh", "-c", probe, "probe", "$(touch pwned) ; rm x")
	require.Equal(t, 3, code, out)
	require.Equal(t, local+"/tmp|$(touch pwned) ; rm x|", out, "the argv runs as given, with TMPDIR on the run directory")
	_, err := os.Stat(local)
	require.True(t, os.IsNotExist(err), "the run directory is removed afterwards, read-only module cache included")
}

func TestCheckRunnerFallsBackToImageDefaults(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tmp")
	for name, env := range map[string][]string{
		"no run directory":      nil,
		"outside the tmp root":  append(runTmpEnvironment("/elsewhere/run"), "KUAYLE_RUN_TMP=/elsewhere/run"),
		"nested path traversal": {"KUAYLE_RUN_TMP=" + runTmpRoot + "/../escape", "TMPDIR=" + runTmpRoot + "/../escape/tmp"},
	} {
		t.Run(name, func(t *testing.T) {
			out, code := runLocally(t, checkRunner, root, env, "sh", "-c", `printf '%s' "${TMPDIR-unset}:${GOCACHE-unset}"`)
			require.Zero(t, code, out)
			require.Equal(t, "unset:unset", out)
		})
	}
}

func TestAgentEntrypointPreparesTheRunDirectoryAndExecs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tmp")
	dir := runTmpDir("agent-fixture")
	entrypoint := agentEntrypoint([]string{"sh", "-c", `[ -d "$GOCACHE" ] && printf '%s' "$1"`, "runner"})
	require.Equal(t, []string{"/bin/sh", "-c", agentRunPrelude, "kuayle-run"}, entrypoint[:4])
	out, code := runLocally(t, agentRunPrelude, root, runTmpEnvironment(dir), append(entrypoint[4:], "argv")...)
	require.Zero(t, code, out)
	require.Equal(t, "argv", out)
	_, err := os.Stat(strings.ReplaceAll(dir, runTmpRoot, root) + "/cache/go-build")
	require.NoError(t, err, "the manager removes an attempt's directory after the attempt")
}

func TestAutonomousAgentContainerUsesARunDirectory(t *testing.T) {
	for _, mode := range []string{"autonomous", "interactive"} {
		t.Run(mode, func(t *testing.T) {
			var input struct {
				container.Config
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/images/"):
					_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
					_, _ = w.Write([]byte(`{"Id":"native"}`))
				case r.Method == http.MethodDelete:
					w.WriteHeader(204)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			require.NoError(t, err)
			defer docker.Close()
			machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume")}
			run := &domain.DevMachineAgentRun{ID: uuid.New(), Mode: mode, CommandArgv: []byte(`["codex"]`)}
			stop := errors.New("stop after create")
			_, err = (&DockerRuntime{client: docker}).RunAgent(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "fixture"}, nil, nil, &AgentObserver{Authorize: func() error { return stop }})
			require.ErrorIs(t, err, stop)
			gocache := "GOCACHE=/workspace/.kuayle/tmp/agent-" + run.ID.String() + "/cache/go-build"
			if mode == "interactive" {
				require.Equal(t, []string{"/usr/local/bin/kuayle-agent-runner"}, input.Entrypoint)
				require.NotContains(t, input.Env, gocache)
				return
			}
			require.Equal(t, agentEntrypoint([]string{"/usr/local/bin/kuayle-agent-runner"}), input.Entrypoint)
			require.Contains(t, input.Env, gocache)
			require.Contains(t, input.Env, "TMPDIR=/workspace/.kuayle/tmp/agent-"+run.ID.String()+"/tmp")
		})
	}
}
