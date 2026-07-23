package machine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const egressNetworkName = "kuayle-machine-egress"

const maxCapturedLogBytes = 4 * 1024 * 1024

const customAgentBootstrap = `set -eu
attempt=0
while [ ! -f /run/kuayle-secrets/.ready ]; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 300 ] || exit 1
  sleep 0.1
done
old_ifs=$IFS
IFS=,
for secret_name in ${KUAYLE_SECRET_NAMES:-}; do
  case "$secret_name" in ''|*[!A-Za-z0-9_]*) exit 1 ;; esac
  secret_file="/run/kuayle-secrets/$secret_name"
  if [ -f "$secret_file" ]; then
    secret_value=$(cat "$secret_file"; printf x)
    secret_value=${secret_value%x}
    export "$secret_name=$secret_value"
    rm -f "$secret_file"
  fi
done
IFS=$old_ifs
rm -f /run/kuayle-secrets/.ready
home_dir=${HOME:-/home/kuayle}
credential_helper=${KUAYLE_GIT_CREDENTIAL_HELPER:-$home_dir/.kuayle-git-credential}
case "$credential_helper" in /*) ;; *) exit 1 ;; esac
helper_dir=${credential_helper%/*}
[ "$helper_dir" != "$credential_helper" ] || helper_dir=.
mkdir -p "$helper_dir"
old_umask=$(umask)
umask 077
cat > "$credential_helper" <<'SCRIPT'
#!/bin/sh
case "${1:-get}" in
  get ) ;;
  store|erase ) exit 0 ;;
  * ) exit 0 ;;
esac
if [ -z "${GITHUB_TOKEN:-}" ]; then
  echo "missing active GitHub token" >&2
  exit 1
fi
printf '%s\n' username=x-access-token
printf '%s\n' "password=$GITHUB_TOKEN"
SCRIPT
chmod 0700 "$credential_helper"
umask "$old_umask"
if command -v git >/dev/null 2>&1; then
  git config --global --unset-all core.askPass >/dev/null 2>&1 || true
  git config --global --unset-all credential.helper >/dev/null 2>&1 || true
  git config --global credential.helper "$credential_helper"
  export GIT_TERMINAL_PROMPT=0
fi
exec "$@"`

type DockerConfig struct {
	Host                 string
	GatewayContainerName string
	SeccompProfile       string
	AppArmorProfile      string
	PullImages           bool
	IngestURL            string
	EgressAllowlist      string
	EgressDenylist       string
}

type DockerRuntime struct {
	client *client.Client
	config DockerConfig
}

func NewDockerRuntime(config DockerConfig) (*DockerRuntime, error) {
	options := []client.Opt{client.WithAPIVersionNegotiation()}
	if config.Host != "" {
		options = append(options, client.WithHost(config.Host))
	} else {
		options = append(options, client.FromEnv)
	}
	dockerClient, err := client.NewClientWithOpts(options...)
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{client: dockerClient, config: config}, nil
}

func (r *DockerRuntime) Ping(ctx context.Context) error {
	_, err := r.client.Ping(ctx, client.PingOptions{})
	return err
}

func (r *DockerRuntime) Inspect(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) (RuntimeInspection, error) {
	networkName := "kuayle-machine-" + machine.RoutingKey
	if machine.DockerNetworkName != nil && *machine.DockerNetworkName != "" {
		networkName = *machine.DockerNetworkName
	}
	volumeName := "kuayle-workspace-" + machine.RoutingKey
	if machine.WorkspaceVolumeName != nil && *machine.WorkspaceVolumeName != "" {
		volumeName = *machine.WorkspaceVolumeName
	}
	result := RuntimeInspection{NetworkName: networkName, VolumeName: volumeName, Services: make(map[string]RuntimeServiceInspection, len(services))}
	networkInspection, err := r.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
	if err == nil {
		result.NetworkExists = true
		if r.config.GatewayContainerName != "" {
			if gateway, gatewayErr := r.client.ContainerInspect(ctx, r.config.GatewayContainerName, client.ContainerInspectOptions{}); gatewayErr == nil {
				_, result.GatewayAttached = networkInspection.Network.Containers[gateway.Container.ID]
			}
		}
	} else if !errdefs.IsNotFound(err) {
		return RuntimeInspection{}, err
	}
	if _, err := r.client.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{}); err == nil {
		result.VolumeExists = true
	} else if !errdefs.IsNotFound(err) {
		return RuntimeInspection{}, err
	}
	for _, service := range services {
		containerReference := service.ContainerName
		if service.ContainerID != nil && *service.ContainerID != "" {
			containerReference = *service.ContainerID
		}
		inspection, inspectErr := r.client.ContainerInspect(ctx, containerReference, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(inspectErr) && containerReference != service.ContainerName {
			inspection, inspectErr = r.client.ContainerInspect(ctx, service.ContainerName, client.ContainerInspectOptions{})
		}
		if errdefs.IsNotFound(inspectErr) {
			result.Services[service.ServiceKey] = RuntimeServiceInspection{}
			continue
		}
		if inspectErr != nil {
			return RuntimeInspection{}, inspectErr
		}
		health := string(inspection.Container.State.Status)
		if inspection.Container.State.Running {
			health = "healthy"
		}
		if inspection.Container.State.Health != nil {
			health = string(inspection.Container.State.Health.Status)
		}
		_, onNetwork := inspection.Container.NetworkSettings.Networks[networkName]
		result.Services[service.ServiceKey] = RuntimeServiceInspection{
			ContainerID: inspection.Container.ID, Status: string(inspection.Container.State.Status), HealthStatus: health,
			Exists: true, Running: inspection.Container.State.Running, Paused: inspection.Container.State.Paused, OnNetwork: onNetwork,
		}
	}
	return result, nil
}

func (r *DockerRuntime) Spawn(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, secrets map[string]map[string]string) (string, string, map[string]string, error) {
	networkName := "kuayle-machine-" + machine.RoutingKey
	volumeName := "kuayle-workspace-" + machine.RoutingKey
	labels := machineLabels(machine)
	containers := make(map[string]string, len(services))
	createdContainers := make(map[string]string, len(services))
	createdMachineNetworkConnections := make(map[string]string, len(services)+1)
	createdEgressNetworkConnections := make(map[string]string, 1)
	networkCreated := false
	volumeCreated := false
	completed := false
	defer func() {
		if completed {
			return
		}
		cleanup := planSpawnFailureCleanup(createdContainers, createdMachineNetworkConnections, networkCreated, volumeCreated)
		_ = r.removeContainers(context.Background(), cleanup.Containers)
		for _, containerID := range createdEgressNetworkConnections {
			_, _ = r.client.NetworkDisconnect(context.Background(), egressNetworkName, client.NetworkDisconnectOptions{Container: containerID, Force: true})
		}
		for _, containerID := range cleanup.NetworkConnections {
			_, _ = r.client.NetworkDisconnect(context.Background(), networkName, client.NetworkDisconnectOptions{Container: containerID, Force: true})
		}
		if cleanup.RemoveNetwork {
			_, _ = r.client.NetworkRemove(context.Background(), networkName, client.NetworkRemoveOptions{})
		}
		if cleanup.RemoveVolume {
			_, _ = r.client.VolumeRemove(context.Background(), volumeName, client.VolumeRemoveOptions{Force: true})
		}
	}()

	var err error
	if networkCreated, err = r.ensureNetwork(ctx, networkName, true, labels); err != nil {
		return "", "", nil, fmt.Errorf("create private network: %w", err)
	}
	if _, err := r.ensureNetwork(ctx, egressNetworkName, false, map[string]string{"com.kuayle.kind": "machine-egress"}); err != nil {
		return "", "", nil, fmt.Errorf("create egress network: %w", err)
	}
	if volumeCreated, err = r.ensureVolume(ctx, volumeName, labels); err != nil {
		return "", "", nil, fmt.Errorf("create workspace volume: %w", err)
	}
	var initImage string
	for _, service := range services {
		if service.ServiceType == "collector" {
			initImage = service.ImageRef
			break
		}
	}
	if err := r.prepareWorkspaceVolume(ctx, machine, volumeName, initImage); err != nil {
		return "", "", nil, fmt.Errorf("prepare workspace volume: %w", err)
	}
	if r.config.GatewayContainerName == "" {
		return "", "", nil, fmt.Errorf("gateway container name is required")
	}
	if connected, err := r.connectNetwork(ctx, networkName, r.config.GatewayContainerName, &network.EndpointSettings{Aliases: []string{"kuayle-gateway"}}); err != nil {
		return "", "", nil, fmt.Errorf("attach gateway to machine network: %w", err)
	} else if connected {
		createdMachineNetworkConnections["gateway"] = r.config.GatewayContainerName
	}

	terminalServices := make([]domain.DevMachineService, 0)
	for _, service := range services {
		if service.ServiceType == "terminal" {
			terminalServices = append(terminalServices, service)
			continue
		}
		containerID, containerCreated, networkAttached, err := r.ensureService(ctx, machine, service, networkName, volumeName, secrets[service.ServiceKey])
		if networkAttached {
			createdMachineNetworkConnections[service.ServiceKey] = containerID
		}
		if err != nil {
			return "", "", nil, fmt.Errorf("start %s: %w", service.ServiceKey, err)
		}
		recordSpawnServiceContainer(containers, createdContainers, service.ServiceKey, containerID, containerCreated)
		if service.ServiceType == "egress" {
			if connected, err := r.connectNetwork(ctx, egressNetworkName, containerID, &network.EndpointSettings{}); err != nil {
				return "", "", nil, fmt.Errorf("attach egress service: %w", err)
			} else if connected && !containerCreated {
				createdEgressNetworkConnections[service.ServiceKey] = containerID
			}
		}
	}
	for _, service := range terminalServices {
		containerID := containers["ide"]
		if containerID == "" {
			return "", "", nil, fmt.Errorf("start terminal: developer container is unavailable")
		}
		recordSpawnServiceContainer(containers, createdContainers, service.ServiceKey, containerID, false)
	}
	completed = true
	return networkName, volumeName, containers, nil
}

func (r *DockerRuntime) connectNetwork(ctx context.Context, networkName, containerID string, endpoint *network.EndpointSettings) (bool, error) {
	if r.containerOnNetwork(ctx, networkName, containerID) {
		return false, nil
	}
	_, err := r.client.NetworkConnect(ctx, networkName, client.NetworkConnectOptions{Container: containerID, EndpointConfig: endpoint})
	if err == nil {
		return true, nil
	}
	if errdefs.IsConflict(err) {
		return false, nil
	}
	if r.containerOnNetwork(ctx, networkName, containerID) {
		return false, nil
	}
	return false, err
}

func (r *DockerRuntime) containerOnNetwork(ctx context.Context, networkName, containerID string) bool {
	containerInspection, containerErr := r.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	networkInspection, networkErr := r.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
	if containerErr != nil || networkErr != nil {
		return false
	}
	_, connected := networkInspection.Network.Containers[containerInspection.Container.ID]
	return connected
}

func (r *DockerRuntime) prepareWorkspaceVolume(ctx context.Context, machine *domain.DevMachine, volumeName, imageRef string) error {
	if err := r.ensureImage(ctx, imageRef); err != nil {
		return err
	}
	containerName := "kuayle-" + machine.RoutingKey + "-volume-init"
	if inspection, err := r.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{}); err == nil {
		if inspection.Container.Config.Labels["com.kuayle.machine-id"] != machine.ID.String() {
			return fmt.Errorf("existing volume initializer is not managed by Kuayle")
		}
		if _, err := r.client.ContainerRemove(ctx, inspection.Container.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			return err
		}
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "volume-init"
	pidsLimit := int64(16)
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: imageRef, User: "0:0", Entrypoint: []string{"/bin/sh"}, Cmd: []string{"-c", "chown 1000:1000 /workspace"}, Labels: labels,
		},
		HostConfig: &container.HostConfig{
			NetworkMode: "none", ReadonlyRootfs: true, CapDrop: []string{"ALL"}, CapAdd: []string{"CHOWN"},
			SecurityOpt: []string{"no-new-privileges=true"},
			LogConfig:   boundedLogConfig(),
			Mounts: []mount.Mount{{
				Type: mount.TypeVolume, Source: volumeName, Target: "/workspace",
				VolumeOptions: &mount.VolumeOptions{NoCopy: true},
			}},
			Resources: container.Resources{Memory: 64 * 1024 * 1024, MemorySwap: 64 * 1024 * 1024, NanoCPUs: 100_000_000, PidsLimit: &pidsLimit},
		},
		Name: containerName,
	})
	if err != nil {
		return err
	}
	defer func() {
		_, _ = r.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	}()
	if _, err := r.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return err
	}
	waitResult := r.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-waitResult.Result:
		if result.StatusCode != 0 {
			_, stderr, _ := r.containerLogs(ctx, response.ID)
			return fmt.Errorf("volume initializer exited %d: %s", result.StatusCode, strings.TrimSpace(stderr))
		}
		return nil
	case err := <-waitResult.Error:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *DockerRuntime) Start(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, secrets map[string]map[string]string) error {
	for _, service := range plannedStartServices(machine.Status, services) {
		if service.ContainerID == nil || *service.ContainerID == "" {
			return fmt.Errorf("service %s has no container", service.ServiceKey)
		}
		inspection, err := r.client.ContainerInspect(ctx, *service.ContainerID, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		if err := r.ensureContainerStarted(ctx, *service.ContainerID, machine.Status, inspection.Container.State.Running, inspection.Container.State.Paused, secrets[service.ServiceKey]); err != nil {
			return err
		}
		if err := r.requireRunning(ctx, *service.ContainerID); err != nil {
			return fmt.Errorf("start %s: %w", service.ServiceKey, err)
		}
	}
	return nil
}

type containerStartAction int

const (
	containerStartNone containerStartAction = iota
	containerStartUnpause
	containerStartStart
	containerStartRestart
)

func plannedStartServices(machineStatus domain.DevMachineStatus, services []domain.DevMachineService) []domain.DevMachineService {
	planned := make([]domain.DevMachineService, 0, len(services))
	indexByContainer := make(map[string]int, len(services))
	for _, service := range services {
		if !startIncludesService(machineStatus, service) {
			continue
		}
		if service.ContainerID == nil || *service.ContainerID == "" {
			planned = append(planned, service)
			continue
		}
		containerID := *service.ContainerID
		if index, ok := indexByContainer[containerID]; ok {
			if preferStartService(service, planned[index]) {
				planned[index] = service
			}
			continue
		}
		indexByContainer[containerID] = len(planned)
		planned = append(planned, service)
	}
	return planned
}

func startIncludesService(machineStatus domain.DevMachineStatus, service domain.DevMachineService) bool {
	if service.ServiceType != "agent" {
		return true
	}
	return machineStatus == domain.DevMachineStatusPaused && (service.Status == "running" || service.Status == "paused")
}

func preferStartService(candidate, current domain.DevMachineService) bool {
	// The terminal endpoint shares the IDE container. Restart the shared
	// developer container once and install the IDE secret set that its
	// entrypoint was configured to consume.
	return current.ServiceType == "terminal" && candidate.ServiceType == "ide"
}

func serviceStartAction(machineStatus domain.DevMachineStatus, running, paused bool) containerStartAction {
	if machineStatus == domain.DevMachineStatusPaused {
		return containerStartRestart
	}
	if paused {
		return containerStartUnpause
	}
	if !running {
		return containerStartStart
	}
	return containerStartNone
}

func (r *DockerRuntime) ensureContainerStarted(ctx context.Context, containerID string, machineStatus domain.DevMachineStatus, running, paused bool, secrets map[string]string) error {
	switch serviceStartAction(machineStatus, running, paused) {
	case containerStartRestart:
		return r.restartContainerWithSecrets(ctx, containerID, running, paused, secrets)
	case containerStartUnpause:
		_, err := r.client.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{})
		return err
	case containerStartStart:
		return r.startContainerWithSecrets(ctx, containerID, secrets)
	default:
		return nil
	}
}

func (r *DockerRuntime) restartContainerWithSecrets(ctx context.Context, containerID string, running, paused bool, secrets map[string]string) error {
	if paused {
		if _, err := r.client.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{}); err != nil {
			return err
		}
		running = true
	}
	if running {
		if err := r.clearSecretFiles(ctx, containerID); err != nil {
			return err
		}
		timeout := 15
		if _, err := r.client.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			return err
		}
	}
	return r.startContainerWithSecrets(ctx, containerID, secrets)
}

func (r *DockerRuntime) startContainerWithSecrets(ctx context.Context, containerID string, secrets map[string]string) error {
	if _, err := r.client.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		return err
	}
	return r.copySecrets(ctx, containerID, secrets)
}

func (r *DockerRuntime) Pause(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService) error {
	for _, containerID := range uniqueContainerIDs(services) {
		if _, err := r.client.ContainerPause(ctx, containerID, client.ContainerPauseOptions{}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			return err
		}
	}
	return nil
}

func (r *DockerRuntime) Stop(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService) error {
	timeout := 15
	for _, containerID := range uniqueContainerIDs(services) {
		if _, err := r.client.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			return err
		}
	}
	return nil
}

func (r *DockerRuntime) Teardown(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) error {
	seen := make(map[string]bool, len(services))
	for _, service := range services {
		if service.ContainerID == nil {
			continue
		}
		if seen[*service.ContainerID] {
			continue
		}
		seen[*service.ContainerID] = true
		if err := r.removeManagedContainer(ctx, *service.ContainerID, machine.ID); err != nil {
			return err
		}
	}
	containers, err := r.client.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", "com.kuayle.machine-id="+machine.ID.String()),
	})
	if err != nil {
		return err
	}
	for _, item := range containers.Items {
		if item.Labels["com.kuayle.managed"] != "true" || item.Labels["com.kuayle.machine-id"] != machine.ID.String() {
			return fmt.Errorf("container %s is not owned by machine", item.ID)
		}
		if _, err := r.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !errdefs.IsNotFound(err) {
			return err
		}
	}
	networkName := "kuayle-machine-" + machine.RoutingKey
	if machine.DockerNetworkName != nil && *machine.DockerNetworkName != "" {
		networkName = *machine.DockerNetworkName
	}
	_, _ = r.client.NetworkDisconnect(ctx, networkName, client.NetworkDisconnectOptions{Container: r.config.GatewayContainerName, Force: true})
	if err := r.removeManagedNetwork(ctx, networkName, machine.ID); err != nil {
		return err
	}
	volumeName := "kuayle-workspace-" + machine.RoutingKey
	if machine.WorkspaceVolumeName != nil && *machine.WorkspaceVolumeName != "" {
		volumeName = *machine.WorkspaceVolumeName
	}
	if err := r.removeManagedVolume(ctx, volumeName, machine.ID); err != nil {
		return err
	}
	return nil
}

func (r *DockerRuntime) removeManagedContainer(ctx context.Context, containerID string, machineID uuid.UUID) error {
	inspection, err := r.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ownedByMachine(inspection.Container.Config.Labels, machineID) {
		return fmt.Errorf("container %s is not owned by machine", containerID)
	}
	if _, err := r.client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *DockerRuntime) removeManagedNetwork(ctx context.Context, networkName string, machineID uuid.UUID) error {
	inspection, err := r.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ownedByMachine(inspection.Network.Labels, machineID) {
		return fmt.Errorf("network %s is not owned by machine", networkName)
	}
	if _, err := r.client.NetworkRemove(ctx, networkName, client.NetworkRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *DockerRuntime) removeManagedVolume(ctx context.Context, volumeName string, machineID uuid.UUID) error {
	inspection, err := r.client.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ownedByMachine(inspection.Volume.Labels, machineID) {
		return fmt.Errorf("volume %s is not owned by machine", volumeName)
	}
	if _, err := r.client.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *DockerRuntime) RunAgent(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, checkout *domain.DevMachineCheckout, secrets map[string]string) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil {
		return nil, fmt.Errorf("machine runtime resources are unavailable")
	}
	if err := r.ensureImage(ctx, provider.ImageRef); err != nil {
		return nil, err
	}
	_, imageEnvironment, err := r.imageConfig(ctx, provider.ImageRef)
	if err != nil {
		return nil, err
	}
	var argv []string
	if err = json.Unmarshal(run.CommandArgv, &argv); err != nil || len(argv) == 0 {
		return nil, fmt.Errorf("invalid agent argv")
	}
	containerName := "kuayle-" + machine.RoutingKey + "-agent-" + run.ID.String()
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	secretNames := make([]string, 0, len(secrets))
	for name := range secrets {
		secretNames = append(secretNames, name)
	}
	hostConfig := r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName)
	entrypoint := []string{"/usr/local/bin/kuayle-agent-runner"}
	if provider.IsCustom {
		entrypoint = []string{"/bin/sh", "-c", customAgentBootstrap, "kuayle-agent"}
	}
	runtimeEnvironment := []string{
		"HOME=/home/kuayle", "HTTP_PROXY=http://" + machine.RoutingKey + "-egress:3128",
		"HTTPS_PROXY=http://" + machine.RoutingKey + "-egress:3128", "NO_PROXY=localhost,127.0.0.1," + machine.RoutingKey + "-collector",
		"KUAYLE_COLLECTOR_URL=http://" + machine.RoutingKey + "-collector:8091", "KUAYLE_SECRET_NAMES=" + strings.Join(secretNames, ","),
	}
	if inspection, inspectErr := r.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{}); inspectErr == nil {
		if inspection.Container.Config.Image != provider.ImageRef || inspection.Container.Config.Labels["com.kuayle.agent-run-id"] != run.ID.String() {
			return nil, fmt.Errorf("existing agent container does not match run")
		}
		return r.resumeAgent(ctx, inspection.Container, run.Mode == "interactive", secrets)
	} else if !errdefs.IsNotFound(inspectErr) {
		return nil, inspectErr
	}
	workingDirectory := "/workspace"
	if checkout != nil {
		workingDirectory = checkout.WorkspacePath
	}
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: provider.ImageRef, User: "1000:1000", WorkingDir: workingDirectory,
			Entrypoint: entrypoint, Cmd: argv,
			Env: mergeEnvironment(imageEnvironment, runtimeEnvironment...), Labels: labels,
			AttachStdout: true, AttachStderr: true, AttachStdin: run.Mode == "interactive",
			OpenStdin: run.Mode == "interactive", Tty: run.Mode == "interactive",
		},
		HostConfig: hostConfig,
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			*machine.DockerNetworkName: {Aliases: []string{"agent-" + run.ID.String()}},
		}},
		Name: containerName,
	})
	if err != nil {
		return nil, err
	}
	containerID := response.ID
	if _, err := r.client.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		_, _ = r.client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		return nil, err
	}
	if err := r.copySecrets(ctx, containerID, secrets); err != nil {
		_, _ = r.client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		return nil, err
	}
	inspection, err := r.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	return r.resumeAgent(ctx, inspection.Container, run.Mode == "interactive", nil)
}

func (r *DockerRuntime) resumeAgent(ctx context.Context, inspection container.InspectResponse, interactive bool, secrets map[string]string) (*AgentExecution, error) {
	containerID := inspection.ID
	if inspection.State.Paused {
		if _, err := r.client.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{}); err != nil {
			return nil, err
		}
		inspection.State.Running = true
	}
	if !inspection.State.Running && inspection.State.Status != "exited" && inspection.State.Status != "dead" {
		if _, err := r.client.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
			return nil, err
		}
		if err := r.copySecrets(ctx, containerID, secrets); err != nil {
			return nil, err
		}
		inspection.State.Running = true
	}
	if interactive && inspection.State.Running {
		if err := r.requireRunning(ctx, containerID); err != nil {
			return &AgentExecution{ContainerID: containerID, ExitCode: -1}, err
		}
		return &AgentExecution{ContainerID: containerID}, nil
	}
	if inspection.State.Running {
		waitResult := r.client.ContainerWait(ctx, containerID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
		select {
		case result := <-waitResult.Result:
			inspection.State.ExitCode = int(result.StatusCode)
		case err := <-waitResult.Error:
			return &AgentExecution{ContainerID: containerID, ExitCode: -1}, err
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, stopErr := r.client.ContainerStop(stopCtx, containerID, client.ContainerStopOptions{})
			cancel()
			if stopErr != nil && !errdefs.IsNotFound(stopErr) {
				return &AgentExecution{ContainerID: containerID, ExitCode: -1}, fmt.Errorf("stop timed out agent: %w", stopErr)
			}
			return &AgentExecution{ContainerID: containerID, ExitCode: -1}, ctx.Err()
		}
	}
	stdout, stderr, err := r.containerLogs(ctx, containerID)
	if err != nil {
		return &AgentExecution{ContainerID: containerID, ExitCode: inspection.State.ExitCode}, err
	}
	return &AgentExecution{ContainerID: containerID, Stdout: stdout, Stderr: stderr, ExitCode: inspection.State.ExitCode}, nil
}

func (r *DockerRuntime) CancelAgent(ctx context.Context, run *domain.DevMachineAgentRun) error {
	containers, err := r.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	for _, item := range containers.Items {
		if item.Labels["com.kuayle.agent-run-id"] != run.ID.String() {
			continue
		}
		if item.Labels["com.kuayle.managed"] != "true" {
			return fmt.Errorf("agent container %s is not managed by Kuayle", item.ID)
		}
		_, err := r.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		return err
	}
	return nil
}

func (r *DockerRuntime) Stats(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService) (ResourceUsage, error) {
	var usage ResourceUsage
	var collectorID string
	seen := make(map[string]bool, len(services))
	for _, service := range services {
		if service.ContainerID == nil || service.Status != "running" {
			continue
		}
		if seen[*service.ContainerID] {
			continue
		}
		seen[*service.ContainerID] = true
		if service.ServiceType == "collector" {
			collectorID = *service.ContainerID
		}
		statsReader, err := r.client.ContainerStats(ctx, *service.ContainerID, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			if errdefs.IsNotFound(err) {
				continue
			}
			return usage, err
		}
		var stats container.StatsResponse
		err = json.NewDecoder(statsReader.Body).Decode(&stats)
		statsReader.Body.Close()
		if err != nil {
			return usage, err
		}
		cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
		systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
		if cpuDelta > 0 && systemDelta > 0 {
			usage.CPUPercent += cpuDelta / systemDelta * float64(stats.CPUStats.OnlineCPUs) * 100
		}
		usage.MemoryBytes += int64(stats.MemoryStats.Usage)
		usage.Pids += int(stats.PidsStats.Current)
		for _, networkStats := range stats.Networks {
			usage.NetworkRxBytes += int64(networkStats.RxBytes)
			usage.NetworkTxBytes += int64(networkStats.TxBytes)
		}
	}
	if collectorID != "" {
		if disk, err := r.workspaceSize(ctx, collectorID); err == nil {
			usage.DiskBytes = disk
		}
	}
	return usage, nil
}

func (r *DockerRuntime) GitState(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) (GitState, error) {
	var ideContainer string
	for _, service := range services {
		if service.ServiceType == "ide" && service.ContainerID != nil {
			ideContainer = *service.ContainerID
			break
		}
	}
	if ideContainer == "" {
		return GitState{}, fmt.Errorf("IDE container is unavailable")
	}
	workspacePath, baseBranch := "/workspace", machine.BaseBranch
	if checkout != nil {
		workspacePath, baseBranch = checkout.WorkspacePath, checkout.BaseBranch
	}
	branch, err := r.execOutput(ctx, ideContainer, []string{"git", "-C", workspacePath, "branch", "--show-current"})
	if err != nil {
		return GitState{}, err
	}
	commits, _ := r.execOutput(ctx, ideContainer, []string{"git", "-C", workspacePath, "rev-list", "origin/" + baseBranch + "..HEAD"})
	changed, _ := r.execOutput(ctx, ideContainer, []string{"git", "-C", workspacePath, "diff", "--name-only", "origin/" + baseBranch + "...HEAD"})
	return GitState{Branch: strings.TrimSpace(branch), Commits: nonEmptyLines(commits), ChangedFiles: nonEmptyLines(changed)}, nil
}

func (r *DockerRuntime) PrepareCheckout(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, token string) error {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return fmt.Errorf("developer container is unavailable")
	}
	if token == "" {
		return fmt.Errorf("repository credential is unavailable")
	}
	if err := r.execWithInput(ctx, developerContainer, []string{
		"/usr/local/bin/kuayle-checkout", checkout.RepositoryFullName, checkout.BaseBranch,
		checkout.WorkingBranch, checkout.WorkspacePath,
	}, token+"\n"); err != nil {
		return err
	}
	return nil
}

func (r *DockerRuntime) TerminateTerminal(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, session *domain.DevMachineTerminalSession) (resultErr error) {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		if machine.Status == domain.DevMachineStatusStopped || machine.Status == domain.DevMachineStatusDestroyed || machine.Status == domain.DevMachineStatusTearingDown {
			return nil
		}
		return fmt.Errorf("developer container is unavailable")
	}
	inspection, err := r.client.ContainerInspect(ctx, developerContainer, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !inspection.Container.State.Running {
		return nil
	}
	if inspection.Container.State.Paused {
		if _, err := r.client.ContainerUnpause(ctx, developerContainer, client.ContainerUnpauseOptions{}); err != nil {
			return err
		}
		defer func() {
			if _, err := r.client.ContainerPause(context.Background(), developerContainer, client.ContainerPauseOptions{}); err != nil && !errdefs.IsNotFound(err) && resultErr == nil {
				resultErr = fmt.Errorf("restore paused developer container: %w", err)
			}
		}()
	}
	if _, err := r.execOutput(ctx, developerContainer, []string{"/usr/local/bin/kuayle-terminal-session", "--close", session.RuntimeSessionName}); err != nil {
		return fmt.Errorf("close tmux session: %w", err)
	}
	return nil
}

func (r *DockerRuntime) SnapshotEnvironment(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, environment *domain.DevMachineEnvironment) (string, error) {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return "", fmt.Errorf("developer container is unavailable")
	}
	inspection, err := r.client.ContainerInspect(ctx, developerContainer, client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	wasPaused, wasRunning := inspection.Container.State.Paused, inspection.Container.State.Running
	if wasPaused {
		if _, err := r.client.ContainerUnpause(ctx, developerContainer, client.ContainerUnpauseOptions{}); err != nil {
			return "", err
		}
	} else if !wasRunning {
		if _, err := r.client.ContainerStart(ctx, developerContainer, client.ContainerStartOptions{}); err != nil {
			return "", err
		}
	}
	restoreState := func() {
		if wasPaused {
			_, _ = r.client.ContainerPause(context.Background(), developerContainer, client.ContainerPauseOptions{})
		} else if !wasRunning {
			timeout := 10
			_, _ = r.client.ContainerStop(context.Background(), developerContainer, client.ContainerStopOptions{Timeout: &timeout})
		}
	}
	defer restoreState()
	_, err = r.execOutputAs(ctx, developerContainer, []string{"/bin/sh", "-c", `set -eu
rm -rf /opt/kuayle-home-template
mkdir -p /opt/kuayle-home-template/.config /opt/kuayle-home-template/.local/share/code-server
for file in .bashrc .profile; do [ ! -f "/home/kuayle/$file" ] || cp -a "/home/kuayle/$file" "/opt/kuayle-home-template/$file"; done
[ ! -d /home/kuayle/.config/code-server ] || cp -a /home/kuayle/.config/code-server /opt/kuayle-home-template/.config/
[ ! -d /home/kuayle/.local/share/code-server/extensions ] || cp -a /home/kuayle/.local/share/code-server/extensions /opt/kuayle-home-template/.local/share/code-server/
find /opt/kuayle-home-template -type f \( -name '*history*' -o -name '*token*' -o -name '*credential*' \) -delete
chown -R 1000:1000 /opt/kuayle-home-template`}, "0:0")
	if err != nil {
		return "", fmt.Errorf("prepare environment home template: %w", err)
	}
	restoreState()
	response, err := r.client.ContainerCommit(ctx, developerContainer, client.ContainerCommitOptions{
		Reference: environment.ImageRef, Comment: "Kuayle Development Environment " + environment.Name,
		Author: "Kuayle", NoPause: true,
		Changes: []string{
			"LABEL com.kuayle.managed=true",
			"LABEL com.kuayle.kind=dev-machine-environment",
			"LABEL com.kuayle.environment-id=" + environment.ID.String(),
			"LABEL com.kuayle.workspace-id=" + environment.WorkspaceID.String(),
			"LABEL com.kuayle.source-machine-id=" + machine.ID.String(),
			"LABEL com.kuayle.machine-id=",
			"LABEL com.kuayle.routing-key=",
			"LABEL com.kuayle.service=",
		},
	})
	if err != nil {
		return "", err
	}
	return response.ID, nil
}

func (r *DockerRuntime) DeleteEnvironmentImage(ctx context.Context, environment *domain.DevMachineEnvironment) error {
	reference := environmentImageCleanupReference(environment)
	if reference == "" {
		return nil
	}
	inspection, err := r.client.ImageInspect(ctx, reference)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	labels := map[string]string{}
	if inspection.Config != nil && inspection.Config.Labels != nil {
		labels = inspection.Config.Labels
	}
	if err := validateEnvironmentImageLabels(labels, environment.WorkspaceID, environment.ID); err != nil {
		return fmt.Errorf("environment image %s is not owned by Kuayle: %w", reference, err)
	}
	_, err = r.client.ImageRemove(ctx, reference, client.ImageRemoveOptions{Force: true, PruneChildren: true})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func developerContainerID(services []domain.DevMachineService) string {
	for _, service := range services {
		if service.ServiceType == "ide" && service.ContainerID != nil {
			return *service.ContainerID
		}
	}
	return ""
}

func uniqueContainerIDs(services []domain.DevMachineService) []string {
	ids := make([]string, 0, len(services))
	seen := make(map[string]bool, len(services))
	for _, service := range services {
		if service.ContainerID == nil || *service.ContainerID == "" || seen[*service.ContainerID] {
			continue
		}
		seen[*service.ContainerID] = true
		ids = append(ids, *service.ContainerID)
	}
	return ids
}

func (r *DockerRuntime) ensureService(ctx context.Context, machine *domain.DevMachine, service domain.DevMachineService, networkName, volumeName string, secrets map[string]string) (containerID string, containerCreated bool, networkAttached bool, err error) {
	inspection, err := r.client.ContainerInspect(ctx, service.ContainerName, client.ContainerInspectOptions{})
	if err == nil {
		if inspection.Container.Config.Image != service.ImageRef || inspection.Container.Config.Labels["com.kuayle.machine-id"] != machine.ID.String() || inspection.Container.Config.Labels["com.kuayle.service"] != service.ServiceType {
			return "", false, false, fmt.Errorf("existing container does not match service")
		}
		containerID = inspection.Container.ID
		networkAttached, err = r.connectNetwork(ctx, networkName, inspection.Container.ID, &network.EndpointSettings{Aliases: []string{service.InternalHost}})
		if err != nil {
			return containerID, false, networkAttached, err
		}
		if err := r.ensureContainerStarted(ctx, inspection.Container.ID, machine.Status, inspection.Container.State.Running, inspection.Container.State.Paused, secrets); err != nil {
			return containerID, false, networkAttached, err
		}
		if err := r.requireRunning(ctx, inspection.Container.ID); err != nil {
			return containerID, false, networkAttached, err
		}
		return containerID, false, networkAttached, nil
	}
	if !errdefs.IsNotFound(err) {
		return "", false, false, err
	}
	if service.ServiceType == "ide" && machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, service.ImageRef, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return "", false, false, err
		}
	} else {
		if err := r.ensureImage(ctx, service.ImageRef); err != nil {
			return "", false, false, err
		}
	}
	command, imageEnvironment, err := r.imageConfig(ctx, service.ImageRef)
	if err != nil {
		return "", false, false, err
	}
	if len(command) == 0 {
		return "", false, false, fmt.Errorf("image %s has no default command", service.ImageRef)
	}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = service.ServiceType
	environment := []string{"KUAYLE_MACHINE_ID=" + machine.ID.String()}
	secretNames := make([]string, 0, len(secrets))
	for name := range secrets {
		secretNames = append(secretNames, name)
	}
	environment = append(environment, "KUAYLE_SECRET_NAMES="+strings.Join(secretNames, ","))
	if service.ServiceType != "egress" {
		environment = append(environment, "HTTP_PROXY=http://"+machine.RoutingKey+"-egress:3128", "HTTPS_PROXY=http://"+machine.RoutingKey+"-egress:3128", "NO_PROXY=localhost,127.0.0.1,"+machine.RoutingKey+"-collector,"+machine.RoutingKey+"-ide,"+machine.RoutingKey+"-browser")
		environment = append(environment, "KUAYLE_COLLECTOR_URL=http://"+machine.RoutingKey+"-collector:8091")
	}
	if service.ServiceType == "ide" {
		environment = append(environment, "KUAYLE_REPO_URL="+machine.RepoURL, "KUAYLE_BASE_BRANCH="+machine.BaseBranch, "KUAYLE_WORKING_BRANCH="+machine.WorkingBranch)
		environment = append(environment, "SHELL=/usr/local/bin/kuayle-shell")
	}
	if service.ServiceType == "collector" {
		environment = append(environment, "KUAYLE_INGEST_URL="+r.config.IngestURL)
		environment = append(environment, "KUAYLE_BROWSER_CDP_URL=http://"+machine.RoutingKey+"-browser:9222")
	}
	if service.ServiceType == "egress" {
		environment = append(environment, "KUAYLE_EGRESS_ALLOWLIST="+r.config.EgressAllowlist, "KUAYLE_EGRESS_DENYLIST="+r.config.EgressDenylist)
	}
	containerConfig := &container.Config{
		Image: service.ImageRef, User: "1000:1000", WorkingDir: "/workspace", Env: append(imageEnvironment, environment...), Labels: labels,
		Entrypoint: []string{"/usr/local/bin/kuayle-service-entrypoint"}, Cmd: command,
	}
	if service.ServiceType == "ide" {
		containerConfig.Healthcheck = &container.HealthConfig{Test: []string{"CMD", "test", "-f", "/workspace/.kuayle-ready"}, Interval: time.Second, Timeout: time.Second, Retries: 300}
	} else if service.ServiceType == "browser" {
		containerConfig.Healthcheck = &container.HealthConfig{Test: []string{"CMD", "/usr/local/bin/kuayle-browser-healthcheck"}, Interval: 10 * time.Second, Timeout: 2 * time.Second, StartPeriod: 10 * time.Second, Retries: 6}
	}
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           containerConfig,
		HostConfig:       r.secureHostConfig(machine, service.ServiceType, networkName, volumeName),
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {Aliases: []string{service.InternalHost}}}},
		Name:             service.ContainerName,
	})
	if err != nil {
		return "", false, false, err
	}
	completed := false
	defer func() {
		if !completed {
			_, _ = r.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		}
	}()
	if _, err := r.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return "", false, false, err
	}
	if err := r.copySecrets(ctx, response.ID, secrets); err != nil {
		return "", false, false, err
	}
	if err := r.requireRunning(ctx, response.ID); err != nil {
		return "", false, false, err
	}
	completed = true
	return response.ID, true, false, nil
}

func (r *DockerRuntime) secureHostConfig(machine *domain.DevMachine, serviceType, networkName, volumeName string) *container.HostConfig {
	fraction := int64(5)
	switch serviceType {
	case "ide":
		fraction = 25
	case "browser":
		fraction = 25
	case "agent":
		fraction = 35
	}
	memory := int64(machine.MemoryMB) * 1024 * 1024 * fraction / 100
	if memory < 128*1024*1024 {
		memory = 128 * 1024 * 1024
	}
	pids := int64(machine.PidsLimit) * fraction / 100
	if serviceType == "browser" && pids < 256 {
		pids = 256
	} else if pids < 32 {
		pids = 32
	}
	securityOptions := []string{"no-new-privileges=true"}
	if r.config.SeccompProfile != "" && r.config.SeccompProfile != "default" {
		securityOptions = append(securityOptions, "seccomp="+r.config.SeccompProfile)
	}
	if r.config.AppArmorProfile != "" && r.config.AppArmorProfile != "default" {
		securityOptions = append(securityOptions, "apparmor="+r.config.AppArmorProfile)
	}
	workspaceReadOnly := serviceType == "collector" || serviceType == "browser" || serviceType == "egress"
	mounts := []mount.Mount{}
	if serviceType != "browser" && serviceType != "egress" {
		mounts = append(mounts, mount.Mount{
			Type: mount.TypeVolume, Source: volumeName, Target: "/workspace", ReadOnly: workspaceReadOnly,
			VolumeOptions: &mount.VolumeOptions{NoCopy: true},
		})
	}
	tmpOptions := "rw,noexec,nosuid,uid=1000,gid=1000,size=256m"
	if serviceType == "agent" || serviceType == "ide" {
		// Compilers and OpenCode's native TUI renderer execute from temporary storage.
		tmpOptions = "rw,exec,nosuid,uid=1000,gid=1000,size=256m"
	}
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(networkName), ReadonlyRootfs: true, Privileged: false,
		CapDrop: []string{"ALL"}, SecurityOpt: securityOptions,
		LogConfig: boundedLogConfig(),
		Tmpfs: map[string]string{
			"/tmp":                tmpOptions,
			"/run/kuayle-secrets": "rw,noexec,nosuid,uid=1000,gid=1000,mode=0700,size=8m",
			"/home/kuayle":        "rw,nosuid,uid=1000,gid=1000,size=512m",
		},
		Mounts: mounts,
		Resources: container.Resources{
			NanoCPUs: int64(machine.CPUMillis) * 1_000_000 * fraction / 100,
			Memory:   memory, MemorySwap: memory, PidsLimit: &pids,
		},
		ShmSize: 64 * 1024 * 1024,
	}
	if machine.EnvironmentBuilder && serviceType == "ide" {
		hostConfig.ReadonlyRootfs = false
		hostConfig.CapDrop = nil
		hostConfig.SecurityOpt = securityOptions[1:]
	}
	if serviceType == "browser" {
		hostConfig.ShmSize = 512 * 1024 * 1024
	}
	return hostConfig
}

func boundedLogConfig() container.LogConfig {
	return container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}}
}

func (r *DockerRuntime) ensureImage(ctx context.Context, reference string) error {
	if reference == "" {
		return fmt.Errorf("container image is not configured")
	}
	if _, err := r.client.ImageInspect(ctx, reference); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	if err := missingImageError(reference, r.config.PullImages); err != nil {
		return err
	}
	reader, err := r.client.ImagePull(ctx, reference, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = io.Copy(io.Discard, reader)
	return err
}

func (r *DockerRuntime) ensureEnvironmentImage(ctx context.Context, reference string, workspaceID, environmentID uuid.UUID) error {
	if !isImmutableLocalImageID(reference) {
		return fmt.Errorf("%w: environment image reference %q is not an immutable local image ID", ErrLocalEnvironmentMissing, reference)
	}
	if err := r.ensureImage(ctx, reference); err != nil {
		return err
	}
	inspection, err := r.client.ImageInspect(ctx, reference)
	if err != nil {
		return err
	}
	labels := map[string]string{}
	if inspection.Config != nil && inspection.Config.Labels != nil {
		labels = inspection.Config.Labels
	}
	if err := validateEnvironmentImageLabels(labels, workspaceID, environmentID); err != nil {
		return fmt.Errorf("environment image %s is not valid for this machine: %w", reference, err)
	}
	return nil
}

func (r *DockerRuntime) imageConfig(ctx context.Context, reference string) ([]string, []string, error) {
	inspection, err := r.client.ImageInspect(ctx, reference)
	if err != nil {
		return nil, nil, err
	}
	if inspection.Config == nil {
		return nil, nil, fmt.Errorf("image %s has no configuration", reference)
	}
	if len(inspection.Config.Volumes) > 0 {
		return nil, nil, fmt.Errorf("image %s declares unmanaged volumes", reference)
	}
	return append([]string(nil), inspection.Config.Cmd...), append([]string(nil), inspection.Config.Env...), nil
}

func mergeEnvironment(base []string, overrides ...string) []string {
	overridden := make(map[string]bool, len(overrides))
	for _, item := range overrides {
		if index := strings.IndexByte(item, '='); index > 0 {
			overridden[item[:index]] = true
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		if index := strings.IndexByte(item, '='); index > 0 && overridden[item[:index]] {
			continue
		}
		result = append(result, item)
	}
	return append(result, overrides...)
}

func (r *DockerRuntime) ensureNetwork(ctx context.Context, name string, internal bool, labels map[string]string) (bool, error) {
	if inspection, err := r.client.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); err == nil {
		if inspection.Network.Internal != internal {
			return false, fmt.Errorf("existing network %s has incompatible isolation", name)
		}
		for key, value := range labels {
			if inspection.Network.Labels[key] != value {
				return false, fmt.Errorf("existing network %s is not managed by Kuayle", name)
			}
		}
		return false, nil
	} else if !errdefs.IsNotFound(err) {
		return false, err
	}
	_, err := r.client.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge", Internal: internal, Labels: labels})
	return err == nil, err
}

func (r *DockerRuntime) ensureVolume(ctx context.Context, name string, labels map[string]string) (bool, error) {
	if inspection, err := r.client.VolumeInspect(ctx, name, client.VolumeInspectOptions{}); err == nil {
		for key, value := range labels {
			if inspection.Volume.Labels[key] != value {
				return false, fmt.Errorf("existing volume %s is not managed by Kuayle", name)
			}
		}
		return false, nil
	} else if !errdefs.IsNotFound(err) {
		return false, err
	}
	_, err := r.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: labels})
	return err == nil, err
}

func (r *DockerRuntime) requireRunning(ctx context.Context, containerID string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("container readiness timed out")
		case <-ticker.C:
			inspection, err := r.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
			if err != nil {
				return err
			}
			if inspection.Container.State.Running && (inspection.Container.State.Health == nil || inspection.Container.State.Health.Status == "healthy") {
				return nil
			}
			if inspection.Container.State.Running {
				continue
			}
			_, stderr, logErr := r.containerLogs(ctx, containerID)
			if logErr != nil {
				stderr = inspection.Container.State.Error
			}
			return fmt.Errorf("container exited with code %d: %s", inspection.Container.State.ExitCode, strings.TrimSpace(stderr))
		}
	}
}

func (r *DockerRuntime) copySecrets(ctx context.Context, containerID string, secrets map[string]string) error {
	return installSecrets(ctx, containerID, secrets, r.clearSecretFiles, r.writeSecretFile)
}

type secretFileClearFunc func(context.Context, string) error
type secretFileWriteFunc func(context.Context, string, string, string) error

func installSecrets(ctx context.Context, containerID string, secrets map[string]string, clear secretFileClearFunc, write secretFileWriteFunc) error {
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		if name == ".ready" || !validSecretName(name) {
			return fmt.Errorf("invalid secret name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if err := clear(ctx, containerID); err != nil {
		return err
	}
	for _, name := range names {
		if err := write(ctx, containerID, name, secrets[name]); err != nil {
			return err
		}
	}
	return write(ctx, containerID, ".ready", "")
}

func (r *DockerRuntime) clearSecretFiles(ctx context.Context, containerID string) error {
	_, err := r.execOutput(ctx, containerID, []string{"/bin/sh", "-c", "rm -rf /run/kuayle-secrets/* /run/kuayle-secrets/.[!.]* /run/kuayle-secrets/..?*"})
	if err != nil {
		return fmt.Errorf("clear scoped secrets: %w", err)
	}
	return nil
}

func (r *DockerRuntime) writeSecretFile(ctx context.Context, containerID, name, value string) error {
	created, err := r.client.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		User: "1000:1000", AttachStdin: true, Cmd: []string{"/bin/sh", "-c", "umask 077; cat > /run/kuayle-secrets/" + name},
	})
	if err != nil {
		return err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attached.Close()
	if _, err := attached.Conn.Write([]byte(value)); err != nil {
		return err
	}
	if err := attached.CloseWrite(); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, attached.Reader); err != nil {
		return err
	}
	inspection, err := r.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if inspection.ExitCode != 0 {
		return fmt.Errorf("write scoped secret: exec exited %d", inspection.ExitCode)
	}
	return nil
}

func validSecretName(name string) bool {
	if name == ".ready" {
		return true
	}
	if name == "" {
		return false
	}
	for _, character := range name {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func (r *DockerRuntime) containerLogs(ctx context.Context, containerID string) (string, string, error) {
	reader, err := r.client.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", "", err
	}
	defer reader.Close()
	stdout := newCappedBuffer(maxCapturedLogBytes)
	stderr := newCappedBuffer(maxCapturedLogBytes)
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		return "", "", err
	}
	return stdout.String(), stderr.String(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	remaining int
}

func newCappedBuffer(limit int) cappedBuffer {
	return cappedBuffer{remaining: limit}
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	if len(value) > b.remaining {
		value = value[:b.remaining]
	}
	if len(value) > 0 {
		_, _ = b.Buffer.Write(value)
		b.remaining -= len(value)
	}
	return originalLength, nil
}

func (r *DockerRuntime) workspaceSize(ctx context.Context, collectorID string) (int64, error) {
	output, err := r.execOutput(ctx, collectorID, []string{"du", "-sb", "/workspace"})
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return 0, fmt.Errorf("workspace size unavailable")
	}
	return strconv.ParseInt(fields[0], 10, 64)
}

func (r *DockerRuntime) execOutput(ctx context.Context, containerID string, command []string) (string, error) {
	return r.execOutputAs(ctx, containerID, command, "1000:1000")
}

func (r *DockerRuntime) execOutputAs(ctx context.Context, containerID string, command []string, user string) (string, error) {
	created, err := r.client.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		User: user, AttachStdout: true, AttachStderr: true, Cmd: command,
	})
	if err != nil {
		return "", err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", err
	}
	defer attached.Close()
	stdout := newCappedBuffer(maxCapturedLogBytes)
	stderr := newCappedBuffer(maxCapturedLogBytes)
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); err != nil {
		return "", err
	}
	inspection, err := r.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return "", err
	}
	if inspection.ExitCode != 0 {
		return "", fmt.Errorf("exec failed: %s", strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r *DockerRuntime) execWithInput(ctx context.Context, containerID string, command []string, input string) error {
	created, err := r.client.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd: command, User: "1000:1000", AttachStdin: true, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attached.Close()
	if _, err := io.WriteString(attached.Conn, input); err != nil {
		return err
	}
	if err := attached.CloseWrite(); err != nil {
		return err
	}
	stdout, stderr := newCappedBuffer(maxCapturedLogBytes), newCappedBuffer(maxCapturedLogBytes)
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	inspection, err := r.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if inspection.ExitCode != 0 {
		return fmt.Errorf("checkout command failed with exit code %d: %s", inspection.ExitCode, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type spawnFailureCleanup struct {
	Containers         map[string]string
	NetworkConnections map[string]string
	RemoveNetwork      bool
	RemoveVolume       bool
}

func recordSpawnServiceContainer(containers, createdContainers map[string]string, serviceKey, containerID string, created bool) {
	if containerID == "" {
		return
	}
	containers[serviceKey] = containerID
	if created {
		createdContainers[serviceKey] = containerID
	}
}

func planSpawnFailureCleanup(createdContainers, createdNetworkConnections map[string]string, networkCreated, volumeCreated bool) spawnFailureCleanup {
	cleanup := spawnFailureCleanup{
		Containers:         make(map[string]string, len(createdContainers)),
		NetworkConnections: make(map[string]string, len(createdNetworkConnections)),
		RemoveNetwork:      networkCreated,
		RemoveVolume:       volumeCreated,
	}
	for key, containerID := range createdContainers {
		if containerID != "" {
			cleanup.Containers[key] = containerID
		}
	}
	for key, containerID := range createdNetworkConnections {
		if containerID != "" {
			cleanup.NetworkConnections[key] = containerID
		}
	}
	return cleanup
}

func (r *DockerRuntime) removeContainers(ctx context.Context, containers map[string]string) error {
	seen := make(map[string]bool, len(containers))
	for _, containerID := range containers {
		if containerID == "" || seen[containerID] {
			continue
		}
		seen[containerID] = true
		_, _ = r.client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	}
	return nil
}

func machineLabels(machine *domain.DevMachine) map[string]string {
	return map[string]string{
		"com.kuayle.managed":      "true",
		"com.kuayle.machine-id":   machine.ID.String(),
		"com.kuayle.workspace-id": machine.WorkspaceID.String(),
		"com.kuayle.routing-key":  machine.RoutingKey,
	}
}

func ownedByMachine(labels map[string]string, machineID uuid.UUID) bool {
	return labels != nil && labels["com.kuayle.managed"] == "true" && labels["com.kuayle.machine-id"] == machineID.String()
}

func nonEmptyLines(value string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}
