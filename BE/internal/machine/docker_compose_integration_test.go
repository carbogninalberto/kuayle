package machine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// Runs a real independent check against a constrained Compose stack. Needs
// local alpine:3 and redis:7-alpine images.
func TestDockerCheckReachesComposeServicesOnAnInternalNetwork(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{ComposeImages: []string{"docker.io/library"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	key := "cmp" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: key, CPUMillis: 2000, MemoryMB: 2048, PidsLimit: 512}
	networkName, volumeName := "kuayle-machine-"+key, "kuayle-workspace-"+key
	machine.DockerNetworkName, machine.WorkspaceVolumeName = &networkName, &volumeName
	_, err = runtime.client.NetworkCreate(ctx, networkName, client.NetworkCreateOptions{Driver: "bridge", Internal: true, Labels: machineLabels(machine)})
	require.NoError(t, err)
	_, err = runtime.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: volumeName, Labels: machineLabels(machine)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Teardown(context.Background(), machine, nil)) })

	// The checkout holds the (untrusted) Compose file.
	compose := "services:\n  cache:\n    image: redis:7-alpine\n    healthcheck:\n      test: [\"CMD\", \"redis-cli\", \"ping\"]\n      interval: 1s\n      retries: 20\n"
	writer, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &container.Config{Image: "alpine:3", Entrypoint: []string{"sh", "-c", `mkdir -p /workspace/tasks/t && printf '%s' "$COMPOSE" > /workspace/tasks/t/compose.yaml && chown -R 1000:1000 /workspace/tasks`}, Env: []string{"COMPOSE=" + compose}},
		HostConfig: &container.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: volumeName, Target: "/workspace"}}},
	})
	require.NoError(t, err)
	_, err = runtime.client.ContainerStart(ctx, writer.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	wait := runtime.client.ContainerWait(ctx, writer.ID, client.ContainerWaitOptions{})
	select {
	case result := <-wait.Result:
		require.Zero(t, result.StatusCode)
	case err := <-wait.Error:
		require.NoError(t, err)
	}
	_, _ = runtime.client.ContainerRemove(ctx, writer.ID, client.ContainerRemoveOptions{Force: true})

	run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID}
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/t"}
	step := &domain.DevMachineAgentRunStep{ID: uuid.New(), ComposeFile: "compose.yaml", CommandArgv: json.RawMessage(`["sh","-c","nc -z -w 3 cache 6379 && echo reached-cache"]`)}
	var output strings.Builder
	execution, err := runtime.RunValidation(ctx, machine, run, step, checkout, "alpine:3", &AgentObserver{Output: func(_ string, data []byte) error { output.Write(data); return nil }})
	require.NoError(t, err)
	require.Zero(t, execution.ExitCode, output.String())
	require.Contains(t, output.String(), "reached-cache")

	service, err := runtime.client.ContainerInspect(ctx, composeContainerName(machine, run, "cache"), client.ContainerInspectOptions{})
	require.NoError(t, err)
	host := service.Container.HostConfig
	require.False(t, host.Privileged)
	require.Contains(t, host.CapDrop, "ALL")
	require.Empty(t, host.Mounts)
	require.Empty(t, host.PortBindings)
	require.Equal(t, checkNetworkName(machine, run), string(host.NetworkMode))
	network, err := runtime.client.NetworkInspect(ctx, checkNetworkName(machine, run), client.NetworkInspectOptions{})
	require.NoError(t, err)
	require.True(t, network.Network.Internal, "services must have no egress")

	require.NoError(t, runtime.RemoveCheckServices(ctx, machine, run, step))
	_, err = runtime.client.ContainerInspect(ctx, composeContainerName(machine, run, "cache"), client.ContainerInspectOptions{})
	require.Error(t, err)
	_, err = runtime.client.NetworkInspect(ctx, checkNetworkName(machine, run), client.NetworkInspectOptions{})
	require.Error(t, err)

	// A file the agent changed to escape the subset is rejected before any
	// service starts.
	rejected := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID}
	bad := *step
	bad.ID, bad.ComposeFile = uuid.New(), "missing.yaml"
	_, err = runtime.RunValidation(ctx, machine, rejected, &bad, checkout, "alpine:3", &AgentObserver{})
	var composeError *ComposeError
	require.True(t, errors.As(err, &composeError), "%v", err)
	require.NoError(t, runtime.CancelAgent(ctx, rejected))
}
