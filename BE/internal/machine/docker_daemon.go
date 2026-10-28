package machine

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// The machine's Docker daemon is a rootless dockerd in its own container, the
// "docker" service: on the machine network only (egress through the machine's
// egress proxy), reachable as "docker" and "<routing key>-docker" with mutual
// TLS on dockerDaemonPort, its data on a dedicated volume and the workspace
// volume at /workspace, so build contexts and bind mounts use the same paths
// as the developer container. Containers it runs live in its namespaces;
// their published ports listen in the daemon container, where the gateway
// reaches them as app ports.
const (
	dockerDaemonPort = 2376
	// dockerDataPath is rootless dockerd's data root in the daemon image.
	dockerDataPath = "/home/rootless/.local/share/docker"
	// dockerShare is the part of the machine's CPU and memory the daemon and
	// its containers share.
	dockerShare = 40
	// dockerMinimumPids leaves room for dockerd, containerd, BuildKit and a
	// compose stack; the container's limit still bounds a fork bomb.
	dockerMinimumPids = 1024
)

func hasDockerService(services []domain.DevMachineService) bool {
	for _, service := range services {
		if service.ServiceType == "docker" {
			return true
		}
	}
	return false
}

func dockerVolumeName(machine *domain.DevMachine) string {
	return "kuayle-docker-" + machine.RoutingKey
}

func egressProxyURL(machine *domain.DevMachine) string {
	return "http://" + machine.RoutingKey + "-egress:3128"
}

// proxyEnvironment points a container at the machine's egress proxy. The
// daemon, when the machine has one, is reached directly.
func proxyEnvironment(machine *domain.DevMachine, docker bool) []string {
	noProxy := "localhost,127.0.0.1," + machine.RoutingKey + "-collector," + machine.RoutingKey + "-ide," + machine.RoutingKey + "-terminal," + machine.RoutingKey + "-browser"
	if docker {
		noProxy += ",docker," + machine.RoutingKey + "-docker"
	}
	return []string{"HTTP_PROXY=" + egressProxyURL(machine), "HTTPS_PROXY=" + egressProxyURL(machine), "NO_PROXY=" + noProxy}
}

// dockerClientEnvironment points the Docker CLI of a developer container at
// the machine's daemon. The start scripts write the client certificates to
// DOCKER_CERT_PATH from the container's scoped secrets.
func dockerClientEnvironment(machine *domain.DevMachine) []string {
	return []string{
		"DOCKER_HOST=tcp://" + machine.RoutingKey + "-docker:" + strconv.Itoa(dockerDaemonPort),
		"DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH=/home/kuayle/.docker",
	}
}

// dockerDaemonEnvironment configures the daemon image's entrypoint: TLS from
// /certs (written by the start script from the scoped secrets) and registry
// pulls through the egress proxy.
func dockerDaemonEnvironment(machine *domain.DevMachine, secretNames []string) []string {
	return append(proxyEnvironment(machine, true),
		"KUAYLE_MACHINE_ID="+machine.ID.String(), "KUAYLE_SECRET_NAMES="+strings.Join(secretNames, ","),
		"DOCKER_TLS_CERTDIR=/certs", "KUAYLE_EGRESS_PROXY="+egressProxyURL(machine))
}

// dockerHostConfig confines the daemon container. Rootless dockerd creates
// user, mount and network namespaces and mounts proc and sys in them, which
// the default seccomp and AppArmor profiles and the masked /proc paths deny,
// and newuidmap needs SETUID and SETGID (so no-new-privileges cannot be set).
// It never runs privileged, receives no host path or socket and stays on the
// machine network. With a configured runtime (Sysbox) none of the profile
// relaxations are applied.
func (r *DockerRuntime) dockerHostConfig(machine *domain.DevMachine, networkName, workspaceVolume, dataVolume string) *container.HostConfig {
	memory := int64(machine.MemoryMB) * 1024 * 1024 * dockerShare / 100
	if memory < 512*1024*1024 {
		memory = 512 * 1024 * 1024
	}
	pids := int64(machine.PidsLimit) * 2
	if pids < dockerMinimumPids {
		pids = dockerMinimumPids
	}
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(networkName), ReadonlyRootfs: true, Privileged: false,
		CapDrop: []string{"ALL"}, CapAdd: []string{"SETUID", "SETGID"},
		LogConfig: boundedLogConfig(),
		Tmpfs: map[string]string{
			"/run":                "rw,nosuid,nodev,uid=1000,gid=1000,size=64m",
			"/run/kuayle-secrets": "rw,noexec,nosuid,uid=1000,gid=1000,mode=0700,size=8m",
			"/tmp":                "rw,nosuid,nodev,uid=1000,gid=1000,size=256m",
			"/home/rootless":      "rw,nosuid,nodev,uid=1000,gid=1000,size=64m",
			"/certs":              "rw,noexec,nosuid,nodev,uid=1000,gid=1000,mode=0700,size=1m",
			"/var/lib/docker":     "rw,noexec,nosuid,nodev,size=1m",
		},
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: workspaceVolume, Target: "/workspace", VolumeOptions: &mount.VolumeOptions{NoCopy: true}},
			{Type: mount.TypeVolume, Source: dataVolume, Target: dockerDataPath},
		},
		Resources: container.Resources{
			NanoCPUs: int64(machine.CPUMillis) * 1_000_000 * dockerShare / 100,
			Memory:   memory, MemorySwap: memory, PidsLimit: &pids,
			Devices: []container.DeviceMapping{{PathOnHost: "/dev/net/tun", PathInContainer: "/dev/net/tun", CgroupPermissions: "rwm"}},
		},
		ShmSize: 64 * 1024 * 1024,
	}
	if runtime := strings.TrimSpace(r.config.DockerRuntime); runtime != "" {
		hostConfig.Runtime = runtime
		return hostConfig
	}
	seccomp, apparmor := r.config.DockerSeccompProfile, r.config.DockerAppArmorProfile
	if seccomp == "" {
		seccomp = "unconfined"
	}
	if apparmor == "" {
		apparmor = "unconfined"
	}
	if seccomp != "default" {
		hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "seccomp="+seccomp)
	}
	if apparmor != "default" {
		hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "apparmor="+apparmor)
	}
	hostConfig.MaskedPaths, hostConfig.ReadonlyPaths = []string{}, []string{}
	return hostConfig
}

// createDockerService creates and starts the daemon container, then installs
// its TLS material. Its health check passes once dockerd answers.
func (r *DockerRuntime) createDockerService(ctx context.Context, machine *domain.DevMachine, service domain.DevMachineService, labels map[string]string, networkName, workspaceVolume string, secrets map[string]string) (string, error) {
	secretNames := make([]string, 0, len(secrets))
	for name := range secrets {
		secretNames = append(secretNames, name)
	}
	config := &container.Config{
		Image: service.ImageRef, User: "1000:1000", Labels: labels,
		Env:        dockerDaemonEnvironment(machine, secretNames),
		Entrypoint: []string{"/usr/local/bin/kuayle-docker-start"},
		Healthcheck: &container.HealthConfig{
			Test:     []string{"CMD", "/usr/local/bin/kuayle-docker-healthcheck"},
			Interval: 2 * time.Second, Timeout: 5 * time.Second, StartPeriod: 2 * time.Minute, Retries: 30,
		},
	}
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: config, HostConfig: r.dockerHostConfig(machine, networkName, workspaceVolume, dockerVolumeName(machine)),
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {Aliases: []string{service.InternalHost, "docker"}}}},
		Name:             service.ContainerName,
	})
	if err != nil {
		return "", err
	}
	completed := false
	defer func() {
		if !completed {
			_, _ = r.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		}
	}()
	if _, err := r.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return "", err
	}
	if err := r.copySecrets(ctx, response.ID, secrets); err != nil {
		return "", err
	}
	if err := r.requireRunning(ctx, response.ID); err != nil {
		return "", err
	}
	completed = true
	return response.ID, nil
}

// ensureDockerVolume creates the daemon's data volume with the machine's
// disk quota (hard or monitored, like the workspace volume).
func (r *DockerRuntime) ensureDockerVolume(ctx context.Context, machine *domain.DevMachine) (bool, error) {
	labels := machineLabels(machine)
	labels["com.kuayle.volume"] = "docker"
	return r.ensureVolume(ctx, dockerVolumeName(machine), labels, machine.DiskGB)
}

func (r *DockerRuntime) dockerVolumeExists(ctx context.Context, machine *domain.DevMachine) (bool, error) {
	labels := machineLabels(machine)
	labels["com.kuayle.volume"] = "docker"
	return r.inspectWorkspaceVolume(ctx, dockerVolumeName(machine), labels, machine.DiskGB)
}

// dockerDiskUsage measures the daemon's data volume. Its files belong to the
// daemon's user namespace, which the machine's users control, so the size is
// read by a short-lived helper container instead: the daemon image's du, the
// volume mounted read-only, no network, only CAP_DAC_READ_SEARCH and the
// default seccomp profile. Results are reused for dockerUsageRefresh.
func (r *DockerRuntime) dockerDiskUsage(ctx context.Context, machine *domain.DevMachine, imageRef string) (int64, error) {
	r.dockerUsageMu.Lock()
	cached, ok := r.dockerUsage[machine.ID]
	r.dockerUsageMu.Unlock()
	if ok && time.Since(cached.at) < dockerUsageRefresh {
		return cached.bytes, nil
	}
	name := "kuayle-" + machine.RoutingKey + "-docker-du"
	if inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		if !ownedByMachine(inspection.Container.Config.Labels, machine.ID) {
			return 0, fmt.Errorf("existing docker usage helper is not managed by Kuayle")
		}
		if _, err := r.client.ContainerRemove(ctx, inspection.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			return 0, err
		}
	} else if !errdefs.IsNotFound(err) {
		return 0, err
	}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "docker-du"
	pids := int64(16)
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: imageRef, User: "0:0", Labels: labels, Entrypoint: []string{"du"}, Cmd: []string{"-sxk", "/data"}},
		HostConfig: &container.HostConfig{
			NetworkMode: "none", ReadonlyRootfs: true, CapDrop: []string{"ALL"}, CapAdd: []string{"DAC_READ_SEARCH"},
			SecurityOpt: []string{"no-new-privileges=true"}, LogConfig: boundedLogConfig(),
			Mounts:    []mount.Mount{{Type: mount.TypeVolume, Source: dockerVolumeName(machine), Target: "/data", ReadOnly: true, VolumeOptions: &mount.VolumeOptions{NoCopy: true}}},
			Resources: container.Resources{Memory: 64 * 1024 * 1024, MemorySwap: 64 * 1024 * 1024, NanoCPUs: 200_000_000, PidsLimit: &pids},
		},
		Name: name,
	})
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = r.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := r.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return 0, err
	}
	waitResult := r.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-waitResult.Result:
		if result.StatusCode != 0 {
			return 0, fmt.Errorf("docker usage helper exited %d", result.StatusCode)
		}
	case err := <-waitResult.Error:
		return 0, err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	stdout, _, err := r.containerLogs(ctx, response.ID)
	if err != nil {
		return 0, err
	}
	bytes, err := parseDiskUsageKiB(stdout)
	if err != nil {
		return 0, err
	}
	r.dockerUsageMu.Lock()
	if r.dockerUsage == nil {
		r.dockerUsage = map[uuid.UUID]dockerUsageSample{}
	}
	r.dockerUsage[machine.ID] = dockerUsageSample{bytes: bytes, at: time.Now()}
	r.dockerUsageMu.Unlock()
	return bytes, nil
}

// dockerUsageRefresh bounds how often a daemon volume is measured.
const dockerUsageRefresh = 2 * time.Minute

type dockerUsageSample struct {
	bytes int64
	at    time.Time
}

// parseDiskUsageKiB reads "du -sk" output as bytes.
func parseDiskUsageKiB(output string) (int64, error) {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return 0, fmt.Errorf("docker data size unavailable")
	}
	kib, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || kib < 0 || kib > math.MaxInt64/1024 {
		return 0, fmt.Errorf("invalid docker data size %q", fields[0])
	}
	return kib * 1024, nil
}

// removeDockerVolume removes the daemon's data volume at teardown.
func (r *DockerRuntime) removeDockerVolume(ctx context.Context, machine *domain.DevMachine) error {
	err := r.removeManagedVolume(ctx, dockerVolumeName(machine), machine.ID)
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}
