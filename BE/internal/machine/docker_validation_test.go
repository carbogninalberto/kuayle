package machine

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidationRunsExactCommandWithoutHarnessOrCredentialHandoff(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("workspace"), CPUMillis: 1000, MemoryMB: 2048, PidsLimit: 256}
	run := &domain.DevMachineAgentRun{ID: uuid.New()}
	step := &domain.DevMachineAgentRunStep{ID: uuid.New(), CommandArgv: json.RawMessage(`["sh","-c","exit 7"]`)}
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/fixture"}
	var config container.Config
	var host container.HostConfig
	var starts, archives, authorizations, started int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			var input struct {
				container.Config
				HostConfig container.HostConfig
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			config = input.Config
			host = input.HostConfig
			_, _ = w.Write([]byte(`{"Id":"validation"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			starts++
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/archive"):
			archives++
			http.Error(w, "credentials must not be copied", 500)
		case strings.HasSuffix(r.URL.Path, "/wait"):
			_, _ = w.Write([]byte(`{"StatusCode":7}`))
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	result, err := (&DockerRuntime{client: docker}).RunValidation(context.Background(), machine, run, step, 0, checkout, "developer:fixture", &AgentObserver{Authorize: func() error { authorizations++; return nil }, Started: func(id string) error { require.Equal(t, "validation", id); started++; return nil }})
	require.NoError(t, err)
	require.Equal(t, 7, result.ExitCode)
	require.Equal(t, 1, starts)
	require.Equal(t, 1, started)
	require.GreaterOrEqual(t, authorizations, 2)
	require.Zero(t, archives)
	// The exact argv runs after the fixed runner, as positional arguments.
	require.Equal(t, []string{"/bin/sh"}, config.Entrypoint)
	require.Equal(t, []string{"-c", checkRunner, "kuayle-check", "sh", "-c", "exit 7"}, config.Cmd)
	// The tmpfs /tmp and home are small; temporary files and caches live in a
	// per-run directory on the workspace volume.
	dir := "/workspace/.kuayle/tmp/validation-" + run.ID.String()
	for _, env := range []string{"KUAYLE_RUN_TMP=" + dir, "TMPDIR=" + dir + "/tmp", "GOTMPDIR=" + dir + "/tmp", "GOCACHE=" + dir + "/cache/go-build", "GOMODCACHE=" + dir + "/go-mod", "XDG_CACHE_HOME=" + dir + "/cache", "npm_config_cache=" + dir + "/cache/npm", "PIP_CACHE_DIR=" + dir + "/cache/pip"} {
		require.Contains(t, config.Env, env)
	}
	require.Equal(t, checkout.WorkspacePath, config.WorkingDir)
	require.Equal(t, "1000:1000", config.User)
	require.True(t, host.ReadonlyRootfs)
	require.False(t, host.Privileged)
	require.Contains(t, host.CapDrop, "ALL")
	require.Contains(t, host.SecurityOpt, "no-new-privileges=true")
	require.Len(t, host.Mounts, 1)
	require.Equal(t, "workspace", host.Mounts[0].Source)
	for _, env := range config.Env {
		require.NotContains(t, env, "TOKEN")
		require.NotContains(t, env, "API_KEY")
		require.NotContains(t, env, "KUAYLE_SECRET")
	}
}

func TestValidationRecoveryReplaysExitedContainerWithoutRerunning(t *testing.T) {
	for _, test := range []struct {
		name             string
		tampered, legacy bool
	}{{"replay", false, false}, {"replay_direct_argv_container", false, true}, {"wrong_ownership", true, false}} {
		tampered := test.tampered
		t.Run(test.name, func(t *testing.T) {
			machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("workspace")}
			run := &domain.DevMachineAgentRun{ID: uuid.New()}
			step := &domain.DevMachineAgentRunStep{ID: uuid.New(), CommandArgv: []byte(`["sh","-c","exit 7"]`)}
			checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/task"}
			labels := machineLabels(machine)
			labels["com.kuayle.service"] = "agent"
			labels["com.kuayle.agent-run-id"] = run.ID.String()
			labels["com.kuayle.validation-id"] = step.ID.String()
			if tampered {
				labels["com.kuayle.workspace-id"] = uuid.NewString()
			}
			entrypoint, cmd := checkEntrypoint, checkCommand([]string{"sh", "-c", "exit 7"})
			if test.legacy {
				entrypoint, cmd = []string{"sh"}, []string{"-c", "exit 7"}
			}
			var starts, creates, observed int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/images/"):
					_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
				case strings.HasSuffix(r.URL.Path, "/json"):
					_ = json.NewEncoder(w).Encode(container.InspectResponse{ID: "retained", Config: &container.Config{Image: "developer:fixture", Labels: labels, Entrypoint: entrypoint, Cmd: cmd, WorkingDir: checkout.WorkspacePath}, State: &container.State{Status: "exited", ExitCode: 7}})
				case strings.HasSuffix(r.URL.Path, "/logs"):
					writeDockerFrame(w, stdcopy.Stdout, "2026-10-27T10:00:00Z retained output\n")
				case strings.HasSuffix(r.URL.Path, "/start"):
					starts++
					w.WriteHeader(204)
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					creates++
					http.Error(w, "must not recreate", 500)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			require.NoError(t, err)
			defer docker.Close()
			result, err := (&DockerRuntime{client: docker}).RunValidation(context.Background(), machine, run, step, 0, checkout, "developer:fixture", &AgentObserver{Started: func(string) error { observed++; return nil }})
			if tampered {
				require.Error(t, err)
				require.Zero(t, observed)
			} else {
				require.NoError(t, err)
				require.Equal(t, 7, result.ExitCode)
				require.Contains(t, result.Stdout, "retained output")
				require.Equal(t, 1, observed)
			}
			require.Zero(t, starts)
			require.Zero(t, creates)
		})
	}
}
