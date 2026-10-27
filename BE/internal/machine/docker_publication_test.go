package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The publication scripts run against real git. Only the network is replaced: a
// wrapper maps the fixed GitHub URL to a local bare "origin" repository.
func TestPublishScriptsIgnoreAgentGitConfigurationAndKeepTheCredentialOut(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	origin, evil := filepath.Join(root, "origin.git"), filepath.Join(root, "evil.git")
	run := func(dir string, env []string, args ...string) string {
		t.Helper()
		command := exec.Command(realGit, args...)
		command.Dir = dir
		command.Env = append(os.Environ(), env...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	identity := []string{"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid"}
	seed := filepath.Join(root, "seed")
	run(root, nil, "init", "-q", "-b", "main", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("base\n"), 0o644))
	run(seed, nil, "add", "README.md")
	run(seed, identity, "commit", "-q", "-m", "base")
	run(root, nil, "clone", "-q", "--bare", seed, origin)
	run(root, nil, "init", "-q", "--bare", evil)
	cache := filepath.Join(root, "repos", "acme-app.git")
	run(root, nil, "clone", "-q", "--bare", origin, cache)
	run(root, nil, "--git-dir="+cache, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	run(root, nil, "--git-dir="+cache, "fetch", "-q", "origin")
	worktree := filepath.Join(root, "tasks", "rec-1")
	run(root, nil, "--git-dir="+cache, "worktree", "add", "-q", "-b", "kuayle/rec-1", worktree, "refs/remotes/origin/main")

	// Everything an agent could write into the checkout's configuration.
	marker := filepath.Join(root, "executed")
	hooks := filepath.Join(root, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	hostile := "#!/bin/sh\necho \"$0\" >> " + marker + "\nenv >> " + marker + "\n"
	for _, name := range []string{"pre-commit", "commit-msg", "pre-push", "post-commit", "reference-transaction"} {
		require.NoError(t, os.WriteFile(filepath.Join(hooks, name), []byte(hostile), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "fsmonitor"), []byte(hostile), 0o755))
	run(worktree, nil, "config", "core.hooksPath", hooks)
	run(worktree, nil, "config", "core.fsmonitor", filepath.Join(root, "fsmonitor"))
	run(worktree, nil, "config", "url."+evil+".insteadOf", origin)
	run(worktree, nil, "config", "user.email", "agent-claimed@example.invalid")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "feature.txt"), []byte("dark mode\n"), 0o644))

	wrapper := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(wrapper, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wrapper, "git"), []byte("#!/bin/sh\nfor arg do\n  shift\n  [ \"$arg\" = https://github.com/acme/app ] && arg="+origin+"\n  set -- \"$@\" \"$arg\"\ndone\nexec "+realGit+" \"$@\"\n"), 0o755))
	script := func(body, name, input string, args ...string) (string, error) {
		command := exec.Command("sh", append([]string{"-c", body, name}, args...)...)
		command.Env = append(os.Environ(), "PATH="+wrapper+":"+os.Getenv("PATH"), "HOME="+root)
		command.Stdin = strings.NewReader(input)
		output, err := command.CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}
	const token = "fixture-publication-credential"
	commit, err := script(publishCommitScript, "commit", "REC-1: Dark mode\n\nKuayle agent task", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid")
	require.NoError(t, err, commit)
	require.Regexp(t, publishCommitPattern, commit)
	output, err := script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, commit)
	require.NoError(t, err, output)

	require.Equal(t, commit, run(origin, nil, "rev-parse", "refs/heads/kuayle/rec-1"))
	require.Equal(t, "Kuayle Agent <agent@kuayle.invalid>", run(origin, nil, "log", "-1", "--format=%an <%ae>", commit))
	require.Equal(t, "REC-1: Dark mode", run(origin, nil, "log", "-1", "--format=%s", commit))
	require.Empty(t, run(evil, nil, "for-each-ref"), "an agent-configured URL rewrite must not redirect the push")
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "agent hooks or fsmonitor ran during publication")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			content, _ := os.ReadFile(path)
			require.NotContains(t, string(content), token, path)
		}
		return nil
	})

	// A retry after a lost result republishes the same commit without changes.
	again, err := script(publishCommitScript, "commit", "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid")
	require.NoError(t, err, again)
	require.Equal(t, commit, again)
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, commit)
	require.NoError(t, err, output)

	// A branch that moved after the commit was recorded is not pushed.
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "late.txt"), []byte("late\n"), 0o644))
	run(worktree, identity, "-c", "core.hooksPath=/dev/null", "add", "late.txt")
	run(worktree, identity, "-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "late")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, commit)
	require.Error(t, err)
	require.Contains(t, output, "task branch moved")
	require.Equal(t, commit, run(origin, nil, "rev-parse", "refs/heads/kuayle/rec-1"))

	// Agent commits with made-up identities or co-author trailers are folded
	// into one commit by the configured author, on top of what was published.
	run(worktree, identity, "-c", "core.hooksPath=/dev/null", "reset", "-q", "--hard", commit)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "follow.txt"), []byte("review fix\n"), 0o644))
	bot := []string{"GIT_AUTHOR_NAME=app[bot]", "GIT_AUTHOR_EMAIL=1+app[bot]@users.noreply.github.com", "GIT_COMMITTER_NAME=app[bot]", "GIT_COMMITTER_EMAIL=1+app[bot]@users.noreply.github.com"}
	run(worktree, bot, "-c", "core.hooksPath=/dev/null", "add", "follow.txt")
	run(worktree, bot, "-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "Fix review\n\nCo-Authored-By: Claude Code <noreply@anthropic.com>")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "uncommitted.txt"), []byte("more\n"), 0o644))
	follow, err := script(publishCommitScript, "commit", "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", commit)
	require.NoError(t, err, follow)
	require.NotEqual(t, commit, follow)
	require.Equal(t, commit, run(worktree, nil, "rev-parse", follow+"^"), "the new commit builds on the published one")
	require.Equal(t, "Kuayle Agent <agent@kuayle.invalid>", run(worktree, nil, "log", "-1", "--format=%an <%ae>", follow))
	require.NotContains(t, run(worktree, nil, "log", "-1", "--format=%B", follow), "Co-Authored-By")
	require.Contains(t, run(worktree, nil, "show", "--name-only", "--format=", follow), "follow.txt")
	require.Contains(t, run(worktree, nil, "show", "--name-only", "--format=", follow), "uncommitted.txt")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, follow)
	require.NoError(t, err, output, "a fast-forward of the published branch")
	same, err := script(publishCommitScript, "commit", "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", follow)
	require.NoError(t, err, same)
	require.Equal(t, follow, same, "a retry keeps Kuayle's own commit")

	// Commits already on GitHub are never folded, whoever authored them, so the
	// next publication stays a fast-forward. Only Kuayle's record says what was
	// published; markers inside the agent-writable checkout are ignored.
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "bot.txt"), []byte("bot\n"), 0o644))
	run(worktree, bot, "-c", "core.hooksPath=/dev/null", "add", "bot.txt")
	run(worktree, bot, "-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "Bot change\n\nCo-Authored-By: Claude Code <noreply@anthropic.com>")
	botCommit := run(worktree, nil, "rev-parse", "HEAD")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, botCommit)
	require.NoError(t, err, output)
	run(worktree, nil, "-c", "core.hooksPath=/dev/null", "update-ref", "refs/kuayle/published", commit)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "after.txt"), []byte("after\n"), 0o644))
	after, err := script(publishCommitScript, "commit", "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", botCommit)
	require.NoError(t, err, after)
	require.Equal(t, botCommit, run(worktree, nil, "rev-parse", after+"^"), "the published bot commit is kept")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, after)
	require.NoError(t, err, output)

	// No changes relative to the base branch means nothing to publish.
	empty := filepath.Join(root, "tasks", "rec-2")
	run(root, nil, "--git-dir="+cache, "worktree", "add", "-q", "-b", "kuayle/rec-2", empty, "refs/remotes/origin/main")
	none, err := script(publishCommitScript, "commit", "REC-2: Nothing", empty, "kuayle/rec-2", "main", "Kuayle Agent", "agent@kuayle.invalid")
	require.NoError(t, err, none)
	require.Equal(t, "none", none)
	wrong, err := script(publishCommitScript, "commit", "REC-2: Nothing", empty, "kuayle/other", "main", "Kuayle Agent", "agent@kuayle.invalid")
	require.Error(t, err)
	require.Contains(t, wrong, "not on the task branch")
}
