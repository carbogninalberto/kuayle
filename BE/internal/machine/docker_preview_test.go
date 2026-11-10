package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// previewDockerFake is a Docker API that holds containers by name and records
// what the runtime asks of it.
type previewDockerFake struct {
	mu         sync.Mutex
	containers map[string]container.InspectResponse
	created    []previewCreate
	removed    []string
	stops      []string
	paused     []string
	unpaused   []string
	execs      []previewExec
	// execOutput is what an exec in a container prints, by container name.
	execOutput map[string]string
	archives   int
	// exitCode is the state a started container reports: 0 keeps it running.
	exitCode int
}

type previewExec struct {
	Container string
	Cmd       []string
	stdin     bool
}

type previewCreate struct {
	Name       string
	Config     container.Config
	HostConfig container.HostConfig
	Networking network.NetworkingConfig
}

func (f *previewDockerFake) add(name string, labels map[string]string, status container.ContainerState) {
	if f.containers == nil {
		f.containers = map[string]container.InspectResponse{}
	}
	f.containers[name] = container.InspectResponse{ID: name, Name: "/" + name, Config: &container.Config{Labels: labels},
		State: &container.State{Status: status, Running: status == container.StateRunning || status == container.StatePaused, Paused: status == container.StatePaused}}
}

func (f *previewDockerFake) serve(t *testing.T) *client.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		path := r.URL.Path
		if index := strings.Index(path, "/containers/"); index >= 0 {
			path = path[index:]
		}
		name, action, _ := strings.Cut(strings.TrimPrefix(path, "/containers/"), "/")
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case action == "exec":
			var body struct {
				Cmd         []string
				AttachStdin bool
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			f.execs = append(f.execs, previewExec{Container: name, Cmd: body.Cmd, stdin: body.AttachStdin})
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": strconv.Itoa(len(f.execs) - 1)})
		case strings.Contains(r.URL.Path, "/exec/") && strings.HasSuffix(r.URL.Path, "/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			id := strings.TrimSuffix(r.URL.Path[strings.Index(r.URL.Path, "/exec/")+len("/exec/"):], "/start")
			index, err := strconv.Atoi(id)
			require.NoError(t, err)
			output := f.execOutput[f.execs[index].Container]
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = rw.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			if output != "" {
				writeDockerFrame(rw, stdcopy.Stdout, output)
			}
			_ = rw.Flush()
			if f.execs[index].stdin {
				f.mu.Unlock()
				_, _ = io.Copy(io.Discard, conn)
				f.mu.Lock()
			}
		case strings.Contains(r.URL.Path, "/exec/") && strings.HasSuffix(r.URL.Path, "/json"):
			_, _ = w.Write([]byte(`{"ExitCode":0}`))
		case strings.HasSuffix(r.URL.Path, "/archive"):
			f.archives++
			http.Error(w, "nothing is copied into a preview", http.StatusInternalServerError)
		case path == "/containers/json":
			items := []container.Summary{}
			for _, item := range f.containers {
				items = append(items, container.Summary{ID: item.ID, Names: []string{item.Name}, Labels: item.Config.Labels, State: item.State.Status})
			}
			_ = json.NewEncoder(w).Encode(items)
		case path == "/containers/create":
			var input struct {
				container.Config
				HostConfig       container.HostConfig
				NetworkingConfig network.NetworkingConfig
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			created := previewCreate{Name: r.URL.Query().Get("name"), Config: input.Config, HostConfig: input.HostConfig, Networking: input.NetworkingConfig}
			f.created = append(f.created, created)
			config := input.Config
			if f.containers == nil {
				f.containers = map[string]container.InspectResponse{}
			}
			f.containers[created.Name] = container.InspectResponse{ID: created.Name, Name: "/" + created.Name, Config: &config, State: &container.State{Status: container.StateCreated}}
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": created.Name})
		case action == "json":
			item, ok := f.containers[name]
			if !ok {
				http.Error(w, `{"message":"no such container"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(item)
		case action == "start":
			item := f.containers[name]
			item.State = &container.State{Status: container.StateRunning, Running: true}
			if f.exitCode != 0 {
				item.State = &container.State{Status: container.StateExited, ExitCode: f.exitCode}
			}
			f.containers[name] = item
			w.WriteHeader(http.StatusNoContent)
		case action == "stop":
			f.stops = append(f.stops, name+" t="+r.URL.Query().Get("t"))
			w.WriteHeader(http.StatusNoContent)
		case action == "pause":
			f.paused = append(f.paused, name)
			w.WriteHeader(http.StatusNoContent)
		case action == "unpause":
			f.unpaused = append(f.unpaused, name)
			w.WriteHeader(http.StatusNoContent)
		case action == "logs":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && action == "":
			if _, ok := f.containers[name]; !ok {
				http.Error(w, `{"message":"no such container"}`, http.StatusNotFound)
				return
			}
			delete(f.containers, name)
			f.removed = append(f.removed, name)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	docker, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = docker.Close() })
	return docker
}

func previewFixture() (*domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout) {
	developer := "developer"
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("workspace"),
		CPUMillis: 4000, MemoryMB: 8192, PidsLimit: 2048, SudoEnabled: true, Status: domain.DevMachineStatusRunning}
	services := []domain.DevMachineService{
		{ServiceType: "ide", ServiceKey: "ide", Status: "running", ImageRef: "developer:fixture", ContainerID: &developer},
		{ServiceType: "terminal", ServiceKey: "terminal", Status: "running", ImageRef: "developer:fixture", ContainerID: &developer},
	}
	return machine, services, &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/eng-1"}
}

// A preview is agent-written code. It runs in a container of its own with the
// isolation of a check: no exec in the developer container, no secrets, no
// sign-ins and no way to gain privileges, even on a machine that allows sudo.
func TestPreviewStartsInItsOwnContainerNotTheDeveloperContainer(t *testing.T) {
	previewStartGrace = 0
	machine, services, checkout := previewFixture()
	docker := &previewDockerFake{}
	runtime := &DockerRuntime{client: docker.serve(t)}
	task := uuid.New()
	require.NoError(t, runtime.StartPreview(context.Background(), machine, services, checkout, task, []string{"npm", "run", "dev"}, 5173, nil))

	// The developer container is only asked, by fixed scripts, to stop a
	// preview an earlier release left in it and to clear the preview's
	// temporary directory. The command itself never reaches it.
	for _, exec := range docker.execs {
		require.Equal(t, "developer", exec.Container)
		require.Contains(t, [][]string{
			{"/bin/sh", "-c", stopPreviewScript, "kuayle-preview-stop", task.String()},
			{"/bin/sh", "-c", runTmpRemoveScript, "kuayle-run-tmp", "/workspace/.kuayle/tmp/preview-" + task.String()},
		}, exec.Cmd)
		require.NotContains(t, exec.Cmd, "npm", "the preview command does not run in the developer container")
	}
	require.Zero(t, docker.archives, "no secret or sign-in file is copied in")
	require.Len(t, docker.created, 1)
	created := docker.created[0]
	require.Equal(t, "kuayle-"+machine.RoutingKey+"-preview-"+task.String(), created.Name)
	require.NotEqual(t, "developer", created.Name)
	require.Equal(t, "developer:fixture", created.Config.Image, "the developer image has the project's toolchain")
	require.Equal(t, "1000:1000", created.Config.User)
	require.Equal(t, checkout.WorkspacePath, created.Config.WorkingDir)
	require.Equal(t, []string{"/bin/sh"}, []string(created.Config.Entrypoint))
	require.Equal(t, []string{"-c", previewRunner, "kuayle-preview", "npm", "run", "dev"}, []string(created.Config.Cmd), "the argv is passed as arguments, never interpolated")

	// PORT, HOST and what a check container gets; nothing else.
	dir := "/workspace/.kuayle/tmp/preview-" + task.String()
	expected := append([]string{"HOME=/home/kuayle", "PORT=5173", "HOST=0.0.0.0"}, proxyVariables("http://"+machine.RoutingKey+"-egress:3128", "localhost,127.0.0.1")...)
	require.ElementsMatch(t, append(expected, runTmpEnvironment(dir)...), created.Config.Env)

	host := created.HostConfig
	require.True(t, host.ReadonlyRootfs)
	require.False(t, host.Privileged)
	require.Equal(t, []string{"ALL"}, []string(host.CapDrop), "sudo machines relax only the developer container")
	require.Contains(t, host.SecurityOpt, "no-new-privileges=true")
	require.Empty(t, host.CapAdd)
	require.Empty(t, host.PortBindings)
	require.Len(t, host.Mounts, 1, "only the workspace volume: no sign-in, inbox or agent CLI mount")
	require.Equal(t, "workspace", host.Mounts[0].Source)
	require.Equal(t, "/workspace", host.Mounts[0].Target)
	require.Equal(t, "network", string(host.NetworkMode))
	require.Equal(t, agentMemoryBytes(machine, false, 1), host.Memory, "the agent share of the machine")
	require.NotZero(t, host.NanoCPUs)

	labels := created.Config.Labels
	require.Equal(t, "agent", labels["com.kuayle.service"], "sized and counted as an agent-class container")
	require.Equal(t, task.String(), labels[previewTaskLabel])
	require.Equal(t, machine.ID.String(), labels["com.kuayle.machine-id"])
	require.Equal(t, machine.WorkspaceID.String(), labels["com.kuayle.workspace-id"])
	require.Equal(t, []string{domain.PreviewPortHost(machine.RoutingKey, 5173)}, created.Networking.EndpointsConfig["network"].Aliases, "the gateway reaches it by this name")
	require.Equal(t, container.StateRunning, docker.containers[created.Name].State.Status)
}

func previewLabels(machine *domain.DevMachine, task uuid.UUID, port int) map[string]string {
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels[previewTaskLabel] = task.String()
	labels[previewPortLabel] = strconv.Itoa(port)
	return labels
}

// Restarting replaces the task's own preview container and leaves the other
// task's preview alone.
func TestPreviewRestartReplacesOnlyThatTasksContainer(t *testing.T) {
	previewStartGrace = 0
	machine, services, checkout := previewFixture()
	docker := &previewDockerFake{}
	runtime := &DockerRuntime{client: docker.serve(t)}
	first, second := uuid.New(), uuid.New()
	ctx := context.Background()
	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, first, []string{"npm", "run", "dev"}, 4100, nil))
	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, second, []string{"npm", "run", "dev"}, 4101, nil))
	require.Empty(t, docker.removed)
	require.Equal(t, agentMemoryBytes(machine, false, 2), docker.created[1].HostConfig.Memory, "running previews split the agent share like agent runs")
	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, first, []string{"npm", "run", "dev"}, 4100, nil))
	require.Equal(t, []string{previewContainerName(machine, first)}, docker.removed)
	require.Len(t, docker.created, 3)
	require.Len(t, docker.containers, 2)
}

// A command that fails at once is reported and leaves no container behind.
func TestPreviewThatExitsAtOnceIsReportedAndRemoved(t *testing.T) {
	previewStartGrace = 0
	machine, services, checkout := previewFixture()
	docker := &previewDockerFake{exitCode: 127}
	runtime := &DockerRuntime{client: docker.serve(t)}
	err := runtime.StartPreview(context.Background(), machine, services, checkout, uuid.New(), []string{"missing-command"}, 4100, nil)
	require.ErrorContains(t, err, "exited with code 127")
	require.Empty(t, docker.containers)
}

func TestPreviewRefusesInvalidInput(t *testing.T) {
	machine, services, checkout := previewFixture()
	docker := &previewDockerFake{}
	runtime := &DockerRuntime{client: docker.serve(t)}
	ctx := context.Background()
	require.Error(t, runtime.StartPreview(ctx, machine, services, checkout, uuid.New(), nil, 4100, nil))
	require.Error(t, runtime.StartPreview(ctx, machine, services, checkout, uuid.New(), []string{"npm"}, 80, nil), "service and privileged ports are refused")
	require.Error(t, runtime.StartPreview(ctx, machine, services, nil, uuid.New(), []string{"npm"}, 4100, nil))
	require.Error(t, runtime.StartPreview(ctx, machine, nil, checkout, uuid.New(), []string{"npm"}, 4100, nil), "without a developer image")
	require.Empty(t, docker.created)
}

// Stopping removes the task's preview container, in any machine state, and
// only a container that is this machine's preview of that task.
func TestStopPreviewRemovesOnlyThatTasksPreviewContainer(t *testing.T) {
	machine, services, _ := previewFixture()
	task, other := uuid.New(), uuid.New()
	docker := &previewDockerFake{}
	docker.add(previewContainerName(machine, task), previewLabels(machine, task, 4100), container.StateRunning)
	docker.add(previewContainerName(machine, other), previewLabels(machine, other, 4101), container.StateRunning)
	runtime := &DockerRuntime{client: docker.serve(t)}
	ctx := context.Background()
	machine.Status = domain.DevMachineStatusPaused
	require.NoError(t, runtime.StopPreview(ctx, machine, services, task))
	require.Equal(t, []string{previewContainerName(machine, task)}, docker.removed)
	require.Equal(t, []string{previewContainerName(machine, task) + " t=5"}, docker.stops, "the command is asked to stop before it is killed")
	require.Empty(t, docker.execs, "a paused developer container is left alone")
	require.NoError(t, runtime.StopPreview(ctx, machine, services, task), "stopping again is a no-op")
	require.Len(t, docker.removed, 1)

	// A container with that name that is not this machine's preview of the
	// task is never removed.
	foreign := uuid.New()
	stranger := *machine
	stranger.ID = uuid.New()
	docker.add(previewContainerName(machine, foreign), previewLabels(&stranger, foreign, 4102), container.StateRunning)
	require.ErrorContains(t, runtime.StopPreview(ctx, machine, services, foreign), "ownership")
	docker.add(previewContainerName(machine, foreign), previewLabels(machine, other, 4102), container.StateRunning)
	require.ErrorContains(t, runtime.StopPreview(ctx, machine, services, foreign), "ownership")
	require.Len(t, docker.removed, 1)
}

// A stopping machine takes every preview container with it and nothing else;
// a pausing machine pauses them and resumes them when it starts again.
func TestMachinePreviewContainersFollowTheMachine(t *testing.T) {
	machine, _, _ := previewFixture()
	first, second := uuid.New(), uuid.New()
	docker := &previewDockerFake{}
	docker.add(previewContainerName(machine, first), previewLabels(machine, first, 4100), container.StateRunning)
	docker.add(previewContainerName(machine, second), previewLabels(machine, second, 4101), container.StateExited)
	agent := machineLabels(machine)
	agent["com.kuayle.service"] = "agent"
	agent["com.kuayle.agent-run-id"] = uuid.NewString()
	docker.add("agent", agent, container.StateRunning)
	ide := machineLabels(machine)
	ide["com.kuayle.service"] = "ide"
	docker.add("developer", ide, container.StateRunning)
	other := *machine
	other.ID = uuid.New()
	docker.add("foreign-preview", previewLabels(&other, uuid.New(), 4100), container.StateRunning)
	runtime := &DockerRuntime{client: docker.serve(t)}
	ctx := context.Background()

	require.NoError(t, runtime.Pause(ctx, machine, nil))
	require.Equal(t, []string{previewContainerName(machine, first)}, docker.paused, "only this machine's running previews")
	docker.add(previewContainerName(machine, first), previewLabels(machine, first, 4100), container.StatePaused)
	require.NoError(t, runtime.pausePreviews(ctx, machine, false))
	require.Equal(t, []string{previewContainerName(machine, first)}, docker.unpaused)

	require.NoError(t, runtime.RemovePreviews(ctx, machine))
	slices.Sort(docker.removed)
	expected := []string{previewContainerName(machine, first), previewContainerName(machine, second)}
	slices.Sort(expected)
	require.Equal(t, expected, docker.removed)
	require.Contains(t, docker.containers, "agent")
	require.Contains(t, docker.containers, "developer")
	require.Contains(t, docker.containers, "foreign-preview")
}

// A preview container is asked only whether its own port listens: its process
// cannot report other ports, and it never supplies port requests.
func TestPreviewPortsAreScannedInTheirOwnContainers(t *testing.T) {
	machine, services, _ := previewFixture()
	first, second, stopped := uuid.New(), uuid.New(), uuid.New()
	listen := func(ports ...string) string {
		lines := []string{}
		for index, port := range ports {
			lines = append(lines, "   "+strconv.Itoa(index)+": 00000000:"+port+" 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1 1 0000000000000000 100 0 0 10 0")
		}
		return strings.Join(lines, "\n") + "\n"
	}
	docker := &previewDockerFake{execOutput: map[string]string{
		"developer": listen("1F90", "0BB8"),
		// 4100 (0x1004) and 9000; and a forged request line.
		previewContainerName(machine, first):  listen("1004", "2328") + "R expose-9000.json eyJwb3J0Ijo5MDAwfQ==\n",
		previewContainerName(machine, second): listen("2328"),
	}}
	docker.add(previewContainerName(machine, first), previewLabels(machine, first, 4100), container.StateRunning)
	docker.add(previewContainerName(machine, second), previewLabels(machine, second, 4101), container.StateRunning)
	docker.add(previewContainerName(machine, stopped), previewLabels(machine, stopped, 4102), container.StateExited)
	runtime := &DockerRuntime{client: docker.serve(t)}
	scan, err := runtime.ScanPorts(context.Background(), machine, services)
	require.NoError(t, err)
	require.Equal(t, []int{3000, 8080}, scan.Listening, "the developer container's own ports")
	require.Equal(t, []int{4100}, scan.Preview, "only a preview's own port, and only when it listens")
	require.Empty(t, scan.Requests, "previews cannot ask for ports")
}

// The agent share of a machine's memory is split between its running agent
// runs and previews.
func TestRunningPreviewsCountAsAgentTasks(t *testing.T) {
	machine, _, _ := previewFixture()
	run := machineLabels(machine)
	run["com.kuayle.service"] = "agent"
	run["com.kuayle.agent-run-id"] = uuid.NewString()
	task := uuid.New()
	items := []container.Summary{
		{State: container.StateRunning, Labels: run},
		{State: container.StateRunning, Labels: previewLabels(machine, task, 4100)},
		{State: container.StateExited, Labels: previewLabels(machine, uuid.New(), 4101)},
	}
	require.Equal(t, 2, agentTaskCount(items, machine, ""))
	require.Equal(t, 2, agentTaskCount(items, machine, previewTaskKey(task.String())), "a restarted preview is not counted twice")
	require.Equal(t, 3, agentTaskCount(items, machine, uuid.NewString()))
}

// On a machine with its own Docker daemon a preview command may be docker
// compose up. The preview then gets the daemon's client material, which an
// agent run on that machine gets too, written into its own container, and no
// other secret.
func TestPreviewOnAMachineWithADockerDaemonGetsOnlyTheDaemonClient(t *testing.T) {
	previewStartGrace = 0
	machine, services, checkout := previewFixture()
	daemon := "daemon"
	services = append(services, domain.DevMachineService{ServiceType: "docker", ServiceKey: "docker", Status: "running", ContainerID: &daemon})
	docker := &previewDockerFake{}
	runtime := &DockerRuntime{client: docker.serve(t)}
	ctx := context.Background()
	task := uuid.New()
	require.ErrorContains(t, runtime.StartPreview(ctx, machine, services, checkout, task, []string{"docker", "compose", "up"}, 4100, map[string]string{"GITHUB_TOKEN": "fixture"}), "no secrets")
	require.ErrorContains(t, runtime.StartPreview(ctx, machine, services, checkout, task, []string{"docker", "compose", "up"}, 4100, map[string]string{"KUAYLE_DOCKER_KEY": "fixture", "KUAYLE_MODEL_API_KEY": "fixture"}), "no secrets")
	require.Empty(t, docker.created)
	require.Empty(t, docker.execs)

	access := map[string]string{"KUAYLE_DOCKER_CA": "ca-fixture", "KUAYLE_DOCKER_CERT": "cert-fixture", "KUAYLE_DOCKER_KEY": "key-fixture", "KUAYLE_DOCKER_HOST": "tcp://" + machine.RoutingKey + "-docker:2376"}
	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, task, []string{"docker", "compose", "up"}, 4100, access))
	require.Len(t, docker.created, 1)
	created := docker.created[0]
	require.Equal(t, []string{"-c", previewDockerPrelude, "kuayle-preview", "docker", "compose", "up"}, []string(created.Config.Cmd))
	require.Contains(t, created.Config.Env, fmt.Sprintf("KUAYLE_SECRET_NAMES=%s", strings.Join(previewDockerMaterial, ",")), "names only")
	for _, env := range created.Config.Env {
		require.NotContains(t, env, "fixture", "values never enter the container configuration")
	}
	require.Equal(t, agentMemoryBytes(machine, true, 1), created.HostConfig.Memory, "the agent share of a machine with a daemon")
	require.Equal(t, []string{"ALL"}, []string(created.HostConfig.CapDrop))
	written := []string{}
	for _, exec := range docker.execs {
		if exec.Container == "developer" {
			continue
		}
		require.Equal(t, created.Name, exec.Container)
		if file, ok := strings.CutPrefix(exec.Cmd[len(exec.Cmd)-1], "umask 077; cat > /run/kuayle-secrets/"); ok {
			written = append(written, file)
		}
	}
	require.ElementsMatch(t, []string{"KUAYLE_DOCKER_CA", "KUAYLE_DOCKER_CERT", "KUAYLE_DOCKER_HOST", "KUAYLE_DOCKER_KEY", ".ready"}, written)
}

// The manager gives a preview the daemon's client material only on a machine
// with a daemon, and nothing on any other machine.
func TestManagerGivesPreviewsOnlyTheDockerDaemonClient(t *testing.T) {
	manager, store, runtime := newPortManager()
	manager.encryptionKey = make([]byte, 32)
	ctx := context.Background()
	start := func() {
		t.Helper()
		store.appPorts.target = previewTarget(store.machine, nil)
		store.checkout = &domain.DevMachineCheckout{ID: store.appPorts.target.Task.CheckoutID, MachineID: store.machine.ID, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1"}
		require.NoError(t, manager.startPreview(ctx, store.machine, &domain.DevMachineOperation{AgentTaskID: &store.appPorts.target.Task.ID}))
	}
	start()
	require.Equal(t, []map[string]string{nil}, runtime.previewDocker, "a machine without a daemon gives a preview nothing")

	daemon := "daemon"
	store.services = append(store.services, domain.DevMachineService{ID: uuid.New(), MachineID: store.machine.ID, ServiceType: "docker", Status: "running", InternalPort: 2376, ContainerID: &daemon})
	start()
	require.Len(t, runtime.previewDocker, 2)
	for name := range runtime.previewDocker[1] {
		require.Contains(t, previewDockerMaterial, name)
	}
	require.Equal(t, "tcp://"+store.machine.RoutingKey+"-docker:2376", runtime.previewDocker[1]["KUAYLE_DOCKER_HOST"])
}
