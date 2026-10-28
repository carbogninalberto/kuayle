package machine

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/dockertls"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func integrationImage(variable, fallback string) string {
	if value := os.Getenv(variable); value != "" {
		return value
	}
	return fallback
}

// A machine with a Docker daemon runs a real Compose project from the
// developer container: a build whose RUN step downloads through the egress
// proxy, a bind mount of the worktree, env_file and profiles. Its published
// port is found by the scan and reachable from the gateway's network; nested
// containers have no direct route out, and the daemon API needs the client
// certificate. Stop/start keeps the daemon working, and teardown removes its
// volume. Needs network access for the busybox image and an HTTP download.
func TestDockerDaemonRunsComposeProjects(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_DAEMON_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_DAEMON_INTEGRATION=1 to run the machine Docker daemon test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	dockerImage := integrationImage("KUAYLE_TEST_DOCKER_IMAGE", "kuayle/dev-machine-docker:0.1.0")
	routingKey := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	gatewayName := "kuayle-integration-gateway-" + routingKey
	runtime, err := NewDockerRuntime(DockerConfig{
		GatewayContainerName: gatewayName, IngestURL: "https://example.invalid/api/dev-machine-ingest",
		DiskQuotaMode: os.Getenv("KUAYLE_TEST_DISK_QUOTA_MODE"), DockerRuntime: os.Getenv("KUAYLE_TEST_DOCKER_RUNTIME"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	require.NoError(t, runtime.Ping(ctx))

	gateway, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: dockerImage, User: "1000:1000", Entrypoint: []string{"/bin/sleep"}, Cmd: []string{"600"}},
		Name:   gatewayName,
	})
	require.NoError(t, err)
	_, err = runtime.client.ContainerStart(ctx, gateway.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), gateway.ID, client.ContainerRemoveOptions{Force: true})
	})

	machine := &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: routingKey, Status: domain.DevMachineStatusQueued,
		Profile: domain.DevMachineProfileHeadless, CPUMillis: 4000, MemoryMB: 4096, DiskGB: 20, PidsLimit: 512,
	}
	services := []domain.DevMachineService{
		integrationService(machine, "collector", "collector", integrationImage("KUAYLE_TEST_COLLECTOR_IMAGE", "kuayle/dev-machine-collector:0.1.0"), 8091),
		integrationService(machine, "egress", "egress", integrationImage("KUAYLE_TEST_EGRESS_IMAGE", "kuayle/dev-machine-egress:0.1.0"), 3128),
		integrationService(machine, "docker", "docker", dockerImage, dockerDaemonPort),
		integrationService(machine, "terminal", "terminal", integrationImage("KUAYLE_TEST_HEADLESS_IMAGE", "kuayle/dev-machine-headless:0.1.0"), 7681),
	}
	material, err := dockertls.New(routingKey, time.Now().Add(time.Hour))
	require.NoError(t, err)
	secrets := map[string]map[string]string{
		"collector": {"KUAYLE_MACHINE_TOKEN": "integration-test-token"},
		"docker":    {"KUAYLE_DOCKER_TLS_CA": material.CA, "KUAYLE_DOCKER_TLS_CERT": material.ServerCert, "KUAYLE_DOCKER_TLS_KEY": material.ServerKey},
		"terminal":  {"KUAYLE_DOCKER_CA": material.CA, "KUAYLE_DOCKER_CERT": material.ClientCert, "KUAYLE_DOCKER_KEY": material.ClientKey},
	}
	networkName, volumeName, containers, err := runtime.Spawn(ctx, machine, services, secrets)
	machine.DockerNetworkName, machine.WorkspaceVolumeName = &networkName, &volumeName
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = runtime.Teardown(context.Background(), machine, services)
		}
	})
	require.NoError(t, err)
	machine.Status = domain.DevMachineStatusRunning
	for index := range services {
		id := containers[services[index].ServiceKey]
		services[index].ContainerID = &id
		services[index].Status = "running"
	}
	daemon := containers["docker"]
	developer := containers["terminal"]

	inspection, err := runtime.client.ContainerInspect(ctx, daemon, client.ContainerInspectOptions{})
	require.NoError(t, err)
	host := inspection.Container.HostConfig
	require.False(t, host.Privileged)
	require.Empty(t, host.Binds)
	require.Empty(t, host.PortBindings)
	require.ElementsMatch(t, []string{"CAP_SETUID", "CAP_SETGID"}, host.CapAdd)
	require.Equal(t, []string{networkName}, keys(inspection.Container.NetworkSettings.Networks), "the daemon is on the machine network only")
	_, err = runtime.dockerVolumeExists(ctx, machine)
	require.NoError(t, err)

	run := func(id string, script string) (string, error) {
		t.Helper()
		return runtime.execOutput(ctx, id, []string{"/bin/sh", "-c", script})
	}
	// Exec sessions get the container environment; terminals also get the
	// start script's exports.
	project := `set -eu
mkdir -p /workspace/proj/srv && cd /workspace/proj
printf 'FROM busybox:1.37\nRUN wget -q -O /fetched http://example.com/ && test -s /fetched\nCMD ["httpd","-f","-p","8000","-h","/srv"]\n' > Dockerfile
printf 'from the worktree\n' > srv/index.html
printf 'GREETING=hello\n' > .env
printf 'services:\n  web:\n    build: .\n    ports: ["3000:8000"]\n    volumes: ["./srv:/srv"]\n    env_file: [.env]\n  extra:\n    image: busybox:1.37\n    command: ["sleep","600"]\n    profiles: [extra]\n' > compose.yaml
docker compose up -d --build --wait 2>&1
docker compose exec -T web sh -c 'echo "$GREETING"'
docker compose --profile extra up -d 2>&1
docker compose ps --services | sort | tr '\n' ' '`
	output, err := run(developer, project)
	require.NoError(t, err, output)
	require.Contains(t, output, "hello")
	require.Contains(t, output, "extra web")

	scan, err := runtime.ScanPorts(ctx, services)
	require.NoError(t, err)
	require.True(t, slices.Contains(scan.Docker, 3000), "published ports are scanned: %v", scan.Docker)
	page, err := run(gateway.ID, "wget -q -T 10 -O- http://"+routingKey+"-docker:3000/")
	require.NoError(t, err, page)
	require.Equal(t, "from the worktree\n", page, "the gateway reaches published ports")
	_, err = run(gateway.ID, "wget -q -T 5 -O- http://"+routingKey+"-docker:2376/version")
	require.Error(t, err, "the daemon API needs TLS and a client certificate")

	output, err = run(developer, `docker run --rm -e http_proxy= -e HTTP_PROXY= busybox:1.37 wget -q -T 5 -O /dev/null http://example.com/ 2>&1 && echo BYPASSED || echo blocked`)
	require.NoError(t, err, output)
	require.Contains(t, output, "blocked", "containers have no route around the egress proxy")

	usage, err := runtime.Stats(ctx, machine, services)
	require.NoError(t, err)
	require.Positive(t, usage.DiskBytes, "the daemon's data counts toward the machine's disk")

	require.NoError(t, runtime.Stop(ctx, machine, services))
	machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	output, err = run(developer, "docker info --format '{{.ServerVersion}}' && docker image ls --format '{{.Repository}}' | sort -u | tr '\\n' ' '")
	require.NoError(t, err, output)
	require.Contains(t, output, "busybox", "images survive a restart")

	require.NoError(t, runtime.Teardown(ctx, machine, services))
	cleaned = true
	_, err = runtime.client.VolumeInspect(ctx, dockerVolumeName(machine), client.VolumeInspectOptions{})
	require.Error(t, err, "teardown removes the daemon's volume")
}

func keys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
