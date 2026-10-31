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
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// hostileInput is shell syntax that must reach a script as plain text.
const hostileInput = "; rm -rf / && touch pwned $(touch pwned-subshell) `touch pwned-backtick` \"$HOME\" '\n next line"

type toolStoreFake struct {
	*managerStoreFake
	execution *domain.AssistantToolRunExecution
	getErr    error
	starts    int
	output    string
	truncated bool
	exit      *int
	failures  []string
}

func (f *toolStoreFake) GetToolRun(context.Context, domain.DevMachineOperation) (*domain.AssistantToolRunExecution, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	copy := *f.execution
	return &copy, nil
}
func (f *toolStoreFake) StartToolRun(context.Context, domain.DevMachineOperation, string) error {
	f.starts++
	f.output = ""
	f.execution.Run.Status = "running"
	return nil
}
func (f *toolStoreFake) AppendToolRunOutput(_ context.Context, _ domain.DevMachineOperation, content string, truncated bool) error {
	f.output += content
	f.truncated = f.truncated || truncated
	return nil
}
func (f *toolStoreFake) CompleteToolRun(_ context.Context, _ domain.DevMachineOperation, exit int) error {
	f.exit = &exit
	return nil
}
func (f *toolStoreFake) FailToolRun(_ context.Context, _ domain.DevMachineOperation, summary string) error {
	f.failures = append(f.failures, summary)
	return nil
}

type toolRuntimeFake struct {
	*runtimeFake
	calls   int
	exit    int
	image   string
	block   bool
	removed []uuid.UUID
	seen    *domain.AssistantToolRunExecution
}

func (f *toolRuntimeFake) RunTool(ctx context.Context, _ *domain.DevMachine, execution *domain.AssistantToolRunExecution, image string, o *AgentObserver) (*AgentExecution, error) {
	f.calls++
	f.image, f.seen = image, execution
	if err := o.Authorize(); err != nil {
		return nil, err
	}
	if err := o.Started("tool"); err != nil {
		return nil, err
	}
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for _, line := range []string{"2026-10-29T10:00:00Z running login.spec.ts\n", "2026-10-29T10:00:01Z token fixture-private-value leaked\n", "2026-10-29T10:00:02Z 3 passed\n"} {
		if err := o.Output("stdout", []byte(line)); err != nil {
			return nil, err
		}
	}
	return &AgentExecution{ContainerID: "tool", ExitCode: f.exit}, nil
}
func (f *toolRuntimeFake) RemoveTool(_ context.Context, _ *domain.DevMachine, run uuid.UUID) error {
	f.removed = append(f.removed, run)
	return nil
}

func toolManager(t *testing.T, exit int) (*Manager, *toolStoreFake, *toolRuntimeFake, *domain.DevMachineOperation) {
	t.Helper()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture"}
	key := make([]byte, 32)
	encrypted, err := cryptoutil.Encrypt(`{"FIXTURE_CREDENTIAL":"fixture-private-value"}`, key)
	require.NoError(t, err)
	run := domain.AssistantToolRun{ID: uuid.New(), ToolName: "run_e2e", Inputs: json.RawMessage(`{"spec":"login.spec.ts"}`), Status: "queued", TimeoutSeconds: 600, WorkingDirectory: "/workspace/tasks/eng-1", CreatedAt: time.Now()}
	store := &toolStoreFake{managerStoreFake: &managerStoreFake{machine: machine, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}},
		execution: &domain.AssistantToolRunExecution{Run: run, Script: "npx playwright test \"$KUAYLE_ARG_SPEC\"", Interpreter: "sh", EncryptedOutputSecrets: &encrypted}}
	runtime := &toolRuntimeFake{runtimeFake: &runtimeFake{}, exit: exit}
	return &Manager{store: store, runtime: runtime, encryptionKey: key}, store, runtime, &domain.DevMachineOperation{Action: domain.DevMachineOpRunTool, IdempotencyKey: "tool-run:" + run.ID.String()}
}

// A run executes once in the check container, stores its redacted output and
// exit code, and removes its container afterwards.
func TestToolRunStoresTheResultAndRemovesItsContainer(t *testing.T) {
	manager, store, runtime, op := toolManager(t, 1)
	require.NoError(t, manager.processOperation(context.Background(), op))
	require.Equal(t, 1, runtime.calls)
	require.Equal(t, "developer:fixture", runtime.image, "the developer image, as for checks")
	require.Zero(t, runtime.agentRuns, "no agent attempt starts")
	require.Equal(t, 1, store.starts)
	require.Equal(t, 1, *store.exit, "the script's exit decides the result")
	require.Equal(t, "running login.spec.ts\ntoken [REDACTED] leaked\n3 passed\n", store.output)
	require.NotContains(t, store.output, "fixture-private-value")
	require.Equal(t, []uuid.UUID{store.execution.Run.ID}, runtime.removed)
	require.Empty(t, store.failures)
}

func TestToolRunFailsForGoodWhenTheToolWasTurnedOff(t *testing.T) {
	manager, store, runtime, op := toolManager(t, 0)
	store.getErr = repository.ErrToolUnavailable
	err := manager.processOperation(context.Background(), op)
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal), "a refused run is not retried")
	require.Equal(t, "The tool changed or was turned off before it ran", toolRunFailure(err))
	require.Zero(t, runtime.calls)
}

func TestToolRunStopsAtItsTimeout(t *testing.T) {
	manager, store, runtime, op := toolManager(t, 0)
	runtime.block = true
	store.execution.Run.TimeoutSeconds = 1
	store.execution.Run.CreatedAt = time.Now().Add(-2 * time.Second)
	err := manager.processOperation(context.Background(), op)
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal))
	require.Equal(t, "The tool exceeded its time limit", terminal.message)
	require.Equal(t, []uuid.UUID{store.execution.Run.ID}, runtime.removed, "the timed-out container is removed")
	require.Nil(t, store.exit)
}

func TestToolRunOnAStaleMachineIsNotRetried(t *testing.T) {
	manager, store, runtime, op := toolManager(t, 0)
	store.machine.Generation = 2
	op.Generation = 1
	err := manager.processOperation(context.Background(), op)
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal))
	require.Zero(t, runtime.calls)
}

// The container runs a fixed command line; the script and the arguments are
// only in its environment, and it gets no credential mounts.
func TestToolContainerGetsArgumentsOnlyAsData(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("workspace"), CPUMillis: 1000, MemoryMB: 2048, PidsLimit: 256}
	inputs, err := json.Marshal(map[string]string{"spec": hostileInput})
	require.NoError(t, err)
	execution := &domain.AssistantToolRunExecution{Run: domain.AssistantToolRun{ID: uuid.New(), ToolName: "run_e2e", Inputs: inputs, WorkingDirectory: "/workspace/tasks/eng-1"}, Script: "echo \"$KUAYLE_ARG_SPEC\"", Interpreter: "python3"}
	var input struct {
		container.Config
		HostConfig container.HostConfig
	}
	var created []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			created = append(created, r.URL.Query().Get("name"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			_, _ = w.Write([]byte(`{"Id":"tool"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/archive"):
			http.Error(w, "nothing is copied into a tool container", 500)
		case strings.HasSuffix(r.URL.Path, "/wait"):
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
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
	result, err := (&DockerRuntime{client: docker}).RunTool(context.Background(), machine, execution, "developer:fixture", &AgentObserver{})
	require.NoError(t, err)
	require.Zero(t, result.ExitCode)
	require.Equal(t, []string{"kuayle-fixture-tool-" + execution.Run.ID.String()}, created)
	require.Equal(t, []string{"sh"}, input.Entrypoint)
	require.Equal(t, []string{"-c", toolBootstrap, "kuayle-tool", "python3"}, input.Cmd)
	for _, arg := range append(input.Entrypoint, input.Cmd...) {
		require.NotContains(t, arg, "touch pwned", "arguments never reach the command line")
		require.NotContains(t, arg, "KUAYLE_ARG_SPEC\"", "nor does the script")
	}
	require.Equal(t, "/workspace/tasks/eng-1", input.WorkingDir)
	require.Equal(t, "1000:1000", input.User)
	require.Contains(t, input.Env, "KUAYLE_ARG_SPEC="+hostileInput)
	require.Contains(t, input.Env, "KUAYLE_TOOL_SCRIPT=echo \"$KUAYLE_ARG_SPEC\"")
	for _, env := range input.Env {
		require.Regexp(t, `^(HOME|CI|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|KUAYLE_TOOL_NAME|KUAYLE_TOOL_SCRIPT|KUAYLE_TOOL_INPUT|KUAYLE_ARG_SPEC|KUAYLE_RUN_TMP|TMPDIR|TMP|TEMP|GOTMPDIR|GOCACHE|GOMODCACHE|XDG_CACHE_HOME|npm_config_cache|PIP_CACHE_DIR|YARN_CACHE_FOLDER)=`, env)
	}
	require.Contains(t, input.Env, "GOCACHE=/workspace/.kuayle/tmp/tool-"+execution.Run.ID.String()+"/cache/go-build", "caches live on the workspace volume, not the small home tmpfs")
	require.Equal(t, execution.Run.ID.String(), input.Labels["com.kuayle.tool-run-id"])
	require.True(t, input.HostConfig.ReadonlyRootfs)
	require.Contains(t, input.HostConfig.CapDrop, "ALL")
	require.Len(t, input.HostConfig.Mounts, 1, "only the workspace volume, no credential files")

	execution.Interpreter = "perl"
	_, err = (&DockerRuntime{client: docker}).RunTool(context.Background(), machine, execution, "developer:fixture", &AgentObserver{})
	require.ErrorContains(t, err, "unsupported tool interpreter")
}

// The container's real bootstrap, run by the local shell: hostile arguments
// arrive as literal strings in the environment and the JSON file, and none of
// their shell syntax runs.
func TestToolBootstrapDeliversHostileArgumentsLiterally(t *testing.T) {
	inputs, err := json.Marshal(map[string]string{"spec": hostileInput})
	require.NoError(t, err)
	scripts := map[string]string{
		"sh":   `printf '%s' "$KUAYLE_ARG_SPEC" > arg.txt; cat "$KUAYLE_TOOL_INPUT_FILE" > input.json; [ -z "${KUAYLE_TOOL_SCRIPT:-}" ] && echo clean`,
		"bash": `printf '%s' "$KUAYLE_ARG_SPEC" > arg.txt; cat "$KUAYLE_TOOL_INPUT_FILE" > input.json; [[ -z "${KUAYLE_TOOL_SCRIPT:-}" ]] && echo clean`,
		"python3": `import json, os
open("arg.txt", "w").write(os.environ["KUAYLE_ARG_SPEC"])
open("input.json", "w").write(open(os.environ["KUAYLE_TOOL_INPUT_FILE"]).read())
print("clean" if "KUAYLE_TOOL_SCRIPT" not in os.environ else "leaked")`,
		"node": `const fs = require("fs")
fs.writeFileSync("arg.txt", process.env.KUAYLE_ARG_SPEC)
fs.writeFileSync("input.json", fs.readFileSync(process.env.KUAYLE_TOOL_INPUT_FILE))
console.log(process.env.KUAYLE_TOOL_SCRIPT === undefined ? "clean" : "leaked")`,
	}
	machine := &domain.DevMachine{RoutingKey: "fixture"}
	for interpreter, script := range scripts {
		t.Run(interpreter, func(t *testing.T) {
			if _, err := exec.LookPath(interpreter); err != nil {
				t.Skipf("%s is not installed", interpreter)
			}
			work, tmp := t.TempDir(), t.TempDir()
			env, err := toolEnvironment(machine, &domain.AssistantToolRunExecution{Run: domain.AssistantToolRun{ToolName: "probe", Inputs: inputs}, Script: script, Interpreter: interpreter})
			require.NoError(t, err)
			cmd, err := toolCommand(interpreter)
			require.NoError(t, err)
			run := exec.Command("sh", cmd...)
			run.Dir = work
			run.Env = append(append([]string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + tmp}, env...), "HOME="+work)
			out, err := run.CombinedOutput()
			require.NoError(t, err, string(out))
			require.Equal(t, "clean\n", string(out), "the script is not left in the environment")
			arg, err := os.ReadFile(filepath.Join(work, "arg.txt"))
			require.NoError(t, err)
			require.Equal(t, hostileInput, string(arg))
			file, err := os.ReadFile(filepath.Join(work, "input.json"))
			require.NoError(t, err)
			require.JSONEq(t, string(inputs), string(file))
			for _, marker := range []string{"pwned", "pwned-subshell", "pwned-backtick"} {
				_, err := os.Stat(filepath.Join(work, marker))
				require.True(t, os.IsNotExist(err), "%s must not run", marker)
			}
		})
	}
}
