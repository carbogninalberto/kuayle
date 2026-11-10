package machine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// Starts real preview servers for two tasks, each in its own container beside
// a developer-like container, restarts and stops one without touching the
// other, and scans the listening ports and agent requests. The previews see
// none of the developer container's secret or sign-in files. Needs a local
// alpine:3 image.
func TestDockerPreviewsRunPerTaskAndPortsAreScanned(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	routingKey := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	networkName, volumeName := "kuayle-preview-test-"+routingKey, "kuayle-preview-test-"+routingKey
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: routingKey, DockerNetworkName: &networkName, WorkspaceVolumeName: &volumeName,
		CPUMillis: 2000, MemoryMB: 2048, PidsLimit: 1024, Status: domain.DevMachineStatusRunning, SudoEnabled: true}
	labels := machineLabels(machine)
	_, err = runtime.client.NetworkCreate(ctx, networkName, client.NetworkCreateOptions{Labels: labels})
	require.NoError(t, err)
	_, err = runtime.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: volumeName, Labels: labels})
	require.NoError(t, err)
	created, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "kuayle-preview-test-developer-" + routingKey,
		Config: &container.Config{Image: "alpine:3", Env: []string{"HOME=/home/kuayle"}, Labels: labels, Entrypoint: []string{"sh", "-c",
			"mkdir -p /workspace/tasks/t /home/kuayle/.kuayle/agent-sign-ins /run/kuayle-secrets && echo fixture > /run/kuayle-secrets/DEV_ONLY_FIXTURE && echo fixture > /home/kuayle/.kuayle/agent-sign-ins/fixture && chown -R 1000:1000 /home/kuayle /workspace && touch /tmp/ready && sleep 300"}},
		HostConfig:       &container.HostConfig{NetworkMode: container.NetworkMode(networkName), Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: volumeName, Target: "/workspace"}}},
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {Aliases: []string{routingKey + "-ide"}}}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup := context.Background()
		_ = runtime.RemovePreviews(cleanup, machine)
		_, _ = runtime.client.ContainerRemove(cleanup, created.ID, client.ContainerRemoveOptions{Force: true})
		_, _ = runtime.client.NetworkRemove(cleanup, networkName, client.NetworkRemoveOptions{})
		_, _ = runtime.client.VolumeRemove(cleanup, volumeName, client.VolumeRemoveOptions{Force: true})
	})
	_, err = runtime.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	id := created.ID
	services := []domain.DevMachineService{{ServiceType: "terminal", Status: "running", ImageRef: "alpine:3", ContainerID: &id}}
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/t"}
	fetch := func(host, port string) string {
		out, _ := runtime.execOutput(ctx, id, []string{"sh", "-c", "wget -q -T 2 -O- http://" + host + ":" + port + "/ 2>/dev/null || true"})
		return strings.TrimSpace(out)
	}
	require.Eventually(t, func() bool {
		_, err := runtime.execOutput(ctx, id, []string{"test", "-f", "/tmp/ready"})
		return err == nil
	}, 10*time.Second, 200*time.Millisecond)

	// $PORT is provided, the server runs from the task worktree, and the
	// developer container's secret and sign-in files do not exist for it.
	argv := []string{"sh", "-c", `seen=isolated; [ ! -e /run/kuayle-secrets/DEV_ONLY_FIXTURE ] || seen=secret; [ ! -e "$HOME/.kuayle/agent-sign-ins" ] || seen=sign-ins
while true; do { printf 'HTTP/1.0 200 OK\r\n\r\n'; echo "preview-ok $seen $(id -u)"; } | nc -l -p "$PORT"; done`}
	first, second := uuid.New(), uuid.New()
	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, first, argv, 8090, nil))
	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, second, argv, 8091, nil))
	firstHost, secondHost := domain.PreviewPortHost(routingKey, 8090), domain.PreviewPortHost(routingKey, 8091)
	require.Eventually(t, func() bool {
		return fetch(firstHost, "8090") == "preview-ok isolated 1000" && fetch(secondHost, "8091") == "preview-ok isolated 1000"
	}, 20*time.Second, 200*time.Millisecond)
	require.Empty(t, fetch("127.0.0.1", "8090"), "nothing listens in the developer container")
	previews := func() map[string]string {
		items, err := runtime.previewContainers(ctx, machine)
		require.NoError(t, err)
		ids := map[string]string{}
		for _, item := range items {
			ids[item.Labels[previewTaskLabel]] = item.ID
		}
		return ids
	}
	before := previews()
	require.Len(t, before, 2)
	require.NotContains(t, []string{before[first.String()], before[second.String()]}, id, "previews do not run in the developer container")
	inspection, err := runtime.client.ContainerInspect(ctx, before[first.String()], client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, inspection.Container.HostConfig.ReadonlyRootfs)
	require.Contains(t, inspection.Container.HostConfig.SecurityOpt, "no-new-privileges=true", "also on a machine that allows sudo")
	require.Equal(t, []string{"ALL"}, []string(inspection.Container.HostConfig.CapDrop))

	require.NoError(t, runtime.StartPreview(ctx, machine, services, checkout, first, argv, 8090, nil), "a restart replaces only that task's preview")
	require.Eventually(t, func() bool { return fetch(firstHost, "8090") == "preview-ok isolated 1000" }, 20*time.Second, 200*time.Millisecond)
	after := previews()
	require.Len(t, after, 2)
	require.NotEqual(t, before[first.String()], after[first.String()])
	require.Equal(t, before[second.String()], after[second.String()])

	_, err = runtime.execOutput(ctx, id, []string{"sh", "-c", `mkdir -p "$HOME/.kuayle/ports/requests" && cd "$HOME/.kuayle/ports/requests" && printf '{"port":8090}' > expose-8090.json && ln -s /etc/passwd expose-9000.json && printf x > notes.txt`})
	require.NoError(t, err)
	scan, err := runtime.ScanPorts(ctx, machine, services)
	require.NoError(t, err)
	require.Equal(t, []int{8090, 8091}, scan.Preview)
	require.NotContains(t, scan.Listening, 8090)
	require.Equal(t, []PortRequest{{Name: "expose-8090.json", Body: []byte(`{"port":8090}`)}}, scan.Requests, "symlinks are never read")
	left, err := runtime.execOutput(ctx, id, []string{"sh", "-c", `ls -A "$HOME/.kuayle/ports/requests"`})
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(left), "every request entry is removed")
	require.NoError(t, runtime.WritePortState(ctx, services, []byte(`{"ports":[]}`)))
	state, err := runtime.execOutput(ctx, id, []string{"sh", "-c", `cat "$HOME/.kuayle/ports/state.json"`})
	require.NoError(t, err)
	require.Equal(t, `{"ports":[]}`, state)

	require.NoError(t, runtime.Pause(ctx, machine, nil))
	paused, err := runtime.client.ContainerInspect(ctx, after[first.String()], client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, paused.Container.State.Paused, "previews pause with the machine")
	require.NoError(t, runtime.pausePreviews(ctx, machine, false))

	require.NoError(t, runtime.StopPreview(ctx, machine, services, second))
	_, err = runtime.client.ContainerInspect(ctx, after[second.String()], client.ContainerInspectOptions{})
	require.True(t, errdefs.IsNotFound(err), "a stopped preview's container is removed")
	require.Eventually(t, func() bool { return fetch(firstHost, "8090") == "preview-ok isolated 1000" }, 20*time.Second, 200*time.Millisecond, "the other task's preview keeps running")
	require.NoError(t, runtime.StopPreview(ctx, machine, services, second), "stopping again is a no-op")

	require.NoError(t, runtime.RemovePreviews(ctx, machine))
	require.Empty(t, previews(), "a stopping machine takes its previews with it")
	_, err = runtime.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	require.NoError(t, err, "the developer container is not a preview")
}
