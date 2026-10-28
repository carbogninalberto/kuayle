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

// Starts real preview servers for two tasks in a developer-like container,
// restarts and stops one without touching the other, and scans the listening
// ports and agent requests. Needs a local alpine:3 image.
func TestDockerPreviewsRunPerTaskAndPortsAreScanned(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	created, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "kuayle-preview-test-" + uuid.NewString()[:8],
		Config: &container.Config{Image: "alpine:3", Env: []string{"HOME=/home/kuayle"}, Entrypoint: []string{"sh", "-c",
			"mkdir -p /workspace/tasks/t /home/kuayle && echo preview-ok > /workspace/tasks/t/index.html && chown -R 1000:1000 /home/kuayle && chmod -R a+rX /workspace && sleep 300"}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	})
	_, err = runtime.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	id := created.ID
	services := []domain.DevMachineService{{ServiceType: "terminal", ContainerID: &id}}
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/t"}
	fetch := func(port string) string {
		out, _ := runtime.execOutput(ctx, id, []string{"sh", "-c", "wget -qO- http://127.0.0.1:" + port + "/ 2>/dev/null || true"})
		return strings.TrimSpace(out)
	}
	require.Eventually(t, func() bool {
		_, err := runtime.execOutput(ctx, id, []string{"test", "-d", "/home/kuayle"})
		return err == nil
	}, 10*time.Second, 200*time.Millisecond)

	// $PORT is provided, and the server runs from the task worktree.
	argv := []string{"sh", "-c", `while true; do { printf 'HTTP/1.0 200 OK\r\n\r\n'; cat index.html; } | nc -l -p "$PORT"; done`}
	first, second := uuid.New(), uuid.New()
	require.NoError(t, runtime.StartPreview(ctx, nil, services, checkout, first, argv, 8090))
	require.NoError(t, runtime.StartPreview(ctx, nil, services, checkout, second, argv, 8091))
	require.Eventually(t, func() bool { return fetch("8090") == "preview-ok" && fetch("8091") == "preview-ok" }, 10*time.Second, 200*time.Millisecond)
	servers := func() string {
		count, err := runtime.execOutput(ctx, id, []string{"sh", "-c", "ps -o args | grep -c '[w]hile true'"})
		require.NoError(t, err)
		return strings.TrimSpace(count)
	}
	require.NoError(t, runtime.StartPreview(ctx, nil, services, checkout, first, argv, 8090), "a restart replaces only that task's preview")
	require.Eventually(t, func() bool { return fetch("8090") == "preview-ok" }, 10*time.Second, 200*time.Millisecond)
	require.Equal(t, "2", servers())

	_, err = runtime.execOutput(ctx, id, []string{"sh", "-c", `mkdir -p "$HOME/.kuayle/ports/requests" && cd "$HOME/.kuayle/ports/requests" && printf '{"port":8090}' > expose-8090.json && ln -s /etc/passwd expose-9000.json && printf x > notes.txt`})
	require.NoError(t, err)
	scan, err := runtime.ScanPorts(ctx, services)
	require.NoError(t, err)
	require.Subset(t, scan.Listening, []int{8090, 8091})
	require.Equal(t, []PortRequest{{Name: "expose-8090.json", Body: []byte(`{"port":8090}`)}}, scan.Requests, "symlinks are never read")
	left, err := runtime.execOutput(ctx, id, []string{"sh", "-c", `ls -A "$HOME/.kuayle/ports/requests"`})
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(left), "every request entry is removed")
	require.NoError(t, runtime.WritePortState(ctx, services, []byte(`{"ports":[]}`)))
	state, err := runtime.execOutput(ctx, id, []string{"sh", "-c", `cat "$HOME/.kuayle/ports/state.json"`})
	require.NoError(t, err)
	require.Equal(t, `{"ports":[]}`, state)

	require.NoError(t, runtime.StopPreview(ctx, services, second))
	require.Eventually(t, func() bool { return servers() == "1" }, 10*time.Second, 200*time.Millisecond)
	require.Equal(t, "preview-ok", fetch("8090"), "the other task's preview keeps running")
}
