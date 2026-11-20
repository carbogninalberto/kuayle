package machine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestDockerAssistantProcessStartsInAReadOnlyContainer(t *testing.T) {
	machine, services, _ := previewFixture()
	docker := &previewDockerFake{}
	runtime := &DockerRuntime{client: docker.serve(t)}
	process := &domain.AssistantProcess{ID: uuid.New(), MachineID: &machine.ID, WorkspaceID: machine.WorkspaceID,
		Command: `python3 -m http.server "$PORT" --bind "$HOST"`, WorkingDirectory: "/workspace", Port: 4321,
		ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, runtime.StartAssistantProcess(t.Context(), machine, services, process))
	running, err := runtime.AssistantProcessRunning(t.Context(), process)
	require.NoError(t, err)
	require.True(t, running)
	require.Len(t, docker.created, 1)
	created := docker.created[0]
	require.True(t, created.HostConfig.ReadonlyRootfs)
	require.True(t, created.HostConfig.Mounts[0].ReadOnly)
	require.Equal(t, "/workspace", created.HostConfig.Mounts[0].Target)
	require.Empty(t, created.HostConfig.PortBindings)
	require.Equal(t, "1000:1000", created.Config.User)
	require.Contains(t, created.Config.Env, "PORT=4321")
	require.Contains(t, created.Config.Env, "HOST=0.0.0.0")
	require.NotContains(t, strings.Join(created.Config.Cmd, " "), process.Command)
	require.NoError(t, runtime.RemoveAssistantProcess(t.Context(), process))
	require.NoError(t, runtime.RemoveAssistantProcess(t.Context(), process))
}

func TestDockerAssistantProcessServesAndExpires(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	machine, _, _ := previewFixture()
	name := "kuayle-process-test-" + machine.ID.String()
	machine.DockerNetworkName, machine.WorkspaceVolumeName = &name, &name
	labels := machineLabels(machine)
	_, err = runtime.client.NetworkCreate(ctx, name, client.NetworkCreateOptions{Labels: labels})
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = runtime.client.NetworkRemove(context.Background(), name, client.NetworkRemoveOptions{}) })
	_, err = runtime.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: labels})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.VolumeRemove(context.Background(), name, client.VolumeRemoveOptions{Force: true})
	})
	services := []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "alpine:3"}}
	process := &domain.AssistantProcess{ID: uuid.New(), MachineID: &machine.ID, WorkspaceID: machine.WorkspaceID,
		Command: `set -eu
if touch /workspace/forbidden 2>/dev/null; then exit 1; fi
while true; do printf 'HTTP/1.0 200 OK\r\n\r\npreview-ok\n' | nc -l -p "$PORT"; done`,
		WorkingDirectory: "/workspace", Port: 4321, ExpiresAt: time.Now().Add(15 * time.Second)}
	t.Cleanup(func() { _ = runtime.RemoveAssistantProcess(context.Background(), process) })
	require.NoError(t, runtime.StartAssistantProcess(ctx, machine, services, process))
	found, err := runtime.assistantProcessContainer(ctx, process)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Eventually(t, func() bool {
		output, err := runtime.execOutput(ctx, found.ID, []string{"wget", "-Y", "off", "-q", "-O-", "http://" + domain.AssistantProcessHost(process.ID) + ":4321/"})
		return err == nil && strings.TrimSpace(output) == "preview-ok"
	}, 3*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		running, err := runtime.AssistantProcessRunning(ctx, process)
		return err == nil && !running
	}, 20*time.Second, 100*time.Millisecond, "the container expires without a manager")
	require.NoError(t, runtime.RemoveAssistantProcess(ctx, process))
}
