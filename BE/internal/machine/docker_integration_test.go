package machine

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestDockerRuntimeLifecycle(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	routingKey := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	gatewayName := "kuayle-integration-gateway-" + routingKey
	runtime, err := NewDockerRuntime(DockerConfig{
		GatewayContainerName: gatewayName,
		IngestURL:            "https://example.invalid/api/dev-machine-ingest",
		EgressDenylist:       "169.254.169.254,metadata.google.internal",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })

	gateway, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      "kuayle/dev-machine-ide:0.1.0",
			Entrypoint: []string{"/bin/sleep"},
			Cmd:        []string{"120"},
		},
		Name: gatewayName,
	})
	require.NoError(t, err)
	_, err = runtime.client.ContainerStart(ctx, gateway.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), gateway.ID, client.ContainerRemoveOptions{Force: true})
	})

	machine := &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: routingKey,
		Status:    domain.DevMachineStatusQueued,
		CPUMillis: 4000, MemoryMB: 4096, PidsLimit: 1024,
		BaseBranch: "main", WorkingBranch: "kuayle/integration-test",
	}
	services := []domain.DevMachineService{
		integrationService(machine, "collector", "collector", "kuayle/dev-machine-collector:0.1.0", 8091),
		integrationService(machine, "egress", "egress", "kuayle/dev-machine-egress:0.1.0", 3128),
		integrationService(machine, "ide", "ide", "kuayle/dev-machine-ide:0.1.0", 8080),
		integrationService(machine, "terminal", "terminal", "kuayle/dev-machine-ide:0.1.0", 7681),
		integrationService(machine, "browser", "browser", "kuayle/dev-machine-browser:0.1.0", 3000),
	}
	secrets := map[string]map[string]string{
		"collector": {"KUAYLE_MACHINE_TOKEN": "integration-test-token"},
	}

	networkName, volumeName, containers, err := runtime.Spawn(ctx, machine, services, secrets)
	require.NoError(t, err)
	machine.Status = domain.DevMachineStatusRunning
	require.Equal(t, containers["ide"], containers["terminal"])
	machine.DockerNetworkName = &networkName
	machine.WorkspaceVolumeName = &volumeName
	for index := range services {
		containerID := containers[services[index].ServiceKey]
		services[index].ContainerID = &containerID
		services[index].Status = "running"
	}
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = runtime.Teardown(context.Background(), machine, services)
		}
	})

	privateNetwork, err := runtime.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
	require.NoError(t, err)
	require.True(t, privateNetwork.Network.Internal)
	require.Contains(t, privateNetwork.Network.Containers, gateway.ID)

	for _, service := range services {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
		inspection, err := runtime.client.ContainerInspect(ctx, *service.ContainerID, client.ContainerInspectOptions{})
		require.NoError(t, err)
		require.Equal(t, "1000:1000", inspection.Container.Config.User)
		require.True(t, inspection.Container.HostConfig.ReadonlyRootfs)
		require.False(t, inspection.Container.HostConfig.Privileged)
		require.Contains(t, inspection.Container.HostConfig.CapDrop, "ALL")
		require.Contains(t, inspection.Container.HostConfig.SecurityOpt, "no-new-privileges=true")
		require.Empty(t, inspection.Container.HostConfig.PortBindings)
		require.NotZero(t, inspection.Container.HostConfig.Memory)
		require.NotZero(t, inspection.Container.HostConfig.NanoCPUs)
		require.NotNil(t, inspection.Container.HostConfig.PidsLimit)
		require.Contains(t, inspection.Container.HostConfig.Tmpfs, "/run/kuayle-secrets")
		if service.ServiceType == "egress" {
			require.Contains(t, inspection.Container.NetworkSettings.Networks, egressNetworkName)
		}
	}
	for _, service := range services {
		if service.ServiceType == "ide" {
			_, err := runtime.execOutput(ctx, *service.ContainerID, []string{"/bin/sh", "-c", "touch /workspace/.kuayle-write-test && rm /workspace/.kuayle-write-test"})
			require.NoError(t, err)
			break
		}
	}

	require.NoError(t, runtime.Pause(ctx, machine, services))
	machine.Status = domain.DevMachineStatusPaused
	for _, service := range services {
		inspection, err := runtime.client.ContainerInspect(ctx, *service.ContainerID, client.ContainerInspectOptions{})
		require.NoError(t, err)
		require.True(t, inspection.Container.State.Paused)
	}
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	for _, service := range services {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
	}

	require.NoError(t, runtime.Stop(ctx, machine, services))
	machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	for _, service := range services {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
	}

	command, err := json.Marshal([]string{"git", "config", "--global", "--get", "credential.helper"})
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{
		ID: uuid.New(), Mode: "task", CommandArgv: command,
	}
	provider := &domain.DevMachineAgentProvider{ImageRef: "kuayle/dev-machine-agent-claude:0.1.0"}
	execution, err := runtime.RunAgent(ctx, machine, run, provider, nil, map[string]string{
		"GITHUB_TOKEN": "integration-github-token",
	})
	require.NoError(t, err)
	require.Zero(t, execution.ExitCode)
	require.Contains(t, execution.Stdout, "/home/kuayle/.kuayle-git-credential")
	retriedExecution, err := runtime.RunAgent(ctx, machine, run, provider, nil, nil)
	require.NoError(t, err)
	require.Equal(t, execution.ContainerID, retriedExecution.ContainerID)
	require.Equal(t, execution.Stdout, retriedExecution.Stdout)
	customCommand, err := json.Marshal([]string{"/usr/bin/env"})
	require.NoError(t, err)
	customExecution, err := runtime.RunAgent(ctx, machine, &domain.DevMachineAgentRun{
		ID: uuid.New(), Mode: "task", CommandArgv: customCommand,
	}, &domain.DevMachineAgentProvider{ImageRef: "kuayle/dev-machine-ide:0.1.0", IsCustom: true}, nil, map[string]string{
		"TEST_SECRET": "custom-secret-value", "GITHUB_TOKEN": "custom-github-token",
	})
	require.NoError(t, err)
	require.Zero(t, customExecution.ExitCode)
	require.Contains(t, customExecution.Stdout, "TEST_SECRET=custom-secret-value")
	services = append(services, domain.DevMachineService{
		ServiceType: "agent", ServiceKey: "agent", ContainerID: &execution.ContainerID,
	}, domain.DevMachineService{
		ServiceType: "agent", ServiceKey: "custom-agent", ContainerID: &customExecution.ContainerID,
	})
	require.NoError(t, runtime.Stop(ctx, machine, services))
	machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	for _, service := range services[:len(services)-2] {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
	}
	agentInspection, err := runtime.client.ContainerInspect(ctx, execution.ContainerID, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.False(t, agentInspection.Container.State.Running)
	customAgentInspection, err := runtime.client.ContainerInspect(ctx, customExecution.ContainerID, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.False(t, customAgentInspection.Container.State.Running)

	require.NoError(t, runtime.Teardown(ctx, machine, services))
	cleaned = true
}

func integrationService(machine *domain.DevMachine, serviceType, key, image string, port int) domain.DevMachineService {
	return domain.DevMachineService{
		ID: uuid.New(), MachineID: machine.ID, ServiceType: serviceType, ServiceKey: key,
		ContainerName: "kuayle-" + machine.RoutingKey + "-" + key, ImageRef: image,
		InternalHost: machine.RoutingKey + "-" + key, InternalPort: port,
	}
}

func requireContainerRunning(t *testing.T, ctx context.Context, runtime *DockerRuntime, serviceKey, containerID string) {
	t.Helper()
	var inspection container.InspectResponse
	var inspectErr error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		raw, rawErr := runtime.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		inspection, inspectErr = raw.Container, rawErr
		if inspectErr == nil && inspection.State.Running {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	logs, err := runtime.client.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		t.Fatalf("%s container is not running: inspect=%v logs=%v", serviceKey, inspectErr, err)
	}
	defer logs.Close()
	output, _ := io.ReadAll(logs)
	t.Fatalf("%s container is not running (status=%s exit=%d error=%q path=%q args=%q): %s", serviceKey, inspection.State.Status, inspection.State.ExitCode, inspection.State.Error, inspection.Path, inspection.Args, output)
}
