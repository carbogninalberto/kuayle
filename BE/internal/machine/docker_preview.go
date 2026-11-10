package machine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// A task's preview command runs code the agent just wrote. It runs in a
// container of its own with the isolation of a check container: the developer
// image and the workspace volume, but none of the developer container's
// secrets, sign-ins, sudo or processes.

// previewTaskLabel marks a preview container with its task; previewPortLabel
// holds the port it serves.
const (
	previewTaskLabel = "com.kuayle.preview-task-id"
	previewPortLabel = "com.kuayle.preview-port"
)

// previewStartGrace is how long a started preview is given before its state
// is read, so a command that fails at once is reported instead of left as a
// dead URL.
var previewStartGrace = time.Second

// previewDockerMaterial is the only material a preview container may receive:
// the client access to the machine's own Docker daemon that an agent run on
// the same machine gets, so a preview command can be docker compose up.
var previewDockerMaterial = []string{"KUAYLE_DOCKER_CA", "KUAYLE_DOCKER_CERT", "KUAYLE_DOCKER_HOST", "KUAYLE_DOCKER_KEY"}

// previewRunner is the fixed command line of a preview container. The preview
// argv arrives as positional arguments and is run as is, never interpolated
// into the script. It runs as a child, not as PID 1, so that stopping the
// container ends it at once: the kernel does not deliver an unhandled SIGTERM
// to PID 1, and most development servers install no handler. The run
// directory is removed afterwards.
const previewRunner = runTmpPrepare + `"$@" &
kuayle_child=$!
trap 'kill -TERM "$kuayle_child" 2>/dev/null' TERM INT
while :; do
  wait "$kuayle_child"
  kuayle_status=$?
  kill -0 "$kuayle_child" 2>/dev/null || break
done
[ -z "$kuayle_tmp" ] || kuayle_tmp_clean "$kuayle_tmp"
exit "$kuayle_status"`

// previewDockerPrelude starts a preview on a machine with a Docker daemon:
// the developer image's start functions wait for the scoped secrets, write
// the client certificate to the in-memory ~/.docker and export DOCKER_HOST,
// as the agent runner does. An image without them runs the command without
// the daemon.
const previewDockerPrelude = `if [ -r /usr/local/lib/kuayle/start-common.sh ]; then
  . /usr/local/lib/kuayle/start-common.sh
  load_scoped_secrets
  configure_docker_client || true
fi
unset KUAYLE_SECRET_NAMES
` + previewRunner

// previewStopTimeout is how many seconds a preview command gets to stop
// (docker compose up stops its containers) before it is killed.
const previewStopTimeout = 5

func previewContainerName(machine *domain.DevMachine, taskID uuid.UUID) string {
	return "kuayle-" + machine.RoutingKey + "-preview-" + taskID.String()
}

// previewRunTmpKey names a preview's directory for temporary files and caches
// on the workspace volume.
func previewRunTmpKey(taskID uuid.UUID) string {
	return "preview-" + taskID.String()
}

// previewTaskKey counts a preview among the tasks that split the agent share
// of the machine's memory (agentTaskCount).
func previewTaskKey(taskID string) string {
	return "preview:" + taskID
}

// stopPreviewScript stops a preview process that an earlier release started
// inside the developer container: its process group, forcefully after five
// seconds. Without such a process it does nothing. The container's sh is
// dash, whose kill rejects "--", so process groups are named as a plain
// negative PID.
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

// StartPreview (re)starts a task's preview command in its worktree, in the
// task's own preview container, listening on port. The container gets PORT,
// HOST and what a check container gets; the command is never executed in the
// developer container. docker is empty, or the Docker daemon client material
// of a machine with a daemon (previewDockerMaterial); nothing else is accepted.
func (r *DockerRuntime) StartPreview(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, taskID uuid.UUID, argv []string, port int, docker map[string]string) error {
	if machine == nil || machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || checkout == nil {
		return fmt.Errorf("preview runtime resources are unavailable")
	}
	if len(argv) == 0 || domain.ValidateTestCommand(argv) != nil || !domain.ValidAppPort(port) || taskID == uuid.Nil {
		return &terminalOperationError{code: "preview_invalid", message: "Preview command or port is invalid"}
	}
	for name := range docker {
		if !slices.Contains(previewDockerMaterial, name) {
			return &terminalOperationError{code: "preview_invalid", message: "A preview receives no secrets"}
		}
	}
	image := ""
	for _, service := range services {
		if isDeveloperService(service.ServiceType) {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return fmt.Errorf("developer environment is unavailable")
	}
	if machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, image, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return err
		}
	} else if err := r.ensureImage(ctx, image); err != nil {
		return err
	}
	// A restart replaces the task's previous preview, wherever it ran.
	if err := r.StopPreview(ctx, machine, services, taskID); err != nil {
		return err
	}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels[previewTaskLabel] = taskID.String()
	labels[previewPortLabel] = strconv.Itoa(port)
	hostConfig, err := r.agentHostConfig(ctx, machine, previewTaskKey(taskID.String()), hasDockerService(services))
	if err != nil {
		return err
	}
	if err := r.scopeMachineResources(ctx, machine, "agent", labels, hostConfig); err != nil {
		return err
	}
	env := []string{"HOME=/home/kuayle", "PORT=" + strconv.Itoa(port), "HOST=0.0.0.0"}
	env = append(env, proxyVariables(egressProxyURL(machine), "localhost,127.0.0.1")...)
	env = append(env, runTmpEnvironment(runTmpDir(previewRunTmpKey(taskID)))...)
	runner := previewRunner
	if len(docker) > 0 {
		names := slices.Sorted(maps.Keys(docker))
		env = append(env, fmt.Sprintf("KUAYLE_SECRET_NAMES=%s", strings.Join(names, ",")))
		runner = previewDockerPrelude
	}
	created, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: previewContainerName(machine, taskID),
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: checkout.WorkspacePath, Labels: labels, Env: env,
			Entrypoint: []string{"/bin/sh"}, Cmd: append([]string{"-c", runner, "kuayle-preview"}, argv...)},
		HostConfig: hostConfig,
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			*machine.DockerNetworkName: {Aliases: []string{domain.PreviewPortHost(machine.RoutingKey, port)}},
		}},
	})
	if err != nil {
		return err
	}
	remove := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = r.client.ContainerRemove(cleanupCtx, created.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	}
	if _, err := r.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		remove()
		return err
	}
	if len(docker) > 0 {
		if err := r.copySecrets(ctx, created.ID, docker); err != nil {
			remove()
			return err
		}
	}
	if previewStartGrace > 0 {
		select {
		case <-ctx.Done():
			remove()
			return ctx.Err()
		case <-time.After(previewStartGrace):
		}
	}
	inspection, err := r.client.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	if err != nil {
		remove()
		return err
	}
	if state := inspection.Container.State; state != nil && !state.Running && state.ExitCode != 0 {
		_, stderr, _ := r.containerLogs(ctx, created.ID)
		remove()
		return fmt.Errorf("preview command exited with code %d: %s", state.ExitCode, previewFailureLine(stderr))
	}
	return nil
}

// previewFailureLine returns the last non-empty line of output, at most 300 bytes.
func previewFailureLine(output string) string {
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}

// StopPreview removes one task's preview container and its temporary
// directory. It also stops a preview process an earlier release started
// inside the developer container. Other tasks' previews keep running.
func (r *DockerRuntime) StopPreview(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, taskID uuid.UUID) error {
	if machine == nil || taskID == uuid.Nil {
		return nil
	}
	name := previewContainerName(machine, taskID)
	result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	switch {
	case errdefs.IsNotFound(err):
	case err != nil:
		return err
	default:
		config := result.Container.Config
		if config == nil || !ownedByMachine(config.Labels, machine.ID) || config.Labels["com.kuayle.workspace-id"] != machine.WorkspaceID.String() || config.Labels[previewTaskLabel] != taskID.String() {
			return fmt.Errorf("preview container ownership does not match")
		}
		// The command is asked to stop first, then killed with its container.
		if result.Container.State != nil && result.Container.State.Running && !result.Container.State.Paused {
			timeout := previewStopTimeout
			if _, err = r.client.ContainerStop(ctx, result.Container.ID, client.ContainerStopOptions{Timeout: &timeout}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
				return err
			}
		}
		if _, err = r.client.ContainerRemove(ctx, result.Container.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !errdefs.IsNotFound(err) {
			return err
		}
	}
	developer := runningDeveloperContainerID(services)
	if developer == "" || machine.Status != domain.DevMachineStatusRunning {
		return nil
	}
	if _, err = r.execOutput(ctx, developer, []string{"/bin/sh", "-c", stopPreviewScript, "kuayle-preview-stop", taskID.String()}); err != nil {
		return err
	}
	if err = r.RemoveRunTmp(ctx, services, previewRunTmpKey(taskID)); err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.preview_stop"}).WithError(err).Debug("preview temporary directory was not removed")
	}
	return nil
}

// runningDeveloperContainerID returns the developer container when its
// service runs, or "".
func runningDeveloperContainerID(services []domain.DevMachineService) string {
	if !developerRunning(services) {
		return ""
	}
	return developerContainerID(services)
}

func previewContainer(item container.Summary) bool {
	return item.Labels[previewTaskLabel] != ""
}

// previewContainers lists the machine's preview containers, running or not.
func (r *DockerRuntime) previewContainers(ctx context.Context, machine *domain.DevMachine) ([]container.Summary, error) {
	listed, err := r.client.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", "com.kuayle.machine-id="+machine.ID.String()).Add("label", previewTaskLabel),
	})
	if err != nil {
		return nil, err
	}
	previews := make([]container.Summary, 0, len(listed.Items))
	for _, item := range listed.Items {
		if ownedByMachine(item.Labels, machine.ID) && item.Labels["com.kuayle.workspace-id"] == machine.WorkspaceID.String() && previewContainer(item) {
			previews = append(previews, item)
		}
	}
	return previews, nil
}

// RemovePreviews removes every preview container of the machine: previews end
// when the machine stops or its containers are provisioned again.
func (r *DockerRuntime) RemovePreviews(ctx context.Context, machine *domain.DevMachine) error {
	if machine == nil {
		return nil
	}
	return r.removeMachineContainers(ctx, machine, previewContainer)
}

// pausePreviews pauses (or resumes) the machine's preview containers with it.
func (r *DockerRuntime) pausePreviews(ctx context.Context, machine *domain.DevMachine, pause bool) error {
	if machine == nil {
		return nil
	}
	previews, err := r.previewContainers(ctx, machine)
	if err != nil {
		return err
	}
	for _, item := range previews {
		switch {
		case pause && item.State == container.StateRunning:
			_, err = r.client.ContainerPause(ctx, item.ID, client.ContainerPauseOptions{})
		case !pause && item.State == container.StatePaused:
			_, err = r.client.ContainerUnpause(ctx, item.ID, client.ContainerUnpauseOptions{})
		default:
			continue
		}
		if err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			return err
		}
	}
	return nil
}

// ScanPorts reads the developer container's listening ports and agent port
// requests, as the container's user, the ports published by the machine's
// Docker daemon and which running preview containers listen on their port.
func (r *DockerRuntime) ScanPorts(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) (PortScan, error) {
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
	scan.Preview = r.previewListening(ctx, machine)
	return scan, nil
}

// previewListening returns the ports of the machine's running preview
// containers that listen. A preview is asked only about its own port, so its
// process cannot report any other; a failed scan leaves a port unknown.
func (r *DockerRuntime) previewListening(ctx context.Context, machine *domain.DevMachine) []int {
	if machine == nil {
		return nil
	}
	previews, err := r.previewContainers(ctx, machine)
	if err != nil {
		return nil
	}
	listening := []int{}
	for _, item := range previews {
		port, ok := domain.ParseAppPortNumber(item.Labels[previewPortLabel])
		if !ok || item.State != container.StateRunning || len(listening) >= maxListeningPorts {
			continue
		}
		output, err := r.execOutput(ctx, item.ID, []string{"/bin/sh", "-c", dockerPortScanScript, "kuayle-port-scan"})
		if err == nil && slices.Contains(parsePortScan(output).Listening, port) && !slices.Contains(listening, port) {
			listening = append(listening, port)
		}
	}
	slices.Sort(listening)
	return listening
}

// dockerPortScanScript prints the LISTEN sockets of a container without
// taking port requests: the docker service container (the daemon's API port
// and the ports its containers publish) or a preview container.
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
