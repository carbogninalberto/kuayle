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

// The captured diff includes edits and new files, against the base branch,
// without changing the checkout's real index. Needs kuayle/dev-machine-headless:dev.
func TestDockerGitStateCapturesTheWorktreeDiff(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	setup := `set -e
mkdir -p /workspace/tasks/t && cd /workspace/tasks/t
git init -q -b main && git config user.email t@t && git config user.name t
printf 'one\n' > kept.txt && git add . && git commit -qm base
git update-ref refs/remotes/origin/main HEAD
printf 'two\n' > kept.txt && printf 'new\n' > added.txt
chown -R 1000:1000 /workspace
sleep 300`
	created, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:   "kuayle-diff-test-" + uuid.NewString()[:8],
		Config: &container.Config{Image: "kuayle/dev-machine-headless:dev", User: "0:0", Entrypoint: []string{"sh", "-c", setup}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	})
	_, err = runtime.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	id := created.ID
	require.Eventually(t, func() bool {
		_, err := runtime.execOutput(ctx, id, []string{"test", "-f", "/workspace/tasks/t/added.txt"})
		return err == nil
	}, 30*time.Second, 300*time.Millisecond)
	time.Sleep(500 * time.Millisecond) // chown runs after the files appear
	services := []domain.DevMachineService{{ServiceType: "terminal", ContainerID: &id}}
	state, err := runtime.GitState(ctx, &domain.DevMachine{BaseBranch: "main"}, services, &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/t", BaseBranch: "main"})
	require.NoError(t, err)
	require.Contains(t, state.Diff, "diff --git a/kept.txt b/kept.txt")
	require.Contains(t, state.Diff, "+two")
	require.Contains(t, state.Diff, "diff --git a/added.txt b/added.txt", "new files are included")
	require.False(t, state.DiffTruncated)
	staged, err := runtime.execOutput(ctx, id, []string{"git", "-C", "/workspace/tasks/t", "diff", "--cached", "--name-only"})
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(staged), "the checkout's real index is untouched")
}

func TestCapDiffKeepsTheLimitOnACharacterBoundary(t *testing.T) {
	small, truncated := capDiff("+ok\n")
	require.Equal(t, "+ok\n", small)
	require.False(t, truncated)
	big, truncated := capDiff(strings.Repeat("é", maxStoredDiffBytes))
	require.True(t, truncated)
	require.LessOrEqual(t, len(big), maxStoredDiffBytes)
	require.True(t, strings.HasSuffix(big, "é"), "never cut a character in half")
}
