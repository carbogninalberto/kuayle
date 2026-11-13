package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	fixtureIdentity = []string{"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid"}
	kuayleIdentity  = []string{"GIT_AUTHOR_NAME=Kuayle Agent", "GIT_AUTHOR_EMAIL=agent@kuayle.invalid", "GIT_COMMITTER_NAME=Kuayle Agent", "GIT_COMMITTER_EMAIL=agent@kuayle.invalid"}
)

// The sync script runs against real git with only the network replaced, like the
// publication scripts: a wrapper maps the fixed GitHub URL to a local bare
// origin repository.
type scriptFixture struct {
	t        *testing.T
	realGit  string
	root     string
	origin   string
	checkout string
	wrapper  string
}

func newScriptFixture(t *testing.T) *scriptFixture {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	f := &scriptFixture{t: t, realGit: realGit, root: t.TempDir()}
	f.origin = filepath.Join(f.root, "origin.git")
	f.checkout = filepath.Join(f.root, "checkout")
	seed := filepath.Join(f.root, "seed")
	f.run(f.root, nil, "init", "-q", "-b", "main", seed)
	f.commitFile(seed, "README.md", fixtureIdentity)
	f.run(f.root, nil, "clone", "-q", "--bare", seed, f.origin)
	f.run(f.root, nil, "clone", "-q", f.origin, f.checkout)
	f.run(f.checkout, nil, "checkout", "-q", "-b", "kuayle/rec-1")
	f.wrapper = filepath.Join(f.root, "bin")
	require.NoError(t, os.MkdirAll(f.wrapper, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.wrapper, "git"), []byte("#!/bin/sh\nfor arg do\n  shift\n  [ \"$arg\" = https://github.com/acme/app ] && arg="+f.origin+"\n  set -- \"$@\" \"$arg\"\ndone\nexec "+realGit+" \"$@\"\n"), 0o755))
	return f
}

func (f *scriptFixture) run(dir string, env []string, args ...string) string {
	f.t.Helper()
	command := exec.Command(f.realGit, append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	command.Dir = dir
	if env == nil {
		// Git setup steps such as a maintainer's merge need an identity; CI
		// runners have no global one.
		env = fixtureIdentity
	}
	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()
	require.NoError(f.t, err, string(output))
	return strings.TrimSpace(string(output))
}

func (f *scriptFixture) commitFile(dir, name string, env []string) string {
	f.t.Helper()
	require.NoError(f.t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644))
	f.run(dir, nil, "add", name)
	f.run(dir, env, "commit", "-q", "-m", name)
	return f.run(dir, nil, "rev-parse", "HEAD")
}

// script runs one publication script with input on stdin, as the manager does.
func (f *scriptFixture) script(body, input, name string, args ...string) (string, error) {
	command := exec.Command("sh", append([]string{"-c", body, name}, args...)...)
	command.Env = append(os.Environ(), "PATH="+f.wrapper+":"+os.Getenv("PATH"), "HOME="+f.root)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (f *scriptFixture) sync(published string) (string, error) {
	return f.script(publishSyncScript, "fixture-credential\n", "sync", "acme/app", "kuayle/rec-1", f.checkout, published)
}

// commit runs the commit step with a commit message, as the manager does.
func (f *scriptFixture) commit(floor string) (string, error) {
	return f.script(publishCommitScript, "REC-1: Dark mode\n", "commit", f.checkout, "kuayle/rec-1", "main", "Kuayle Agent", "agent@kuayle.invalid", floor)
}

func (f *scriptFixture) push(commit, lease string) (string, error) {
	return f.script(publishPushScript, "fixture-credential\n", "push", "acme/app", "kuayle/rec-1", f.checkout, commit, "main", "agent@kuayle.invalid", lease)
}

// freshCheckout replaces the checkout with a clone that only knows the base
// branch, as when the agent rebuilt its branch from scratch: the commits only
// GitHub's task branch has, the published one included, are no longer in it.
func (f *scriptFixture) freshCheckout() {
	f.t.Helper()
	require.NoError(f.t, os.RemoveAll(f.checkout))
	f.run(f.root, nil, "clone", "-q", "--single-branch", "--branch", "main", f.origin, f.checkout)
	f.run(f.checkout, nil, "checkout", "-q", "-b", "kuayle/rec-1")
}

func (f *scriptFixture) head() string { return f.run(f.checkout, nil, "rev-parse", "HEAD") }

func (f *scriptFixture) originTip() string {
	return f.run(f.origin, nil, "rev-parse", "refs/heads/kuayle/rec-1")
}

// fails runs git and reports whether it exited non-zero, without failing the test.
func (f *scriptFixture) fails(dir string, args ...string) bool {
	f.t.Helper()
	command := exec.Command(f.realGit, args...)
	command.Dir = dir
	command.Env = os.Environ()
	return command.Run() != nil
}

// The sync step's stdout is the manager's only input: a fast-forward prints
// nothing, an accepted rewrite prints the lease, and anything else is a failure
// of the script contract rather than a lease.
func TestParseSyncLeaseOnlyAcceptsAFastForwardOrALease(t *testing.T) {
	sha1, sha256 := strings.Repeat("a", 40), strings.Repeat("b", 64)
	for _, test := range []struct {
		output string
		lease  string
		valid  bool
	}{
		{"", "", true},
		{" \n", "", true},
		{"lease " + sha1 + "\n", sha1, true},
		{"lease " + sha256, sha256, true},
		{"lease", "", false},
		{"lease not-a-commit", "", false},
		{"lease " + sha1 + " extra", "", false},
		{"fast-forward", "", false},
	} {
		lease, err := parseSyncLease(test.output)
		require.Equal(t, test.valid, err == nil, test.output)
		require.Equal(t, test.lease, lease, test.output)
	}
}

func TestSyncScriptFastForwardsToCommitsOthersPushedAndNeverRewrites(t *testing.T) {
	f := newScriptFixture(t)
	published := f.commitFile(f.checkout, "feature.txt", fixtureIdentity)
	f.run(f.checkout, nil, "push", "-q", f.origin, "kuayle/rec-1")

	output, err := f.sync(published)
	require.NoError(t, err, output)
	require.Empty(t, output, "a fast-forward has nothing to lease")
	require.Equal(t, published, f.head(), "nothing to bring in")

	// A smudge filter configured in the checkout would run during the
	// fast-forward and look for the credential. The checkout's configuration is
	// never loaded, so it does not run at all.
	marker := filepath.Join(f.root, "filter-ran")
	filter := filepath.Join(f.root, "filter.sh")
	require.NoError(t, os.WriteFile(filter, []byte("#!/bin/sh\necho ran >> "+marker+"\nfor f in /tmp/kuayle-sync.*/*; do echo \"$f\" >> "+marker+"; cat \"$f\" >> "+marker+" 2>/dev/null; done\ncat\n"), 0o755))
	f.run(f.checkout, nil, "config", "filter.spy.smudge", filter)
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, ".git", "info", "attributes"), []byte("* filter=spy\n"), 0o644))

	// A maintainer merges main into the pull request branch on GitHub.
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", "-b", "kuayle/rec-1", f.origin, maintainer)
	f.run(maintainer, nil, "checkout", "-q", "main")
	f.commitFile(maintainer, "main-only.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "main")
	f.run(maintainer, nil, "checkout", "-q", "kuayle/rec-1")
	f.run(maintainer, nil, "merge", "-q", "--no-ff", "-m", "Merge branch 'main'", "main")
	merged := f.run(maintainer, nil, "rev-parse", "HEAD")
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")

	output, err = f.sync(published)
	require.NoError(t, err, output)
	require.Empty(t, output, "a fast-forward has nothing to lease")
	require.Equal(t, merged, f.head(), "the checkout fast-forwarded to the new head")
	spied, _ := os.ReadFile(marker)
	require.Empty(t, string(spied), "the checkout's filter ran during the fast-forward")

	// Local commits GitHub does not have, on top of what it has, are kept.
	local := f.commitFile(f.checkout, "local.txt", fixtureIdentity)
	output, err = f.sync(published)
	require.NoError(t, err, output)
	require.Empty(t, output)
	require.Equal(t, local, f.head())

	// Diverged histories are reported, and neither side is touched. Kuayle's
	// record is the commit it published earlier, not the maintainer's merge.
	f.run(maintainer, nil, "checkout", "-q", "kuayle/rec-1")
	f.commitFile(maintainer, "other.txt", fixtureIdentity)
	pushed := f.run(maintainer, nil, "rev-parse", "HEAD")
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")
	output, err = f.sync(published)
	require.Error(t, err)
	require.Contains(t, output, "someone else pushed to the branch")
	require.Contains(t, output, "cannot be fast-forwarded")
	require.NotContains(t, output, "lease ")
	require.Equal(t, local, f.head())
	require.Equal(t, pushed, f.originTip())
}

// An agent that rebased its own branch rewrote the commits Kuayle published.
// GitHub's tip is still exactly Kuayle's last published commit, so nobody else
// pushed: the sync reports that commit as the lease for a guarded force push and
// leaves the checkout for the commit step.
func TestSyncScriptLeasesThePublishedCommitWhenTheAgentRewroteItsOwnHistory(t *testing.T) {
	f := newScriptFixture(t)
	published := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	f.run(f.checkout, nil, "push", "-q", f.origin, "kuayle/rec-1")

	// The base branch moves on.
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", f.origin, maintainer)
	f.commitFile(maintainer, "main-only.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "main")

	// The agent rebases its own branch onto the new base. The rebase keeps the
	// published author and only replaces the commits below Kuayle's record.
	f.run(f.checkout, nil, "fetch", "-q", "origin")
	f.run(f.checkout, kuayleIdentity, "rebase", "-q", "refs/remotes/origin/main")
	rewritten := f.head()
	require.NotEqual(t, published, rewritten)
	require.True(t, f.fails(f.checkout, "merge-base", "--is-ancestor", published, rewritten), "the published commit is no longer in the checkout's history")

	output, err := f.sync(published)
	require.NoError(t, err, output)
	require.Equal(t, "lease "+published, output)
	require.Equal(t, rewritten, f.head(), "the lease leaves the checkout alone")
	require.Equal(t, published, f.originTip(), "the sync pushes nothing")

	// A rewrite the agent made under its own identity is still another author's
	// commit, even though it replaces Kuayle's published commit.
	f.run(f.checkout, fixtureIdentity, "commit", "-q", "--amend", "-m", "agent rewrite")
	output, err = f.push(f.head(), published)
	require.Error(t, err)
	require.Contains(t, output, "another author")
	require.Equal(t, published, f.originTip(), "the author check refused the push")

	// The rewritten history is not a fast-forward, so only the lease allows it.
	f.run(f.checkout, kuayleIdentity, "commit", "-q", "--amend", "-m", "rewritten")
	rewritten = f.head()
	output, err = f.push(rewritten, "")
	require.Error(t, err, "a rewrite needs the lease")
	require.Contains(t, output, "non-fast-forward")
	require.Equal(t, published, f.originTip())

	output, err = f.push(rewritten, published)
	require.NoError(t, err, output)
	require.Equal(t, rewritten, f.originTip())
	require.NotContains(t, f.run(f.origin, nil, "rev-list", rewritten), published, "GitHub now holds the rewritten commit")
}

// Someone else pushed to the branch after Kuayle's last publish. Even though the
// checkout also diverged, that is not the agent's own rewrite, so the sync
// refuses instead of leasing the push.
func TestSyncScriptRefusesDivergenceWhenSomeoneElsePushedAfterKuayle(t *testing.T) {
	f := newScriptFixture(t)
	published := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	f.run(f.checkout, nil, "push", "-q", f.origin, "kuayle/rec-1")

	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", "-b", "kuayle/rec-1", f.origin, maintainer)
	other := f.commitFile(maintainer, "other.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")

	// The agent also rewrote its own published commit in the checkout.
	f.run(f.checkout, fixtureIdentity, "commit", "-q", "--amend", "-m", "rewritten")
	rewritten := f.head()

	output, err := f.sync(published)
	require.Error(t, err)
	require.Contains(t, output, "someone else pushed to the branch")
	require.Contains(t, output, "nothing was force-pushed")
	require.NotContains(t, output, "lease ")
	require.Equal(t, rewritten, f.head())
	require.Equal(t, other, f.originTip(), "nothing was pushed")
}

// The lease is what protects a rewrite: the pushed commit here is the amended
// one, which does not descend from GitHub's tip, so the push is a real force
// rather than a fast-forward. When the branch moves between the sync and the
// push, git's stale-info check refuses the force and GitHub keeps the newer
// commit.
func TestPushScriptRefusesAStaleLeaseWhenTheRemoteMovedAfterSync(t *testing.T) {
	f := newScriptFixture(t)
	published := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	f.run(f.checkout, nil, "push", "-q", f.origin, "kuayle/rec-1")

	// The agent rewrote the published commit under Kuayle's identity, so the
	// sync accepts the rewrite and returns the lease.
	f.run(f.checkout, kuayleIdentity, "commit", "-q", "--amend", "-m", "rewritten")
	rewritten := f.head()
	require.True(t, f.fails(f.checkout, "merge-base", "--is-ancestor", published, rewritten), "the rewrite is not a fast-forward")
	lease, err := f.sync(published)
	require.NoError(t, err, lease)
	require.Equal(t, "lease "+published, lease)

	// Someone else pushes after the sync and before the push.
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", "-b", "kuayle/rec-1", f.origin, maintainer)
	other := f.commitFile(maintainer, "other.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")

	output, err := f.push(rewritten, published)
	require.Error(t, err)
	require.Contains(t, output, "stale info")
	require.Equal(t, other, f.originTip(), "the lease refused the push and GitHub keeps the newer commit")
}

// A maintainer's commit on the task branch must survive a publication that no
// longer has the published commit. The lease lets the push replace GitHub's tip,
// but only while every commit it drops is Kuayle's own.
func TestPushScriptRefusesToDropCommitsSomeoneElsePushed(t *testing.T) {
	f := newScriptFixture(t)

	// A maintainer pushed M to the task branch, and Kuayle published P on top.
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", f.origin, maintainer)
	f.run(maintainer, nil, "checkout", "-q", "-b", "kuayle/rec-1")
	maintainerCommit := f.commitFile(maintainer, "review.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")
	f.run(f.checkout, nil, "fetch", "-q", "origin")
	f.run(f.checkout, nil, "reset", "-q", "--hard", "refs/remotes/origin/kuayle/rec-1")
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "feature.txt"), []byte("feature\n"), 0o644))
	published, err := f.commit("")
	require.NoError(t, err, published)
	output, err := f.push(published, "")
	require.NoError(t, err, output)
	require.Equal(t, published, f.originTip())

	// The checkout is rebuilt from the base branch without the published commit
	// or the maintainer's commit below it, as the merge-base fallback leaves it.
	// The push gets that commit, which does not descend from the lease.
	f.freshCheckout()
	rebuiltCommit := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	lease, err := f.sync(published)
	require.NoError(t, err, lease)
	require.Equal(t, "lease "+published, lease)
	require.True(t, f.fails(f.checkout, "merge-base", "--is-ancestor", published, rebuiltCommit), "the push would not descend from the lease")

	output, err = f.push(rebuiltCommit, published)
	require.Error(t, err)
	require.Contains(t, output, "the push would drop commits someone else pushed to the task branch; nothing was force-pushed")
	require.Equal(t, published, f.originTip(), "GitHub still ends at the published commit")
	require.Contains(t, f.run(f.origin, nil, "rev-list", published), maintainerCommit, "the maintainer's commit is still on the branch")
}

// Base-branch commits on the task branch are never counted as dropped: a
// published commit built on a newer base, replaced by a rebuild from an older
// base, only drops Kuayle's own commit.
func TestPushScriptDoesNotCountBaseCommitsAsDropped(t *testing.T) {
	f := newScriptFixture(t)
	oldBase := strings.TrimSpace(f.run(f.checkout, nil, "rev-parse", "HEAD"))

	// Someone else's commit lands on the base branch, and Kuayle publishes on it.
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", f.origin, maintainer)
	baseCommit := f.commitFile(maintainer, "base.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "HEAD:main")
	f.run(f.checkout, nil, "fetch", "-q", "origin")
	f.run(f.checkout, nil, "reset", "-q", "--hard", "refs/remotes/origin/main")
	published := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	output, err := f.push(published, "")
	require.NoError(t, err, output)

	// The checkout is rebuilt from the older base, without either commit.
	f.run(f.checkout, nil, "reset", "-q", "--hard", oldBase)
	rebuiltCommit := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	require.True(t, f.fails(f.checkout, "merge-base", "--is-ancestor", baseCommit, rebuiltCommit), "the rebuild lacks the base commit")

	output, err = f.push(rebuiltCommit, published)
	require.NoError(t, err, output)
	require.Equal(t, rebuiltCommit, f.originTip())
}

// The other side of the same guard: a force push that only drops commits Kuayle
// published itself replaces them with the rebuilt commit.
func TestPushScriptPublishesAForceThatOnlyDropsKuayleCommits(t *testing.T) {
	f := newScriptFixture(t)
	published := f.commitFile(f.checkout, "feature.txt", kuayleIdentity)
	output, err := f.push(published, "")
	require.NoError(t, err, output)
	require.Equal(t, published, f.originTip())

	// The checkout is rebuilt from the base branch without the published commit,
	// as the merge-base fallback leaves it.
	f.freshCheckout()
	// A different file, so the rebuild never has the published commit's ID.
	rebuiltCommit := f.commitFile(f.checkout, "rebuilt.txt", kuayleIdentity)
	lease, err := f.sync(published)
	require.NoError(t, err, lease)
	require.Equal(t, "lease "+published, lease)
	require.True(t, f.fails(f.checkout, "merge-base", "--is-ancestor", published, rebuiltCommit), "the push would drop the published commit")
	// Someone else's commit lands on the base branch meanwhile; it is not on
	// the task branch, so it is not among the commits the force drops.
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", f.origin, maintainer)
	f.commitFile(maintainer, "base.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "HEAD:main")

	output, err = f.push(rebuiltCommit, published)
	require.NoError(t, err, output)
	require.Equal(t, rebuiltCommit, f.originTip())
	require.NotContains(t, f.run(f.origin, nil, "rev-list", rebuiltCommit), published, "the push dropped Kuayle's own published commit")
}
