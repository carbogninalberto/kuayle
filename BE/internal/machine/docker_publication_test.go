package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	output, err := script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, commit, "main", "agent@kuayle.invalid")
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
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, commit, "main", "agent@kuayle.invalid")
	require.NoError(t, err, output)

	// A branch that moved after the commit was recorded is not pushed.
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "late.txt"), []byte("late\n"), 0o644))
	run(worktree, identity, "-c", "core.hooksPath=/dev/null", "add", "late.txt")
	run(worktree, identity, "-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "late")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, commit, "main", "agent@kuayle.invalid")
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
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, follow, "main", "agent@kuayle.invalid")
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
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, botCommit, "main", "agent@kuayle.invalid")
	require.Error(t, err, "Kuayle never publishes another author's commit itself")
	require.Contains(t, output, "another author")
	run(origin, nil, "fetch", "-q", cache, "+refs/heads/kuayle/rec-1:refs/heads/kuayle/rec-1")
	require.Equal(t, botCommit, run(origin, nil, "rev-parse", "refs/heads/kuayle/rec-1"))
	run(worktree, nil, "-c", "core.hooksPath=/dev/null", "update-ref", "refs/kuayle/published", commit)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "after.txt"), []byte("after\n"), 0o644))
	after, err := script(publishCommitScript, "commit", "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", botCommit)
	require.NoError(t, err, after)
	require.Equal(t, botCommit, run(worktree, nil, "rev-parse", after+"^"), "the published bot commit is kept")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, after, "main", "agent@kuayle.invalid")
	require.NoError(t, err, output)

	// A checkout whose history was rewritten below the published commit (by an
	// agent, or an earlier fold) is rebuilt on top of it with the same tree.
	run(worktree, identity, "-c", "core.hooksPath=/dev/null", "reset", "-q", "--soft", "refs/remotes/origin/main")
	run(worktree, []string{"GIT_AUTHOR_NAME=Kuayle Agent", "GIT_AUTHOR_EMAIL=agent@kuayle.invalid", "GIT_COMMITTER_NAME=Kuayle Agent", "GIT_COMMITTER_EMAIL=agent@kuayle.invalid"}, "-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "rewritten")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "rewrite.txt"), []byte("rewrite\n"), 0o644))
	rebuilt, err := script(publishCommitScript, "commit", "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", after)
	require.NoError(t, err, rebuilt)
	require.Equal(t, after, run(worktree, nil, "rev-parse", rebuilt+"^"))
	require.Equal(t, "rewrite.txt", run(worktree, nil, "show", "--name-only", "--format=", rebuilt), "only the new change is on top")
	output, err = script(publishPushScript, "push", token+"\n", "acme/app", "kuayle/rec-1", worktree, rebuilt, "main", "agent@kuayle.invalid")
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

// Publication runs as the developer container's user, next to that container's
// secrets and sign-ins, while agent runs can write the checkout's git
// configuration, attributes and nested repositories. None of the programs they
// name may run while the checkout is synced, committed or pushed.
func TestPublishScriptsNeverRunProgramsNamedByTheCheckout(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	isolated := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME=" + root}
	as := func(name, email string) []string {
		return []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email}
	}
	bot, human := as("app[bot]", "1+app[bot]@users.noreply.github.com"), as("Maintainer", "maintainer@example.invalid")
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
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	// The repository itself applies the agent's drivers to every path.
	origin, seed := filepath.Join(root, "origin.git"), filepath.Join(root, "seed")
	run(root, nil, "init", "-q", "-b", "main", seed)
	write(seed, "README.md", "base\n")
	write(seed, ".gitattributes", "* filter=spy diff=spy merge=spy\n")
	run(seed, nil, "add", "README.md", ".gitattributes")
	run(seed, human, "commit", "-q", "-m", "base")
	run(root, nil, "clone", "-q", "--bare", seed, origin)
	cache := filepath.Join(root, "repos", "acme-app.git")
	run(root, nil, "clone", "-q", "--bare", origin, cache)
	run(root, nil, "--git-dir="+cache, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	run(root, nil, "--git-dir="+cache, "fetch", "-q", "origin")
	worktree := filepath.Join(root, "tasks", "rec-1")
	run(root, nil, "--git-dir="+cache, "worktree", "add", "-q", "-b", "kuayle/rec-1", worktree, "refs/remotes/origin/main")

	// The agent's work: a commit under its own identity that includes a nested
	// repository, a later commit inside that repository, an untracked file, and
	// Kuayle's own files, which the repository's info/exclude keeps out.
	nested := filepath.Join(worktree, "vendor")
	run(root, nil, "init", "-q", "-b", "main", nested)
	write(nested, "lib.txt", "one\n")
	run(nested, nil, "add", "lib.txt")
	run(nested, bot, "commit", "-q", "-m", "lib")
	write(worktree, "feature.txt", "dark mode\n")
	run(worktree, nil, "-c", "advice.addEmbeddedRepo=false", "add", "-A")
	run(worktree, bot, "commit", "-q", "-m", "Agent work")
	write(nested, "more.txt", "two\n")
	run(nested, nil, "add", "more.txt")
	run(nested, bot, "commit", "-q", "-m", "more")
	nestedHead := run(nested, nil, "rev-parse", "HEAD")
	write(worktree, "pending.txt", "pending\n")
	write(worktree, ".kuayle/outbox/result.json", "{}\n")
	write(cache, "info/exclude", "/.kuayle/\n")
	cleanConfig, err := os.ReadFile(filepath.Join(cache, "config"))
	require.NoError(t, err)

	// Everything the checkout can name a program in. Each one records that it ran.
	marker, spy := filepath.Join(root, "executed"), filepath.Join(root, "spy")
	require.NoError(t, os.WriteFile(spy, []byte("#!/bin/sh\necho \"$0 $*\" >> "+marker+"\ncat 2>/dev/null\nexit 0\n"), 0o755))
	hooks := filepath.Join(root, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	events := []string{"pre-commit", "commit-msg", "post-commit", "reference-transaction", "post-merge", "post-checkout", "post-index-change", "pre-auto-gc", "pre-push"}
	for _, name := range events {
		require.NoError(t, os.WriteFile(filepath.Join(hooks, name), []byte("#!/bin/sh\nexec "+spy+" hook "+name+"\n"), 0o755))
	}
	hostile := [][2]string{
		{"filter.spy.clean", spy + " clean"}, {"filter.spy.smudge", spy + " smudge"},
		{"diff.spy.textconv", spy + " textconv"}, {"diff.spy.command", spy + " diff"}, {"diff.external", spy + " external-diff"},
		{"merge.spy.driver", spy + " merge %A"},
		{"core.hooksPath", hooks}, {"hook.spy.command", spy + " config-hook"},
		{"core.fsmonitor", spy + " fsmonitor"}, {"core.sshCommand", spy + " ssh"}, {"core.gitProxy", spy + " proxy"},
		{"core.pager", spy + " pager"}, {"core.editor", spy + " editor"}, {"sequence.editor", spy + " sequence-editor"},
		{"core.askPass", spy + " askpass"}, {"credential.helper", "!" + spy + " credential"},
		{"core.alternateRefsCommand", spy + " alternate-refs"}, {"uploadpack.packObjectsHook", spy + " pack-objects"},
		{"gpg.program", spy + " gpg"}, {"commit.gpgSign", "true"}, {"log.showSignature", "true"},
		{"alias.add", "!" + spy + " alias"}, {"alias.commit", "!" + spy + " alias"}, {"alias.fetch", "!" + spy + " alias"}, {"alias.merge", "!" + spy + " alias"},
		{"protocol.ext.allow", "always"}, {"gc.auto", "1"},
		{"remote.origin.uploadpack", spy + " upload-pack"}, {"remote.origin.receivepack", spy + " receive-pack"},
		{"url.ext::" + spy + " transport .insteadOf", origin}, {"url.ext::" + spy + " transport-tmp .insteadOf", "/tmp/"},
		{"url.ext::" + spy + " transport-github .insteadOf", "https://github.com/"},
	}
	for _, repository := range []string{worktree, nested} {
		for _, setting := range hostile {
			run(repository, nil, "config", setting[0], setting[1])
		}
		for _, name := range events {
			run(repository, nil, "config", "--add", "hook.spy.event", name)
		}
	}
	write(cache, "info/attributes", "* filter=spy diff=spy merge=spy\n")
	write(nested, ".git/info/attributes", "* filter=spy diff=spy merge=spy\n")
	// Tracked files whose timestamps changed are read again, through a clean
	// filter when one is in effect.
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(worktree, "README.md"), later, later))
	require.NoError(t, os.Chtimes(filepath.Join(nested, "lib.txt"), later, later))
	require.NoError(t, os.RemoveAll(marker))

	wrapper := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(wrapper, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wrapper, "git"), []byte("#!/bin/sh\nfor arg do\n  shift\n  [ \"$arg\" = https://github.com/acme/app ] && arg="+origin+"\n  set -- \"$@\" \"$arg\"\ndone\nexec "+realGit+" \"$@\"\n"), 0o755))
	script := func(step, body, input string, args ...string) string {
		t.Helper()
		command := exec.Command("sh", append([]string{"-c", body, "kuayle-publish"}, args...)...)
		command.Env = append(os.Environ(), "PATH="+wrapper+":"+os.Getenv("PATH"), "HOME="+root)
		command.Stdin = strings.NewReader(input)
		output, err := command.CombinedOutput()
		ran, _ := os.ReadFile(marker)
		require.Empty(t, string(ran), "the "+step+" step ran a program named by the checkout")
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	// What the manager writes to the scripts that authenticate: a fixture, not a credential.
	const input = "fixture-publication-credential\n"
	commit := func(floor string) string {
		t.Helper()
		published := script("commit", publishCommitScript, "REC-1: Dark mode", worktree, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", floor)
		require.Regexp(t, publishCommitPattern, published)
		return published
	}
	push := func(published string) {
		t.Helper()
		script("push", publishPushScript, input, "acme/app", "kuayle/rec-1", worktree, published, "main", "agent@kuayle.invalid")
		require.Equal(t, published, run(origin, nil, "rev-parse", "refs/heads/kuayle/rec-1"))
	}
	sync := func() {
		t.Helper()
		script("sync", publishSyncScript, input, "acme/app", "kuayle/rec-1", worktree)
	}

	sync()
	first := commit("")
	push(first)
	require.Equal(t, "Kuayle Agent <agent@kuayle.invalid> Kuayle Agent <agent@kuayle.invalid>", run(origin, nil, "log", "-1", "--format=%an <%ae> %cn <%ce>", first))
	require.Equal(t, "REC-1: Dark mode", run(origin, nil, "log", "-1", "--format=%B", first))
	require.Equal(t, run(origin, nil, "rev-parse", "refs/heads/main"), run(origin, nil, "rev-parse", first+"^"), "the agent's commit is folded into one")
	require.Equal(t, ".gitattributes\nREADME.md\nfeature.txt\npending.txt\nvendor", run(origin, nil, "ls-tree", "-r", "--name-only", first))
	require.Equal(t, "pending", run(origin, nil, "cat-file", "blob", first+":pending.txt"), "content is committed as it is in the checkout")
	require.Equal(t, "160000 commit "+nestedHead+"\tvendor", run(origin, nil, "ls-tree", first, "vendor"), "the nested repository is recorded at its current commit")
	require.Equal(t, first, commit(first), "a retry republishes the same commit")

	// A maintainer pushes to the task branch; the sync brings it into the checkout.
	maintainer := filepath.Join(root, "maintainer")
	run(root, nil, "clone", "-q", "-b", "kuayle/rec-1", origin, maintainer)
	write(maintainer, "review.txt", "review\n")
	run(maintainer, nil, "add", "review.txt")
	run(maintainer, human, "commit", "-q", "-m", "Review")
	reviewed := run(maintainer, nil, "rev-parse", "HEAD")
	run(maintainer, nil, "push", "-q", origin, "kuayle/rec-1")
	sync()
	content, err := os.ReadFile(filepath.Join(worktree, "review.txt"))
	require.NoError(t, err, "the checkout fast-forwarded to the maintainer's commit")
	require.Equal(t, "review\n", string(content))
	require.Equal(t, "none", script("commit", publishCommitScript, "REC-2: Nothing", worktree, "kuayle/rec-1", "kuayle/rec-1", "Kuayle Agent", "agent@kuayle.invalid", ""),
		"nothing ahead of the fetched branch")

	write(worktree, "second.txt", "second\n")
	second := commit(first)
	push(second)
	require.Equal(t, reviewed, run(origin, nil, "rev-parse", second+"^"), "the maintainer's commit is kept")
	require.Equal(t, "second.txt", run(origin, nil, "show", "--name-only", "--format=", second))

	// The checkout is left on the published commit with nothing pending.
	require.NoError(t, os.WriteFile(filepath.Join(cache, "config"), cleanConfig, 0o644))
	require.NoError(t, os.Remove(filepath.Join(cache, "info", "attributes")))
	require.Equal(t, second, run(worktree, nil, "rev-parse", "HEAD"))
	require.Empty(t, run(worktree, nil, "-c", "core.fsmonitor=false", "status", "--porcelain", "--ignore-submodules=all"))
}
