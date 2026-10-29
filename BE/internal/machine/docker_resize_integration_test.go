package machine

import (
	"context"
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

// Resizes a running and a stopped machine container with docker update, then
// lets a container exceed its memory limit: the exit reports the kill and the
// event stream reports it as it happens. Needs a local alpine:3 image.
func TestDockerResizeAndMemoryLimitDetection(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	key := "rsz" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: key, CPUMillis: 1000, MemoryMB: 2048, PidsLimit: 512}
	create := func(name, service string, resources container.Resources, cmd ...string) string {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = service
		created, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
			Name:       "kuayle-" + key + "-" + name,
			Config:     &container.Config{Image: "alpine:3", Cmd: cmd, Labels: labels},
			HostConfig: &container.HostConfig{NetworkMode: "none", Resources: resources},
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = runtime.client.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
		})
		return created.ID
	}
	initial := runtime.secureHostConfig(machine, "terminal", "", "").Resources
	running := create("terminal", "terminal", initial, "sleep", "120")
	_, err = runtime.client.ContainerStart(ctx, running, client.ContainerStartOptions{})
	require.NoError(t, err)
	stopped := create("agent", "agent", runtime.secureHostConfig(machine, "agent", "", "").Resources, "true")

	resized := *machine
	resized.CPUMillis, resized.MemoryMB = 2000, 4096
	require.NoError(t, runtime.UpdateResources(ctx, &resized))
	for id, service := range map[string]string{running: "terminal", stopped: "agent"} {
		inspection, err := runtime.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		require.NoError(t, err)
		expected := runtime.secureHostConfig(&resized, service, "", "").Resources
		require.Equal(t, expected.Memory, inspection.Container.HostConfig.Memory, service)
		require.Equal(t, expected.NanoCPUs, inspection.Container.HostConfig.NanoCPUs, service)
		require.Equal(t, *expected.PidsLimit, *inspection.Container.HostConfig.PidsLimit, service)
	}
	runningInspection, err := runtime.client.ContainerInspect(ctx, running, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, runningInspection.Container.State.Running, "a live resize keeps the container running")

	hits := make(chan ResourceLimitHit, 4)
	watchCtx, stopWatching := context.WithCancel(ctx)
	defer stopWatching()
	go runtime.WatchResourceLimits(watchCtx, func(hit ResourceLimitHit) { hits <- hit })
	time.Sleep(500 * time.Millisecond)
	limit := int64(64 * 1024 * 1024)
	hog := create("hog", "agent", container.Resources{Memory: limit, MemorySwap: limit}, "sh", "-c", "tail /dev/zero")
	_, err = runtime.client.ContainerStart(ctx, hog, client.ContainerStartOptions{})
	require.NoError(t, err)
	inspection, err := runtime.client.ContainerInspect(ctx, hog, client.ContainerInspectOptions{})
	require.NoError(t, err)
	execution, err := runtime.resumeExecution(ctx, inspection.Container, false, nil, nil, false)
	require.NoError(t, err)
	require.Equal(t, 137, execution.ExitCode)
	require.True(t, execution.OOMKilled)
	require.Equal(t, limit, execution.MemoryLimitBytes)
	select {
	case hit := <-hits:
		require.Equal(t, machine.ID, hit.MachineID)
		require.Equal(t, machine.WorkspaceID, hit.WorkspaceID)
		require.Equal(t, "agent", hit.Service)
		require.Equal(t, hog, hit.ContainerID)
	case <-time.After(30 * time.Second):
		t.Fatal("the memory event was not reported")
	}
}
