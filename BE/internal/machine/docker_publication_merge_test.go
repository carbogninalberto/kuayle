package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Base-branch merges and commits people pushed to the task branch on GitHub are
// published as they are: only the agent's own commits are judged and folded.
func TestPublishScriptsKeepBaseMergesAndCommitsAlreadyOnGitHub(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	isolated := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME=" + root}
	as := func(name, email string) []string {
		return []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email}
	}
	kuayle, bot, human := as("Kuayle Agent", "agent@kuayle.invalid"), as("app[bot]", "1+app[bot]@users.noreply.github.com"), as("Maintainer", "maintainer@example.invalid")
	run := func(dir string, env []string, args ...string) string {
		t.Helper()
		command := exec.Command(realGit, args...)
		command.Dir = dir
		command.Env = append(append(os.Environ(), isolated...), env...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	write := func(dir, name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	commit := func(dir string, env []string, name, message string) string {
		t.Helper()
		write(dir, name, name+"\n")
		run(dir, env, "add", name)
		run(dir, env, "commit", "-q", "-m", message)
		return run(dir, nil, "rev-parse", "HEAD")
	}

	origin, seed, people := filepath.Join(root, "origin.git"), filepath.Join(root, "seed"), filepath.Join(root, "people")
	run(root, nil, "init", "-q", "-b", "main", seed)
	commit(seed, human, "README.md", "base")
	run(root, nil, "clone", "-q", "--bare", seed, origin)
	run(root, nil, "clone", "-q", origin, people)
	cache := filepath.Join(root, "repos", "acme-app.git")
	run(root, nil, "clone", "-q", "--bare", origin, cache)
	run(root, nil, "--git-dir="+cache, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	fetch := func() { run(root, nil, "--git-dir="+cache, "fetch", "-q", "--prune", "origin") }
	fetch()
	worktree := filepath.Join(root, "tasks", "rec-1")
	run(root, nil, "--git-dir="+cache, "worktree", "add", "-q", "-b", "kuayle/rec-1", worktree, "refs/remotes/origin/main")
	advanceMain := func(name string) string {
		t.Helper()
		run(people, nil, "checkout", "-q", "main")
		run(people, nil, "pull", "-q", "--ff-only")
		tip := commit(people, human, name, "Human "+name)
		run(people, nil, "push", "-q", "origin", "main")
		fetch()
		return tip
	}

	wrapper := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(wrapper, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wrapper, "git"), []byte("#!/bin/sh\nfor arg do\n  shift\n  [ \"$arg\" = https://github.com/acme/app ] && arg="+origin+"\n  set -- \"$@\" \"$arg\"\ndone\nexec "+realGit+" \"$@\"\n"), 0o755))
	script := func(body, input string, args ...string) (string, error) {
		command := exec.Command("sh", append([]string{"-c", body, "kuayle-publish"}, args...)...)
		command.Env = append(os.Environ(), "PATH="+wrapper+":"+os.Getenv("PATH"), "HOME="+root)
		command.Stdin = strings.NewReader(input)
		output, err := command.CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}
	publish := func(floor string) string {
		t.Helper()
		args := []string{worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid"}
		if floor != "" {
			args = append(args, floor)
		}
		published, err := script(publishCommitScript, "REC-1: Dark mode", args...)
		require.NoError(t, err, published)
		require.Regexp(t, publishCommitPattern, published)
		return published
	}
	push := func(published string) (string, error) {
		return script(publishPushScript, "fixture-publication-credential\n", "acme/app", "kuayle/rec-1", worktree, published, "main", "agent@kuayle.invalid")
	}
	pushed := func(published string) {
		t.Helper()
		output, err := push(published)
		require.NoError(t, err, output)
		require.Equal(t, published, run(origin, nil, "rev-parse", "refs/heads/kuayle/rec-1"))
	}
	format := func(spec, rev string) string { return run(worktree, nil, "log", "-1", "--format="+spec, rev) }
	aheadOfMain := func(rev string) string { return run(worktree, nil, "rev-list", "refs/remotes/origin/main.."+rev) }
	changed := func(from, to string) string { return run(worktree, nil, "diff", "--name-only", from, to) }

	write(worktree, "feature.txt", "dark mode\n")
	first := publish("")
	pushed(first)

	// The agent merges an updated base and commits under its own identity: the
	// fold keeps the merge, so the branch is not shown behind its base.
	mainTip := advanceMain("main-1.txt")
	run(worktree, bot, "merge", "-q", "--no-edit", "refs/remotes/origin/main")
	commit(worktree, bot, "fix.txt", "Fix\n\nCo-Authored-By: Claude Code <noreply@anthropic.com>")
	write(worktree, "pending.txt", "pending\n")
	folded := publish(first)
	require.Equal(t, first+" "+mainTip, format("%P", folded), "a merge of the published commit and the base tip")
	require.Equal(t, "Kuayle Agent <agent@kuayle.invalid> Kuayle Agent <agent@kuayle.invalid>", format("%an <%ae> %cn <%ce>", folded))
	require.Equal(t, "REC-1: Dark mode", format("%B", folded))
	require.Equal(t, folded+"\n"+first, aheadOfMain(folded), "the base's commits are not re-added")
	require.Equal(t, "feature.txt\nfix.txt\npending.txt", changed("refs/remotes/origin/main", folded))
	pushed(folded)
	require.Equal(t, folded, publish(folded), "a retry keeps Kuayle's own merge")

	// A merge of the base alone, by the agent's identity, is folded the same way.
	mainTip = advanceMain("main-2.txt")
	run(worktree, bot, "merge", "-q", "--no-edit", "refs/remotes/origin/main")
	agentMerge := run(worktree, nil, "rev-parse", "HEAD")
	mergeOnly := publish(folded)
	require.Equal(t, folded+" "+mainTip, format("%P", mergeOnly))
	require.Equal(t, "Kuayle Agent <agent@kuayle.invalid>", format("%an <%ae>", mergeOnly))
	require.Equal(t, format("%T", agentMerge), format("%T", mergeOnly), "the merge result is unchanged")
	pushed(mergeOnly)

	// A merge already made by the configured author is published unchanged.
	advanceMain("main-3.txt")
	run(worktree, kuayle, "merge", "-q", "--no-edit", "refs/remotes/origin/main")
	ownMerge := run(worktree, nil, "rev-parse", "HEAD")
	require.Equal(t, ownMerge, publish(mergeOnly))
	pushed(ownMerge)

	// A maintainer merges the base into the task branch on GitHub; the checkout
	// picks it up and the agent commits on top. Only the agent's commit is folded.
	advanceMain("main-4.txt")
	run(people, nil, "fetch", "-q", "origin")
	run(people, nil, "checkout", "-q", "-B", "kuayle/rec-1", "origin/kuayle/rec-1")
	run(people, human, "merge", "-q", "--no-edit", "origin/main")
	humanMerge := run(people, nil, "rev-parse", "HEAD")
	run(people, nil, "push", "-q", "origin", "kuayle/rec-1")
	fetch()
	run(worktree, nil, "merge", "-q", "--ff-only", "refs/remotes/origin/kuayle/rec-1")
	commit(worktree, bot, "after.txt", "After review")
	onHuman := publish(ownMerge)
	require.Equal(t, humanMerge, format("%P", onHuman), "the maintainer's merge is kept")
	require.Equal(t, "Kuayle Agent <agent@kuayle.invalid>", format("%an <%ae>", onHuman))
	require.Equal(t, "after.txt", changed(humanMerge, onHuman))
	pushed(onHuman)

	// The checkout's remote-tracking refs are agent-writable. A base ref moved
	// onto the agent's own commit hides it from the fold, but the push step
	// checks against GitHub and refuses to publish it.
	forged := commit(worktree, bot, "forged.txt", "Forged")
	run(worktree, nil, "update-ref", "refs/remotes/origin/main", forged)
	commit(worktree, bot, "later.txt", "Later")
	smuggled := publish(onHuman)
	require.Equal(t, onHuman+" "+forged, format("%P", smuggled))
	output, err := push(smuggled)
	require.Error(t, err)
	require.Contains(t, output, "another author")
	require.Equal(t, onHuman, run(origin, nil, "rev-parse", "refs/heads/kuayle/rec-1"), "nothing was pushed")
}

func TestPublishScriptsJudgeAuthorsWhenTheConfiguredEmailStartsWithADash(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME=" + root,
		"GIT_AUTHOR_NAME=app[bot]", "GIT_AUTHOR_EMAIL=1+app[bot]@users.noreply.github.com",
		"GIT_COMMITTER_NAME=app[bot]", "GIT_COMMITTER_EMAIL=1+app[bot]@users.noreply.github.com"}
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command(realGit, args...)
		command.Dir = root
		command.Env = append(os.Environ(), env...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("base\n"), 0o644))
	run("add", "README.md")
	run("commit", "-q", "-m", "base")
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("checkout", "-q", "-b", "kuayle/rec-1")
	require.NoError(t, os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0o644))
	run("add", "feature.txt")
	run("commit", "-q", "-m", "Agent commit")

	command := exec.Command("sh", "-c", publishCommitScript, "kuayle-publish", root, "kuayle/rec-1", "main", "Kuayle Agent", "-agent@kuayle.invalid")
	command.Env = append(os.Environ(), "HOME="+root)
	command.Stdin = strings.NewReader("REC-1: Feature")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	published := strings.TrimSpace(string(output))
	require.Equal(t, "-agent@kuayle.invalid -agent@kuayle.invalid", run("log", "-1", "--format=%ae %ce", published),
		"a commit by another author is folded even when the configured email looks like an option")
}

func TestPublishScriptsKeepTheBaseOfARebaseOntoANewerBase(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	as := func(name, email string) []string {
		return []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email}
	}
	kuayle, bot := as("Kuayle Agent", "agent@kuayle.invalid"), as("app[bot]", "1+app[bot]@users.noreply.github.com")
	run := func(env []string, args ...string) string {
		t.Helper()
		command := exec.Command(realGit, args...)
		command.Dir = root
		command.Env = append(append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME="+root), env...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	commit := func(env []string, name string) string {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), 0o644))
		run(env, "add", name)
		run(env, "commit", "-q", "-m", name)
		return run(nil, "rev-parse", "HEAD")
	}
	run(nil, "init", "-q", "-b", "main")
	commit(bot, "README.md")
	run(nil, "update-ref", "refs/remotes/origin/main", "HEAD")
	run(nil, "checkout", "-q", "-b", "kuayle/rec-1")
	published := commit(kuayle, "feature.txt")
	run(nil, "checkout", "-q", "main")
	mainTip := commit(bot, "main-1.txt")
	run(nil, "update-ref", "refs/remotes/origin/main", mainTip)
	run(nil, "checkout", "-q", "kuayle/rec-1")
	// The agent rebases the published work onto the newer base, so the
	// published commit is no longer an ancestor of the checkout.
	run(bot, "rebase", "-q", "refs/remotes/origin/main")

	command := exec.Command("sh", "-c", publishCommitScript, "kuayle-publish", root, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", published)
	command.Env = append(os.Environ(), "HOME="+root)
	command.Stdin = strings.NewReader("REC-1: Feature")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	rebuilt := strings.TrimSpace(string(output))
	require.Equal(t, published+" "+mainTip, run(nil, "log", "-1", "--format=%P", rebuilt), "rebuilt on the published commit, merging the newer base")
	require.Equal(t, "agent@kuayle.invalid agent@kuayle.invalid", run(nil, "log", "-1", "--format=%ae %ce", rebuilt))
	require.Equal(t, "feature.txt", run(nil, "diff", "--name-only", "refs/remotes/origin/main", rebuilt), "the base's commits are not re-added")
}

// The checkout's git directory is not used as one, so a merge the agent left
// uncommitted there is still published as a merge and concluded.
func TestPublishScriptsConcludeAMergeLeftUncommitted(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command(realGit, args...)
		command.Dir = root
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME="+root,
			"GIT_AUTHOR_NAME=Kuayle Agent", "GIT_AUTHOR_EMAIL=agent@kuayle.invalid", "GIT_COMMITTER_NAME=Kuayle Agent", "GIT_COMMITTER_EMAIL=agent@kuayle.invalid")
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	commit := func(name string) string {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), 0o644))
		run("add", name)
		run("commit", "-q", "-m", name)
		return run("rev-parse", "HEAD")
	}
	run("init", "-q", "-b", "main")
	commit("README.md")
	run("checkout", "-q", "-b", "kuayle/rec-1")
	published := commit("feature.txt")
	run("checkout", "-q", "main")
	mainTip := commit("main-1.txt")
	run("update-ref", "refs/remotes/origin/main", mainTip)
	run("checkout", "-q", "kuayle/rec-1")
	run("merge", "-q", "--no-commit", "refs/remotes/origin/main")

	command := exec.Command("sh", "-c", publishCommitScript, "kuayle-publish", root, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", published)
	command.Env = append(os.Environ(), "HOME="+root)
	command.Stdin = strings.NewReader("REC-1: Feature")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	merged := strings.TrimSpace(string(output))
	require.Equal(t, published+" "+mainTip, run("log", "-1", "--format=%P", merged))
	require.Equal(t, merged, run("rev-parse", "HEAD"))
	require.NoFileExists(t, filepath.Join(root, ".git", "MERGE_HEAD"))
	require.Empty(t, run("status", "--porcelain"))
}
