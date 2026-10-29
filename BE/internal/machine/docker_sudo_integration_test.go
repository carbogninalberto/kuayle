package machine

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// Linux capability numbers checked in the effective set of a sudo shell.
const (
	capNetAdmin = 12
	capNetRaw   = 13
	capSysAdmin = 21
)

// A headless machine created with sudo lets its user run commands as root
// and install into the root filesystem, but root has neither NET_ADMIN,
// SYS_ADMIN nor raw sockets, and has no route around the egress proxy. Global
// npm installs work without sudo and land on the workspace volume. Sudo keeps
// working after a stop/start, while an agent attempt on the same machine and
// the terminal of a machine without sudo cannot use it. Needs the headless,
// collector and egress images and network access for an npm download.
func TestSudoMachinesGetRootWithoutLeavingTheSandbox(t *testing.T) {
	if os.Getenv("KUAYLE_SUDO_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_SUDO_INTEGRATION=1 to run the machine sudo test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	headlessImage := integrationImage("KUAYLE_TEST_HEADLESS_IMAGE", "kuayle/dev-machine-headless:0.1.0")
	routingKey := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	gatewayName := "kuayle-integration-gateway-" + routingKey
	runtime, err := NewDockerRuntime(DockerConfig{
		GatewayContainerName: gatewayName, IngestURL: "https://example.invalid/api/dev-machine-ingest",
		DiskQuotaMode: os.Getenv("KUAYLE_TEST_DISK_QUOTA_MODE"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	require.NoError(t, runtime.Ping(ctx))

	gateway, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: headlessImage, User: "1000:1000", Entrypoint: []string{"/bin/sleep"}, Cmd: []string{"600"}},
		Name:   gatewayName,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), gateway.ID, client.ContainerRemoveOptions{Force: true})
	})
	_, err = runtime.client.ContainerStart(ctx, gateway.ID, client.ContainerStartOptions{})
	require.NoError(t, err)

	machine := &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: routingKey, Status: domain.DevMachineStatusQueued,
		Profile: domain.DevMachineProfileHeadless, CPUMillis: 4000, MemoryMB: 4096, DiskGB: 20, PidsLimit: 512,
		SudoEnabled: true,
	}
	services := []domain.DevMachineService{
		integrationService(machine, "collector", "collector", integrationImage("KUAYLE_TEST_COLLECTOR_IMAGE", "kuayle/dev-machine-collector:0.1.0"), 8091),
		integrationService(machine, "egress", "egress", integrationImage("KUAYLE_TEST_EGRESS_IMAGE", "kuayle/dev-machine-egress:0.1.0"), 3128),
		integrationService(machine, "terminal", "terminal", headlessImage, 7681),
	}
	secrets := map[string]map[string]string{"collector": {"KUAYLE_MACHINE_TOKEN": "integration-test-token"}}
	networkName, volumeName, containers, err := runtime.Spawn(ctx, machine, services, secrets)
	machine.DockerNetworkName, machine.WorkspaceVolumeName = &networkName, &volumeName
	t.Cleanup(func() { _ = runtime.Teardown(context.Background(), machine, services) })
	require.NoError(t, err)
	machine.Status = domain.DevMachineStatusRunning
	for index := range services {
		id := containers[services[index].ServiceKey]
		services[index].ContainerID = &id
		services[index].Status = "running"
	}
	developer := containers["terminal"]
	run := func(id, script string) (string, error) {
		t.Helper()
		return runtime.execOutput(ctx, id, []string{"/bin/sh", "-c", script})
	}

	inspection, err := runtime.client.ContainerInspect(ctx, developer, client.ContainerInspectOptions{})
	require.NoError(t, err)
	host := inspection.Container.HostConfig
	require.False(t, host.Privileged)
	require.Empty(t, host.Binds, "nothing from the host, in particular no Docker socket")
	require.Empty(t, host.CapAdd)

	output, err := run(developer, "sudo -n id -u")
	require.NoError(t, err, output)
	require.Equal(t, "0", strings.TrimSpace(output))
	output, err = run(developer, "sudo -n sh -c 'echo probe > /usr/local/bin/kuayle-sudo-probe' && cat /usr/local/bin/kuayle-sudo-probe")
	require.NoError(t, err, output)
	require.Equal(t, "probe", strings.TrimSpace(output), "root can install into the root filesystem")

	output, err = run(developer, "sudo -n grep CapEff /proc/self/status")
	require.NoError(t, err, output)
	fields := strings.Fields(output)
	require.Len(t, fields, 2, output)
	effective, err := strconv.ParseUint(fields[1], 16, 64)
	require.NoError(t, err)
	for _, capability := range []uint{capNetAdmin, capNetRaw, capSysAdmin} {
		require.Zero(t, effective&(1<<capability), "root lacks capability %d (CapEff %s)", capability, fields[1])
	}
	output, err = run(developer, "sudo -n env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy curl -fsS -m 5 -o /dev/null http://example.com/ && echo BYPASSED || echo blocked")
	require.NoError(t, err, output)
	require.Contains(t, output, "blocked", "root has no route around the egress proxy")

	output, err = run(developer, "npm config get prefix && npm install -g --no-audit --no-fund semver@7.7.2 >/dev/null 2>&1 && command -v semver && semver 1.2.3")
	require.NoError(t, err, output)
	require.Equal(t, "/workspace/.kuayle/npm-global\n/workspace/.kuayle/npm-global/bin/semver\n1.2.3\n", output, "global npm installs need no sudo and persist on the workspace volume")

	require.NoError(t, runtime.Stop(ctx, machine, services))
	machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	output, err = run(developer, "sudo -n id -u && semver 2.0.0")
	require.NoError(t, err, output)
	require.Equal(t, "0\n2.0.0\n", output, "sudo and npm installs survive a restart")

	// An agent attempt on the same machine, and a developer container of a
	// machine without sudo, keep no-new-privileges and no capabilities.
	locked := *machine
	locked.SudoEnabled = false
	for name, config := range map[string]*container.HostConfig{
		"agent":    runtime.secureHostConfig(machine, "agent", networkName, volumeName),
		"terminal": runtime.secureHostConfig(&locked, "terminal", networkName, volumeName),
	} {
		created, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config:           &container.Config{Image: headlessImage, User: "1000:1000", WorkingDir: "/workspace", Entrypoint: []string{"/bin/sleep"}, Cmd: []string{"300"}, Labels: machineLabels(machine)},
			HostConfig:       config,
			NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {}}},
			Name:             "kuayle-" + routingKey + "-sudo-probe-" + name,
		})
		require.NoError(t, err, name)
		t.Cleanup(func() {
			_, _ = runtime.client.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
		})
		_, err = runtime.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
		require.NoError(t, err, name)
		output, err = run(created.ID, "sudo -n true 2>&1 && echo ROOT || echo denied")
		require.NoError(t, err, name)
		require.Contains(t, output, "denied", name)
		require.NotContains(t, output, "ROOT", name)
		output, err = run(created.ID, "touch /usr/local/bin/probe 2>/dev/null && echo WRITABLE || echo read-only")
		require.NoError(t, err, name)
		require.Contains(t, output, "read-only", name)
		_, err = runtime.client.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		require.NoError(t, err, name)
	}
}
