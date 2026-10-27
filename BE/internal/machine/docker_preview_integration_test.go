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

// Starts a real preview server in a developer-like container and replaces it.
// Needs a local alpine:3 image.
func TestDockerPreviewStartsInTheWorktreeAndReplacesThePrevious(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	created, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:   "kuayle-preview-test-" + uuid.NewString()[:8],
		Config: &container.Config{Image: "alpine:3", Entrypoint: []string{"sh", "-c", "mkdir -p /workspace/tasks/t && echo preview-ok > /workspace/tasks/t/index.html && chmod -R a+rX /workspace && sleep 300"}},
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
	fetch := func() string {
		out, _ := runtime.execOutput(ctx, id, []string{"sh", "-c", "wget -qO- http://127.0.0.1:8090/ 2>/dev/null || true"})
		return strings.TrimSpace(out)
	}
	require.Eventually(t, func() bool {
		_, err := runtime.execOutput(ctx, id, []string{"test", "-f", "/workspace/tasks/t/index.html"})
		return err == nil
	}, 10*time.Second, 200*time.Millisecond)

	// $PORT is provided, and the server runs from the task worktree.
	// A tiny server loop that serves the worktree's index.html on $PORT.
	argv := []string{"sh", "-c", `while true; do { printf 'HTTP/1.0 200 OK\r\n\r\n'; cat index.html; } | nc -l -p "$PORT"; done`}
	require.NoError(t, runtime.StartPreview(ctx, nil, services, checkout, argv, 8090))
	require.Eventually(t, func() bool { return fetch() == "preview-ok" }, 10*time.Second, 200*time.Millisecond)

	require.NoError(t, runtime.StartPreview(ctx, nil, services, checkout, argv, 8090), "a new attempt replaces the running preview")
	require.Eventually(t, func() bool { return fetch() == "preview-ok" }, 10*time.Second, 200*time.Millisecond)
	count, err := runtime.execOutput(ctx, id, []string{"sh", "-c", "ps -o args | grep -c '[w]hile true'"})
	require.NoError(t, err)
	require.Equal(t, "1", strings.TrimSpace(count), "the previous preview process was stopped")
}
