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
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type commandStoreFake struct {
	*managerStoreFake
	run      *domain.CommandRun
	getErr   error
	starts   int
	output   string
	exit     *int
	failures []string
}

func (f *commandStoreFake) GetCommandRun(context.Context, domain.DevMachineOperation) (*domain.CommandRun, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	copy := *f.run
	return &copy, nil
}
func (f *commandStoreFake) StartCommandRun(context.Context, domain.DevMachineOperation) error {
	f.starts++
	f.run.Status = "running"
	return nil
}
func (f *commandStoreFake) AppendCommandRunOutput(_ context.Context, _ domain.DevMachineOperation, content string, _ bool) error {
	f.output += content
	return nil
}
func (f *commandStoreFake) CompleteCommandRun(_ context.Context, _ domain.DevMachineOperation, exit int) error {
	f.exit = &exit
	return nil
}
func (f *commandStoreFake) FailCommandRun(_ context.Context, _ domain.DevMachineOperation, summary string) error {
	f.failures = append(f.failures, summary)
	return nil
}

type commandRuntimeFake struct {
	*runtimeFake
	calls   int
	image   string
	block   bool
	fail    error
	removed []uuid.UUID
}

func (f *commandRuntimeFake) RunCommand(ctx context.Context, _ *domain.DevMachine, run *domain.CommandRun, image string, o *AgentObserver) (*AgentExecution, error) {
	f.calls++
	f.image = image
	if err := o.Authorize(); err != nil {
		return nil, err
	}
	if err := o.Started("command"); err != nil {
		return nil, err
	}
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.fail != nil {
		return nil, f.fail
	}
	if err := o.Output("stdout", []byte("2026-11-15T01:00:00Z 1267650600228229401496703205376\n")); err != nil {
		return nil, err
	}
	return &AgentExecution{ContainerID: "command", ExitCode: 0}, nil
}
func (f *commandRuntimeFake) RemoveCommand(_ context.Context, _ *domain.DevMachine, run uuid.UUID) error {
	f.removed = append(f.removed, run)
	return nil
}

func commandManager(t *testing.T) (*Manager, *commandStoreFake, *commandRuntimeFake, *domain.DevMachineOperation) {
	t.Helper()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture"}
	run := &domain.CommandRun{ID: uuid.New(), Command: "python3 -c 'print(2**100)'", WorkingDirectory: "/workspace", Status: "queued", CreatedAt: time.Now()}
	store := &commandStoreFake{managerStoreFake: &managerStoreFake{machine: machine, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}}, run: run}
	runtime := &commandRuntimeFake{runtimeFake: &runtimeFake{}}
	return &Manager{store: store, runtime: runtime}, store, runtime, &domain.DevMachineOperation{Action: domain.DevMachineOpRunCommand, IdempotencyKey: "command-run:" + run.ID.String()}
}

func TestCommandRunStoresTheResultAndRemovesItsContainer(t *testing.T) {
	manager, store, runtime, op := commandManager(t)
	require.NoError(t, manager.processOperation(context.Background(), op))
	require.Equal(t, 1, runtime.calls)
	require.Equal(t, "developer:fixture", runtime.image, "the developer image, as for checks")
	require.Zero(t, runtime.agentRuns, "no agent attempt starts")
	require.Equal(t, 1, store.starts)
	require.Equal(t, 0, *store.exit)
	require.Equal(t, "1267650600228229401496703205376\n", store.output)
	require.Equal(t, []uuid.UUID{store.run.ID}, runtime.removed)
}

func TestCommandRunIsRefusedForGoodWhenTheOwnerLostTheMachine(t *testing.T) {
	manager, store, runtime, op := commandManager(t)
	store.getErr = repository.ErrCommandMachineUnavailable
	err := manager.processOperation(context.Background(), op)
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal), "a refused run is not retried")
	require.Equal(t, "The run is no longer allowed on this machine", commandRunFailure(err))
	require.Zero(t, runtime.calls)
}

func TestCommandRunStopsAtItsTimeout(t *testing.T) {
	manager, store, runtime, op := commandManager(t)
	runtime.block = true
	deadline := commandRunDeadline
	commandRunDeadline = 50 * time.Millisecond
	t.Cleanup(func() { commandRunDeadline = deadline })
	err := manager.processOperation(context.Background(), op)
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal))
	require.Equal(t, "The command took too long and was stopped after 60 seconds", commandRunFailure(err))
	require.Equal(t, []uuid.UUID{store.run.ID}, runtime.removed, "the stopped container is removed")
	require.Nil(t, store.exit)
}

func TestCommandRunHidesInfrastructureErrors(t *testing.T) {
	manager, _, runtime, op := commandManager(t)
	runtime.fail = errors.New("docker: dial unix /var/run/docker.sock: connection refused")
	err := manager.processOperation(context.Background(), op)
	require.Error(t, err)
	require.Equal(t, "The command could not finish", commandRunFailure(err))
	require.Len(t, runtime.removed, 1, "a command never runs twice, so its container goes")
}

// commandDockerFake answers the Docker API calls of RunCommand and records
// every container it is asked to create, by name.
type commandDockerFake struct {
	api     string
	shared  bool
	created map[string]commandCreateInput
	order   []string
}

type commandCreateInput struct {
	container.Config
	HostConfig container.HostConfig
}

func (f *commandDockerFake) run(t *testing.T, machine *domain.DevMachine, run *domain.CommandRun) error {
	t.Helper()
	f.created = map[string]commandCreateInput{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("Api-Version", f.api)
			_, _ = w.Write([]byte("OK"))
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			var input commandCreateInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			name := r.URL.Query().Get("name")
			f.created[name], f.order = input, append(f.order, name)
			_, _ = w.Write([]byte(`{"Id":"` + name + `"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/wait"):
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.WriteHeader(200)
		case r.Method == http.MethodDelete:
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.44"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	if f.shared {
		runtime.slices = &machineSlicesFake{}
		runtime.machineScopes = map[uuid.UUID]bool{machine.ID: true}
	}
	_, err = runtime.RunCommand(context.Background(), machine, run, "developer:fixture", &AgentObserver{})
	return err
}

func commandFixtureMachine() *domain.DevMachine {
	return &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("workspace"), CPUMillis: 1000, MemoryMB: 2048, PidsLimit: 256}
}

// A write run's prep container is scoped to the machine slice like the command
// container, so on a shared machine it uses the machine's memory and CPUs, not
// a small per-container share. Without slices it keeps its bounded share.
func TestCommandPrepContainerSharesTheMachineSlice(t *testing.T) {
	machine := commandFixtureMachine()
	machine.RoutingKey = "0123456789abcdef0123"
	folder := domain.CommandFolder(uuid.New())
	run := &domain.CommandRun{ID: uuid.New(), Command: "echo hi", Write: true, WorkingDirectory: folder}
	docker := &commandDockerFake{api: "1.52", shared: true}
	require.NoError(t, docker.run(t, machine, run))

	slice := "kuayle-0123456789abcdef0123.slice"
	for _, name := range []string{commandPrepName(machine, run.ID), commandContainerName(machine, run.ID)} {
		input := docker.created[name]
		require.Equal(t, slice, input.HostConfig.CgroupParent, name)
		require.Equal(t, int64(machine.MemoryMB)<<20, input.HostConfig.Resources.Memory, name)
		require.Equal(t, int64(machine.CPUMillis)*1_000_000, input.HostConfig.Resources.NanoCPUs, name)
		require.Equal(t, int64(-1), input.HostConfig.Resources.MemorySwap, name)
		require.Equal(t, resourceScopeMachine, input.Labels[resourceScopeLabel], name)
	}

	plainRun := &domain.CommandRun{ID: uuid.New(), Command: "echo hi", Write: true, WorkingDirectory: folder}
	plain := &commandDockerFake{api: "1.52"}
	require.NoError(t, plain.run(t, machine, plainRun))
	require.Empty(t, plain.created[commandPrepName(machine, plainRun.ID)].HostConfig.CgroupParent, "without slices the prep container keeps its own limit")
}

// The command is only in the container's environment, and /workspace is
// always read-only: a write run can change only its conversation's folder,
// mounted read-write from a subpath of the workspace volume.
func TestCommandContainerGetsTheCommandOnlyAsData(t *testing.T) {
	machine := commandFixtureMachine()
	for _, write := range []bool{false, true} {
		folder := domain.CommandFolder(uuid.New())
		run := &domain.CommandRun{ID: uuid.New(), Command: hostileInput, Write: write, WorkingDirectory: folder}
		docker := &commandDockerFake{api: "1.52"}
		require.NoError(t, docker.run(t, machine, run))
		input := docker.created[commandContainerName(machine, run.ID)]
		require.Equal(t, []string{"sh"}, input.Entrypoint)
		require.Equal(t, []string{"-c", commandBootstrap, "kuayle-cmd", "60"}, input.Cmd)
		for _, arg := range append(input.Entrypoint, input.Cmd...) {
			require.NotContains(t, arg, "touch pwned", "the command never reaches the command line")
		}
		require.Contains(t, input.Env, "KUAYLE_COMMAND="+hostileInput)
		require.Contains(t, input.Env, "KUAYLE_COMMAND_CWD="+run.WorkingDirectory)
		for _, env := range input.Env {
			require.Regexp(t, `^(HOME|CI|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy|KUAYLE_COMMAND|KUAYLE_COMMAND_CWD|KUAYLE_COMMAND_WRITE)=`, env, "no credentials")
		}
		require.Equal(t, run.ID.String(), input.Labels["com.kuayle.command-run-id"])
		require.True(t, input.HostConfig.ReadonlyRootfs)
		require.Contains(t, input.HostConfig.CapDrop, "ALL")
		require.NotNil(t, input.HostConfig.Init, "tini is PID 1, so the shell's timeout child is reaped and killable")
		require.True(t, *input.HostConfig.Init)
		require.Equal(t, "/workspace", input.HostConfig.Mounts[0].Target)
		require.True(t, input.HostConfig.Mounts[0].ReadOnly, "/workspace is never writable")
		if !write {
			require.Len(t, input.HostConfig.Mounts, 1, "only the workspace volume")
			require.Equal(t, []string{commandContainerName(machine, run.ID)}, docker.order, "a read-only run needs no prep")
			continue
		}
		require.Len(t, input.HostConfig.Mounts, 2)
		writable := input.HostConfig.Mounts[1]
		require.Equal(t, "workspace", writable.Source)
		require.Equal(t, folder, writable.Target)
		require.False(t, writable.ReadOnly)
		require.NotNil(t, writable.VolumeOptions)
		require.Equal(t, strings.TrimPrefix(folder, "/workspace/"), writable.VolumeOptions.Subpath)
		require.True(t, writable.VolumeOptions.NoCopy)

		// The folder is created first, by a container that never sees the
		// command and has no network.
		require.Equal(t, []string{commandPrepName(machine, run.ID), commandContainerName(machine, run.ID)}, docker.order)
		prep := docker.created[commandPrepName(machine, run.ID)]
		require.Equal(t, []string{"sh"}, prep.Entrypoint)
		require.Equal(t, commandPrepArgv(folder), prep.Cmd)
		require.Equal(t, "1000:1000", prep.User)
		for _, arg := range append(prep.Cmd, prep.Env...) {
			require.NotContains(t, arg, "touch pwned")
			require.NotContains(t, arg, "KUAYLE_COMMAND")
		}
		require.True(t, prep.NetworkDisabled)
		require.Equal(t, "none", string(prep.HostConfig.NetworkMode))
		require.Equal(t, "true", prep.Labels["com.kuayle.command-prep"])
		require.Equal(t, run.ID.String(), prep.Labels["com.kuayle.command-run-id"], "RemoveCommand finds it")
	}

	_, err := (&DockerRuntime{}).RunCommand(context.Background(), machine, &domain.CommandRun{ID: uuid.New(), Command: "ls", WorkingDirectory: "/workspace/../etc"}, "developer:fixture", &AgentObserver{})
	require.ErrorContains(t, err, "invalid command run")
	_, err = (&DockerRuntime{}).RunCommand(context.Background(), machine, &domain.CommandRun{ID: uuid.New(), Command: "ls", Write: true, WorkingDirectory: "/workspace"}, "developer:fixture", &AgentObserver{})
	require.ErrorContains(t, err, "invalid command run", "a write run is only ever in a conversation folder")
}

// A daemon that cannot mount a subpath refuses write runs for good, rather
// than making all of /workspace writable; read-only runs still run.
func TestCommandWriteRunIsRefusedOnOldDocker(t *testing.T) {
	machine := commandFixtureMachine()
	docker := &commandDockerFake{api: "1.44"}
	err := docker.run(t, machine, &domain.CommandRun{ID: uuid.New(), Command: "touch x", Write: true, WorkingDirectory: domain.CommandFolder(uuid.New())})
	var terminal *terminalOperationError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, "This machine's Docker is too old to confine writes to the conversation folder", commandRunFailure(err))
	require.Empty(t, docker.created, "nothing is created")

	require.NoError(t, docker.run(t, machine, &domain.CommandRun{ID: uuid.New(), Command: "ls", WorkingDirectory: "/workspace"}))
	require.Len(t, docker.created, 1)
}

func TestCommandWriteFolderIsOneConversationFolder(t *testing.T) {
	conversation := uuid.New()
	subpath, ok := commandWriteFolder(domain.CommandFolder(conversation))
	require.True(t, ok)
	require.Equal(t, "kuayle-assistant/"+conversation.String(), subpath)
	for _, directory := range []string{"/workspace", domain.CommandFolderRoot, domain.CommandFolderRoot + "/", domain.CommandFolderRoot + "/a/b", domain.CommandFolderRoot + "/../x", domain.CommandFolderRoot + "/.hidden", "/workspace/repo"} {
		_, ok := commandWriteFolder(directory)
		require.False(t, ok, directory)
	}
}

// The containers' real scripts, run by the local shell: the prep creates the
// folder and refuses a symbolic link; the bootstrap enters the folder and
// never creates one.
func TestCommandBootstrapEntersTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "kuayle-assistant", "conversation")
	machine := &domain.DevMachine{RoutingKey: "fixture"}
	runIn := func(run *domain.CommandRun) (string, error) {
		cmd := exec.Command("sh", commandArgv()...)
		cmd.Dir = root
		cmd.Env = append(append([]string{"PATH=" + os.Getenv("PATH")}, commandEnvironment(machine, run)...), "KUAYLE_COMMAND_CWD="+folder, "HOME="+root)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	prep := func(dir string) error {
		return exec.Command("sh", commandPrepArgv(dir)...).Run()
	}

	for _, write := range []bool{false, true} {
		out, err := runIn(&domain.CommandRun{Command: "pwd", Write: write})
		require.Error(t, err, "the bootstrap does not create its directory")
		require.Contains(t, out, "does not exist")
		_, statErr := os.Stat(folder)
		require.True(t, os.IsNotExist(statErr))
	}

	require.NoError(t, prep(folder))
	require.NoError(t, prep(folder), "preparing again is harmless")
	out, err := runIn(&domain.CommandRun{Command: `echo hi > out.txt; pwd; [ -z "${KUAYLE_COMMAND:-}" ] && echo clean`, Write: true})
	require.NoError(t, err, out)
	require.Equal(t, folder+"\nclean\n", out, "the command is not left in its environment")
	written, err := os.ReadFile(filepath.Join(folder, "out.txt"))
	require.NoError(t, err)
	require.Equal(t, "hi\n", string(written))

	out, err = runIn(&domain.CommandRun{Command: "exit 7"})
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, out)
	require.Equal(t, 7, exitErr.ExitCode(), "the command's exit status is the container's")

	link := filepath.Join(root, "kuayle-assistant", "linked")
	require.NoError(t, os.Symlink(root, link))
	require.Error(t, prep(link), "a folder that is a link to elsewhere is refused")
	require.Error(t, prep(filepath.Join(link, "inside")), "so is one inside such a link")
}

// Expired temporary machines are permanently deleted by reconciliation.
func TestReconcileDeletesExpiredTemporaryMachines(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute)
	expired := domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning,
		Generation: 2, TemporaryExpiresAt: &expiredAt, DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume")}
	store := &managerStoreFake{machine: &expired, expiredTemporaryMachines: []domain.DevMachine{expired}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, nil, nil, "test")

	require.NoError(t, manager.reconcile(context.Background()))
	require.Equal(t, 1, store.permanentDeleteRequests)
	require.Equal(t, []domain.DevMachineOperationAction{domain.DevMachineOpTeardown}, store.actions)
	require.Equal(t, []domain.DevMachineStatus{domain.DevMachineStatusDestroyed}, store.desired)
}
