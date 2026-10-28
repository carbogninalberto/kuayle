package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests run the developer start and checkout scripts against real git
// repositories served from local paths, so restart and multi-repository
// behavior is exercised with git's own rules rather than a fake.

type gitScriptEnv struct {
	t       *testing.T
	root    string
	home    string
	remotes string
	env     []string
}

func newGitScriptEnv(t *testing.T) *gitScriptEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	e := &gitScriptEnv{t: t, root: root, home: filepath.Join(root, "home"), remotes: filepath.Join(root, "remotes")}
	for _, dir := range []string{e.home, e.remotes} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	e.env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + e.home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Kuayle Test", "GIT_AUTHOR_EMAIL=test@example.test",
		"GIT_COMMITTER_NAME=Kuayle Test", "GIT_COMMITTER_EMAIL=test@example.test",
	}
	// GitHub URLs resolve to the local remotes, as the egress proxy would to GitHub.
	e.git("", "config", "--global", "url.file://"+e.remotes+"/.insteadOf", "https://github.com/")
	e.git("", "config", "--global", "init.defaultBranch", "main")
	return e
}

func (e *gitScriptEnv) git(dir string, args ...string) string {
	e.t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Env = e.env
	output, err := cmd.CombinedOutput()
	require.NoError(e.t, err, "git %s: %s", strings.Join(args, " "), output)
	return strings.TrimSpace(string(output))
}

// remote creates a GitHub-like repository owner/name with one commit on main.
func (e *gitScriptEnv) remote(fullName, file string) string {
	e.t.Helper()
	bare := filepath.Join(e.remotes, fullName)
	require.NoError(e.t, os.MkdirAll(filepath.Dir(bare), 0o700))
	e.git("", "init", "--bare", "--initial-branch=main", bare)
	seed := filepath.Join(e.root, "seed", fullName)
	e.git("", "clone", bare, seed)
	require.NoError(e.t, os.WriteFile(filepath.Join(seed, file), []byte(fullName+"\n"), 0o600))
	e.git(seed, "add", file)
	e.git(seed, "commit", "-m", "seed "+fullName)
	e.git(seed, "push", "origin", "HEAD:main")
	return seed
}

func (e *gitScriptEnv) runStart(workspace string, extra ...string) (string, error) {
	e.t.Helper()
	common := filepath.Join(repoRootFromCaller(e.t), "devmachine", "common", "start-common.sh")
	cmd := exec.Command("sh", "-eu", "-c", `. "$1"; prepare_developer_workspace; printf '%s\n' "$kuayle_default_workspace"`, "kuayle", common)
	cmd.Env = append(append([]string{}, e.env...), "KUAYLE_WORKSPACE_ROOT="+workspace, "KUAYLE_COLLECTOR_URL=http://127.0.0.1:9")
	cmd.Env = append(cmd.Env, extra...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return lines[len(lines)-1]
}

func (e *gitScriptEnv) runCheckout(workspace, repository, workspacePath string) (string, error) {
	e.t.Helper()
	secrets, tmp := filepath.Join(e.root, "secrets"), filepath.Join(e.root, "tmp")
	for _, dir := range []string{secrets, tmp} {
		require.NoError(e.t, os.MkdirAll(dir, 0o700))
	}
	script := filepath.Join(repoRootFromCaller(e.t), "devmachine", "ide", "checkout.sh")
	working := "kuayle/" + filepath.Base(workspacePath)
	cmd := exec.Command("sh", script, repository, "main", working, workspacePath)
	cmd.Stdin = strings.NewReader("installation-token-" + strings.ReplaceAll(repository, "/", "-") + "\n")
	cmd.Env = append(append([]string{}, e.env...),
		"KUAYLE_WORKSPACE_ROOT="+workspace,
		"KUAYLE_CHECKOUT_SECRET_DIR="+secrets,
		"KUAYLE_CHECKOUT_TMP_DIR="+tmp,
		"KUAYLE_CHECKOUT_ALLOW_ROOT_OVERRIDE=1",
		"KUAYLE_GIT_CREDENTIAL_HELPER="+filepath.Join(e.home, ".kuayle-git-credential"),
	)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// A repository machine's workspace volume already holds Kuayle's own state
// (the Codex package cache, task checkouts) when its developer container
// starts: on first start after link_codex_packages and on every restart. The
// root checkout must be created around that state and an already prepared
// workspace must be accepted as it is.
func TestStartCommonPreparesARepositoryWorkspaceThatAlreadyHoldsState(t *testing.T) {
	e := newGitScriptEnv(t)
	e.remote("octo/app", "README.md")
	workspace := filepath.Join(e.root, "workspace")
	for _, dir := range []string{".kuayle/codex-packages", "tasks/eng-1", "repos"} {
		require.NoError(t, os.MkdirAll(filepath.Join(workspace, dir), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "tasks", "eng-1", "notes.txt"), []byte("keep\n"), 0o600))
	repoEnv := []string{"KUAYLE_REPO_URL=https://github.com/octo/app", "KUAYLE_BASE_BRANCH=main", "KUAYLE_WORKING_BRANCH=kuayle/eng-1"}

	output, err := e.runStart(workspace, repoEnv...)
	require.NoError(t, err, output)
	require.FileExists(t, filepath.Join(workspace, "README.md"))
	require.DirExists(t, filepath.Join(workspace, ".git"))
	require.Equal(t, "kuayle/eng-1", e.git(workspace, "branch", "--show-current"))
	content, err := os.ReadFile(filepath.Join(workspace, "tasks", "eng-1", "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "keep\n", string(content), "existing workspace state is never replaced")
	require.NoDirExists(t, filepath.Join(workspace, ".kuayle", "root-checkout"))
	require.Equal(t, workspace, lastLine(output))

	// Local work on the root checkout survives a restart, even once the working
	// branch exists on the remote with other commits.
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "local.txt"), []byte("local\n"), 0o600))
	e.git(workspace, "add", "local.txt")
	e.git(workspace, "commit", "-m", "local work")
	localHead := e.git(workspace, "rev-parse", "HEAD")
	e.git(filepath.Join(e.root, "seed", "octo/app"), "push", "origin", "HEAD:refs/heads/kuayle/eng-1")

	output, err = e.runStart(workspace, repoEnv...)
	require.NoError(t, err, "restart of a prepared workspace: %s", output)
	require.Equal(t, localHead, e.git(workspace, "rev-parse", "HEAD"))
	require.Equal(t, "kuayle/eng-1", e.git(workspace, "branch", "--show-current"))
}

// A workspace whose earlier preparation was interrupted is completed on the
// next start instead of failing because the directory is not empty.
func TestStartCommonCompletesAnInterruptedRootCheckout(t *testing.T) {
	e := newGitScriptEnv(t)
	e.remote("octo/app", "README.md")
	workspace := filepath.Join(e.root, "workspace")
	staging := filepath.Join(workspace, ".kuayle", "root-checkout")
	require.NoError(t, os.MkdirAll(staging, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(staging, "partial"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "README.md"), []byte("octo/app\n"), 0o600))

	output, err := e.runStart(workspace, "KUAYLE_REPO_URL=https://github.com/octo/app", "KUAYLE_BASE_BRANCH=main")
	require.NoError(t, err, output)
	require.Equal(t, "main", e.git(workspace, "branch", "--show-current"))
	require.Empty(t, e.git(workspace, "status", "--porcelain", "--untracked-files=no"))
	require.NoDirExists(t, staging)
}

// A machine without a repository (task machines check out into worktrees)
// starts the same way whether its volume is new or already used.
func TestStartCommonRestartsAWorkspaceWithoutRootRepository(t *testing.T) {
	e := newGitScriptEnv(t)
	workspace := filepath.Join(e.root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, ".kuayle", "codex-packages"), 0o700))
	for range 2 {
		output, err := e.runStart(workspace)
		require.NoError(t, err, output)
		require.Equal(t, filepath.Join(workspace, "tasks"), lastLine(output))
	}
	require.NoDirExists(t, filepath.Join(workspace, ".git"))
}

// One machine serves several repositories: each is cloned once into its own
// cache under repos/<owner>/<repo>.git and every task gets its own worktree.
// Repository names that would collide when flattened stay separate.
func TestCheckoutServesSeveralRepositoriesFromOneWorkspace(t *testing.T) {
	e := newGitScriptEnv(t)
	e.remote("octo/app", "app.txt")
	e.remote("octo/api", "api.txt")
	e.remote("octo-a/b", "flat-one.txt")
	e.remote("octo/a-b", "flat-two.txt")
	workspace := filepath.Join(e.root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "tasks"), 0o700))

	checkouts := []struct{ repository, path, file string }{
		{"octo/app", "eng-1", "app.txt"},
		{"octo/api", "eng-2", "api.txt"},
		{"octo/app", "eng-3", "app.txt"},
		{"octo-a/b", "eng-4", "flat-one.txt"},
		{"octo/a-b", "eng-5", "flat-two.txt"},
	}
	for _, checkout := range checkouts {
		path := filepath.Join(workspace, "tasks", checkout.path)
		output, err := e.runCheckout(workspace, checkout.repository, path)
		require.NoError(t, err, "%s: %s", checkout.repository, output)
		require.FileExists(t, filepath.Join(path, checkout.file), checkout.repository)
		require.Equal(t, "kuayle/"+checkout.path, e.git(path, "branch", "--show-current"))
		require.Equal(t, filepath.Join(workspace, "repos", checkout.repository+".git"), e.git(path, "rev-parse", "--path-format=absolute", "--git-common-dir"))
		require.Equal(t, "https://github.com/"+checkout.repository, e.git(path, "config", "--get", "remote.origin.url"))
	}
	require.NoFileExists(t, filepath.Join(workspace, "tasks", "eng-4", "flat-two.txt"))
	worktrees := e.git("", "--git-dir="+filepath.Join(workspace, "repos", "octo", "app.git"), "worktree", "list")
	require.Contains(t, worktrees, filepath.Join(workspace, "tasks", "eng-1"))
	require.Contains(t, worktrees, filepath.Join(workspace, "tasks", "eng-3"))
	require.NotContains(t, worktrees, filepath.Join(workspace, "tasks", "eng-2"))

	// Preparing an existing checkout again keeps its local work.
	path := filepath.Join(workspace, "tasks", "eng-1")
	require.NoError(t, os.WriteFile(filepath.Join(path, "work.txt"), []byte("work\n"), 0o600))
	output, err := e.runCheckout(workspace, "octo/app", path)
	require.NoError(t, err, output)
	require.FileExists(t, filepath.Join(path, "work.txt"))

	// A path that holds another repository's checkout is never reused for this one.
	output, err = e.runCheckout(workspace, "octo/api", path)
	require.Error(t, err, output)
	require.Contains(t, output, "another repository")

	// The bare caches keep no credentials; the start script configures the
	// token-less helper on every cache and checkout.
	for _, bare := range []string{"octo/app.git", "octo/api.git", "octo-a/b.git", "octo/a-b.git"} {
		config, err := os.ReadFile(filepath.Join(workspace, "repos", bare, "config"))
		require.NoError(t, err)
		require.NotContains(t, string(config), "installation-token")
	}
	output, err = e.runStart(workspace)
	require.NoError(t, err, output)
	helper := filepath.Join(e.home, ".kuayle-git-credential")
	require.Equal(t, helper, e.git("", "--git-dir="+filepath.Join(workspace, "repos", "octo", "api.git"), "config", "credential.helper"))
}
