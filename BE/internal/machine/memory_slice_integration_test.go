package machine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// TestMemorySliceBoundsContainersWithoutTheirOwnLimit runs the real helper on a
// systemd host. It must run in a container of an image whose
// /usr/local/bin/machine-manager is this branch's manager, with the Docker
// socket, as the manager does (KUAYLE_MEMORY_SLICE_INTEGRATION=1, plus an
// alpine:3 image on the host).
func TestMemorySliceBoundsContainersWithoutTheirOwnLimit(t *testing.T) {
	if os.Getenv("KUAYLE_MEMORY_SLICE_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_MEMORY_SLICE_INTEGRATION=1 on a systemd Docker host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	require.NoError(t, runtime.EnableMemorySlices(ctx, 0))
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "slicetest" + uuid.NewString()[:8], MemoryMB: 256}
	name, _ := memorySliceName(machine)
	require.NoError(t, runtime.slices.Apply(ctx, machine))
	defer func() { require.NoError(t, runtime.slices.Remove(context.Background(), machine)) }()

	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	response, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: "alpine:3", Cmd: []string{"tail", "/dev/zero"}, Labels: labels},
		// The container's own limit is far above the machine's, as for
		// workload containers of a shared machine.
		HostConfig: &container.HostConfig{Resources: container.Resources{CgroupParent: name, Memory: 4 << 30, MemorySwap: -1}},
	})
	require.NoError(t, err)
	defer func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	_, err = runtime.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	wait := runtime.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case <-wait.Result:
	case err := <-wait.Error:
		require.NoError(t, err)
	}
	inspection, err := runtime.client.ContainerInspect(ctx, response.ID, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, inspection.Container.State.OOMKilled, "the slice's 256 MiB stopped the process")
}
