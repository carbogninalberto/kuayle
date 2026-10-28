package machine

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func dockerTestMachine() *domain.DevMachine {
	return &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", CPUMillis: 2000, MemoryMB: 4096, PidsLimit: 512, DiskGB: 20}
}

// The daemon container is the only machine container with relaxed kernel
// confinement, and only what rootless Docker needs: no privileged mode, no
// host mounts or socket, all capabilities dropped except the two newuidmap
// needs, the machine network only, and its own data volume.
func TestDockerDaemonHostConfigRelaxesOnlyWhatRootlessDockerNeeds(t *testing.T) {
	runtime := &DockerRuntime{config: DockerConfig{SeccompProfile: "default", DockerSeccompProfile: "unconfined", DockerAppArmorProfile: "unconfined"}}
	machine := dockerTestMachine()
	config := runtime.dockerHostConfig(machine, "kuayle-machine-net", "kuayle-workspace-x", "kuayle-docker-x")

	require.False(t, config.Privileged)
	require.Equal(t, "kuayle-machine-net", string(config.NetworkMode), "never the host, control plane or egress network")
	require.True(t, config.ReadonlyRootfs)
	require.Equal(t, []string{"ALL"}, config.CapDrop)
	require.ElementsMatch(t, []string{"SETUID", "SETGID"}, config.CapAdd)
	require.ElementsMatch(t, []string{"seccomp=unconfined", "apparmor=unconfined"}, config.SecurityOpt)
	require.NotNil(t, config.MaskedPaths)
	require.Empty(t, config.MaskedPaths, "rootlesskit mounts proc and sys in its namespaces")
	require.NotNil(t, config.ReadonlyPaths)
	require.Empty(t, config.ReadonlyPaths)
	require.Len(t, config.Devices, 1)
	require.Equal(t, "/dev/net/tun", config.Devices[0].PathOnHost)
	require.Empty(t, config.Binds)
	require.Empty(t, config.Runtime)
	require.Empty(t, config.PortBindings, "nothing is published on the host")
	targets := map[string]mount.Mount{}
	for _, item := range config.Mounts {
		require.Equal(t, mount.TypeVolume, item.Type, "no host paths")
		targets[item.Target] = item
	}
	require.Equal(t, "kuayle-workspace-x", targets["/workspace"].Source, "build contexts and bind mounts see the worktrees at the same path")
	require.False(t, targets["/workspace"].ReadOnly)
	require.Equal(t, "kuayle-docker-x", targets[dockerDataPath].Source)
	require.Len(t, targets, 2)
	require.Contains(t, config.Tmpfs, "/run/kuayle-secrets")
	require.Contains(t, config.Tmpfs, "/var/lib/docker", "the image's anonymous volume is replaced")
	require.Positive(t, config.Resources.Memory)
	require.Equal(t, config.Resources.Memory, config.Resources.MemorySwap)
	require.GreaterOrEqual(t, *config.Resources.PidsLimit, int64(1024))
	require.Positive(t, config.Resources.NanoCPUs)

	sysbox := &DockerRuntime{config: DockerConfig{DockerRuntime: "sysbox-runc", DockerSeccompProfile: "unconfined", DockerAppArmorProfile: "unconfined"}}
	isolated := sysbox.dockerHostConfig(machine, "kuayle-machine-net", "kuayle-workspace-x", "kuayle-docker-x")
	require.Equal(t, "sysbox-runc", isolated.Runtime)
	require.Empty(t, isolated.SecurityOpt, "Sysbox needs no relaxed profiles")
	require.Nil(t, isolated.MaskedPaths, "Sysbox virtualizes proc and sys itself")
	require.False(t, isolated.Privileged)
}

// The daemon reaches registries only through the machine's egress proxy and
// serves its API with mutual TLS on the machine network.
func TestDockerDaemonEnvironmentUsesTheEgressProxyAndTLS(t *testing.T) {
	machine := dockerTestMachine()
	environment := dockerDaemonEnvironment(machine, []string{"KUAYLE_DOCKER_TLS_CA", "KUAYLE_DOCKER_TLS_CERT", "KUAYLE_DOCKER_TLS_KEY"})
	require.Contains(t, environment, "HTTP_PROXY=http://"+machine.RoutingKey+"-egress:3128")
	require.Contains(t, environment, "HTTPS_PROXY=http://"+machine.RoutingKey+"-egress:3128")
	require.Contains(t, environment, "DOCKER_TLS_CERTDIR=/certs")
	require.Contains(t, environment, "KUAYLE_SECRET_NAMES=KUAYLE_DOCKER_TLS_CA,KUAYLE_DOCKER_TLS_CERT,KUAYLE_DOCKER_TLS_KEY")
}

// Developer containers of a machine with a daemon talk to it over mutual TLS,
// never through the egress proxy.
func TestDeveloperEnvironmentPointsTheDockerCLIAtTheDaemon(t *testing.T) {
	machine := dockerTestMachine()
	environment := dockerClientEnvironment(machine)
	require.Contains(t, environment, "DOCKER_HOST=tcp://"+machine.RoutingKey+"-docker:2376")
	require.Contains(t, environment, "DOCKER_TLS_VERIFY=1")
	require.Contains(t, environment, "DOCKER_CERT_PATH=/home/kuayle/.docker")
	noProxy := ""
	for _, item := range proxyEnvironment(machine, true) {
		if value, ok := strings.CutPrefix(item, "NO_PROXY="); ok {
			noProxy = value
		}
	}
	require.True(t, slices.Contains(strings.Split(noProxy, ","), machine.RoutingKey+"-docker"))
	require.True(t, slices.Contains(strings.Split(noProxy, ","), "docker"))
	for _, item := range proxyEnvironment(machine, false) {
		require.NotContains(t, item, "-docker", "machines without a daemon are unchanged")
	}
}

func TestHasDockerServiceAndVolumeName(t *testing.T) {
	machine := dockerTestMachine()
	require.Equal(t, "kuayle-docker-"+machine.RoutingKey, dockerVolumeName(machine))
	require.False(t, hasDockerService([]domain.DevMachineService{{ServiceType: "ide"}}))
	require.True(t, hasDockerService([]domain.DevMachineService{{ServiceType: "ide"}, {ServiceType: "docker"}}))
}

func TestParseDiskUsageKiB(t *testing.T) {
	bytes, err := parseDiskUsageKiB("1234\t/data\n")
	require.NoError(t, err)
	require.Equal(t, int64(1234*1024), bytes)
	for _, invalid := range []string{"", "x /data", "-1 /data", "99999999999999999999 /data"} {
		_, err = parseDiskUsageKiB(invalid)
		require.Error(t, err, invalid)
	}
}
