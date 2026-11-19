package machine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTaskCleanupRejectsAnotherTasksPaths(t *testing.T) {
	runtime := &DockerRuntime{}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	task := uuid.New()
	checkout := &domain.DevMachineCheckout{
		MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: task.String(), RepositoryFullName: "acme/app",
		WorkspacePath: "/workspace/tasks/rec-1-" + uuid.NewString(),
	}
	err := runtime.CleanupTaskResources(context.Background(), machine, nil, checkout, task, nil)
	require.ErrorContains(t, err, "Checkout paths do not match the task")
}

func TestTaskCleanupRemovesOnlyRegisteredWorktreeAndRunDirectories(t *testing.T) {
	e := newGitScriptEnv(t)
	seed := e.remote("acme/app", "README.md")
	root := filepath.Join(e.root, "workspace")
	repo := filepath.Join(root, "repos", "acme", "app.git")
	first, second := filepath.Join(root, "tasks", "first"), filepath.Join(root, "tasks", "second")
	run := filepath.Join(root, ".kuayle", "tmp", "agent-1")
	otherRun := filepath.Join(root, ".kuayle", "tmp", "agent-2")
	for _, dir := range []string{filepath.Dir(repo), filepath.Dir(first), run, otherRun} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	for _, checkout := range []string{first, second} {
		out, err := e.runCheckout(root, "acme/app", checkout)
		require.NoError(t, err, out)
	}
	e.git(first, "commit", "--allow-empty", "-qm", "local task commit")
	head := e.git(first, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(first, "untracked"), []byte("discard"), 0o600))
	require.NoError(t, os.Chmod(run, 0o500))
	clean := func(worktree string, runs ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", append([]string{"-c", taskWorkspaceCleanupScript, "kuayle-task-cleanup", root, "acme", "app", worktree}, runs...)...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := clean("first", "agent-1")
	require.NoError(t, err, out)
	require.Contains(t, out, first)
	require.Contains(t, out, run)
	require.NoDirExists(t, first)
	require.NoDirExists(t, run)
	require.DirExists(t, second)
	require.DirExists(t, otherRun)
	current, err := os.Readlink(filepath.Join(root, "current"))
	require.NoError(t, err)
	require.Equal(t, second, current)
	require.NotContains(t, e.git("", "--git-dir="+repo, "worktree", "list", "--porcelain"), "worktree "+first)
	require.Equal(t, head, e.git("", "--git-dir="+repo, "rev-parse", "refs/heads/kuayle/first"))

	out, err = clean("first", "agent-1")
	require.NoError(t, err, out)
	require.Empty(t, out)
	// A published branch can have newer local commits to carry into a retry.
	e.git(second, "push", "origin", "HEAD:refs/heads/kuayle/first")
	out, err = e.runCheckout(root, "acme/app", first)
	require.NoError(t, err, out)
	require.Equal(t, head, e.git(first, "rev-parse", "HEAD"))
	require.NoFileExists(t, filepath.Join(first, "untracked"))

	e.git(first, "push", "origin", "HEAD:refs/heads/kuayle/first")
	out, err = clean("first")
	require.NoError(t, err, out)
	e.git(seed, "fetch", "origin", "kuayle/first")
	e.git(seed, "checkout", "-B", "kuayle/first", "FETCH_HEAD")
	e.git(seed, "commit", "--allow-empty", "-qm", "upstream task commit")
	newHead := e.git(seed, "rev-parse", "HEAD")
	e.git(seed, "push", "origin", "HEAD:refs/heads/kuayle/first")
	out, err = e.runCheckout(root, "acme/app", first)
	require.NoError(t, err, out)
	require.Equal(t, newHead, e.git(first, "rev-parse", "HEAD"))

	unrelated := filepath.Join(root, "tasks", "unregistered")
	require.NoError(t, os.Mkdir(unrelated, 0o755))
	out, err = clean("unregistered")
	require.Error(t, err, out)
	require.DirExists(t, unrelated)

	require.NoError(t, os.RemoveAll(filepath.Join(root, ".kuayle", "tmp")))
	require.NoError(t, os.Symlink(second, filepath.Join(root, ".kuayle", "tmp")))
	out, err = clean("first", "agent-2")
	require.Error(t, err, out)
	require.DirExists(t, first)
	require.DirExists(t, second)

	require.NoError(t, os.Remove(filepath.Join(root, ".kuayle", "tmp")))
	require.NoError(t, os.RemoveAll(second))
	out, err = clean("second")
	require.NoError(t, err, out)
	require.NotContains(t, e.git("", "--git-dir="+repo, "worktree", "list", "--porcelain"), "worktree "+second)
	require.DirExists(t, first)
}

func TestTaskCleanupRemovesOnlyLabeledVolumes(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", "/run/fixture")
	t.Setenv("DOCKER_HOST", "tcp://host-daemon")
	t.Setenv("DOCKER_CONTEXT", "host")
	const fakeDocker = `#!/bin/sh
set -eu
[ "$DOCKER_HOST" = unix:///run/fixture/docker.sock ] && [ -z "${DOCKER_CONTEXT:-}" ]
case "$*" in
  "volume ls -q --filter label=com.docker.compose.project=rec-1-task")
    printf '%s\n' attached-cache vanished-volume rec1-gocache ;;
  "volume ls -q --filter label=com.kuayle.task-id=task")
    printf '%s\n' task-db rec1-gocache ;;
  "volume rm -- task-db"|"volume rm -- rec1-gocache")
    printf '%s\n' "$4" ;;
  "volume rm -- vanished-volume")
    printf '%s\n' 'get vanished-volume: no such volume' >&2
    exit 1 ;;
  "volume rm -- attached-cache")
    printf '%s\n' 'remove attached-cache: volume is in use - [running-container]' >&2
    exit 1 ;;
  *) echo "unexpected docker command: $*" >&2; exit 2 ;;
esac`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDocker), 0o700))
	cmd := exec.Command("sh", "-c", taskVolumeCleanupScript, "kuayle-task-volumes", "rec-1-task", "task")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, []string{"in use attached-cache", "rec1-gocache", "task-db"}, nonEmptyLines(string(out)))
}
