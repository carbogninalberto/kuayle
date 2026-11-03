package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The sync script runs against real git with only the network replaced, like
// the publication scripts.
func TestSyncScriptFastForwardsToCommitsOthersPushedAndNeverRewrites(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	identity := []string{"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid"}
	run := func(dir string, args ...string) string {
		t.Helper()
		command := exec.Command(realGit, append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		command.Dir = dir
		command.Env = append(os.Environ(), identity...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	commitFile := func(dir, name string) string {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644))
		run(dir, "add", name)
		run(dir, "commit", "-q", "-m", name)
		return run(dir, "rev-parse", "HEAD")
	}
	seed := filepath.Join(root, "seed")
	run(root, "init", "-q", "-b", "main", seed)
	commitFile(seed, "README.md")
	run(root, "clone", "-q", "--bare", seed, origin)
	worktree := filepath.Join(root, "checkout")
	run(root, "clone", "-q", origin, worktree)
	run(worktree, "checkout", "-q", "-b", "kuayle/rec-1")
	published := commitFile(worktree, "feature.txt")
	run(worktree, "push", "-q", origin, "kuayle/rec-1")

	wrapper := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(wrapper, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wrapper, "git"), []byte("#!/bin/sh\nfor arg do\n  shift\n  [ \"$arg\" = https://github.com/acme/app ] && arg="+origin+"\n  set -- \"$@\" \"$arg\"\ndone\nexec "+realGit+" \"$@\"\n"), 0o755))
	sync := func() (string, error) {
		command := exec.Command("sh", "-c", publishSyncScript, "sync", "acme/app", "kuayle/rec-1", worktree)
		command.Env = append(os.Environ(), "PATH="+wrapper+":"+os.Getenv("PATH"), "HOME="+root)
		command.Stdin = strings.NewReader("fixture-credential\n")
		output, err := command.CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}

	output, err := sync()
	require.NoError(t, err, output)
	require.Equal(t, published, run(worktree, "rev-parse", "HEAD"), "nothing to bring in")

	// A maintainer merges main into the pull request branch on GitHub.
	maintainer := filepath.Join(root, "maintainer")
	run(root, "clone", "-q", "-b", "kuayle/rec-1", origin, maintainer)
	run(maintainer, "checkout", "-q", "main")
	commitFile(maintainer, "main-only.txt")
	run(maintainer, "push", "-q", origin, "main")
	run(maintainer, "checkout", "-q", "kuayle/rec-1")
	run(maintainer, "merge", "-q", "--no-ff", "-m", "Merge branch 'main'", "main")
	merged := run(maintainer, "rev-parse", "HEAD")
	run(maintainer, "push", "-q", origin, "kuayle/rec-1")

	output, err = sync()
	require.NoError(t, err, output)
	require.Equal(t, merged, run(worktree, "rev-parse", "HEAD"), "the checkout fast-forwarded to the new head")

	// Local commits GitHub does not have, on top of what it has, are kept.
	local := commitFile(worktree, "local.txt")
	output, err = sync()
	require.NoError(t, err, output)
	require.Equal(t, local, run(worktree, "rev-parse", "HEAD"))

	// Diverged histories are reported, and neither side is touched.
	run(maintainer, "checkout", "-q", "kuayle/rec-1")
	commitFile(maintainer, "other.txt")
	pushed := run(maintainer, "rev-parse", "HEAD")
	run(maintainer, "push", "-q", origin, "kuayle/rec-1")
	output, err = sync()
	require.Error(t, err)
	require.Contains(t, output, "cannot be fast-forwarded")
	require.Equal(t, local, run(worktree, "rev-parse", "HEAD"))
	require.Equal(t, pushed, run(origin, "rev-parse", "refs/heads/kuayle/rec-1"))
}
