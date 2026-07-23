package machine

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const removedCheckoutTokenPath = "/run/kuayle-secrets/github-token"

func TestCheckoutScriptCleansEphemeralCredentialsOnSuccess(t *testing.T) {
	result := runCheckoutScriptFixture(t, false)
	require.NoError(t, result.err, result.stderr)

	assertDirectoryEmpty(t, result.secretDir)
	assertDirectoryEmpty(t, result.tmpDir)
	assertNoSensitiveCheckoutData(t, result, removedCheckoutTokenPath)

	helperBytes, err := os.ReadFile(result.helperPath)
	require.NoError(t, err)
	helper := string(helperBytes)
	require.Contains(t, helper, "GITHUB_TOKEN")
	require.NotContains(t, helper, result.rawToken)
	require.NotContains(t, helper, removedCheckoutTokenPath)

	missingTokenCmd := exec.Command(result.helperPath, "get")
	missingTokenCmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	require.Error(t, missingTokenCmd.Run())
	processTokenCmd := exec.Command(result.helperPath, "get")
	processTokenCmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_TOKEN=process-env-token"}
	processTokenOutput, err := processTokenCmd.Output()
	require.NoError(t, err)
	require.Contains(t, string(processTokenOutput), "username=x-access-token")
	require.Contains(t, string(processTokenOutput), "password=process-env-token")

	workspaceConfig := filepath.Join(result.workspacePath, ".git", "config")
	configBytes, err := os.ReadFile(workspaceConfig)
	require.NoError(t, err)
	config := string(configBytes)
	require.Contains(t, config, "credential.helper="+result.helperPath)
	require.NotContains(t, config, result.rawToken)
	require.NotContains(t, config, removedCheckoutTokenPath)
}

func TestCheckoutScriptCleansEphemeralCredentialsOnFailure(t *testing.T) {
	result := runCheckoutScriptFixture(t, true)
	require.Error(t, result.err)
	require.Contains(t, result.stderr, "exit status 42")

	assertDirectoryEmpty(t, result.secretDir)
	assertDirectoryEmpty(t, result.tmpDir)
	assertNoSensitiveCheckoutData(t, result, removedCheckoutTokenPath)
}

func TestCredentialHelperScriptsDoNotReferencePersistentCheckoutToken(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	paths := []string{
		filepath.Join(repoRoot, "devmachine", "ide", "checkout.sh"),
		filepath.Join(repoRoot, "devmachine", "ide", "start.sh"),
		filepath.Join(repoRoot, "devmachine", "common", "kuayle-agent-runner"),
		filepath.Join(repoRoot, "BE", "internal", "machine", "docker.go"),
	}

	for _, path := range paths {
		contentBytes, err := os.ReadFile(path)
		require.NoError(t, err)
		content := string(contentBytes)
		require.NotContains(t, content, removedCheckoutTokenPath, path)
		require.NotContains(t, content, "cat /run/kuayle-secrets/github-token", path)
	}
}

func TestTerminalSessionCloseKillsOnlyTheNamedSessionAndIsIdempotent(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o700))
	statePath := filepath.Join(tempDir, "tmux-state")
	logPath := filepath.Join(tempDir, "tmux.log")
	require.NoError(t, os.WriteFile(statePath, []byte("term-test"), 0o600))
	writeFakeTmux(t, filepath.Join(fakeBin, "tmux"))
	script := filepath.Join(repoRoot, "devmachine", "ide", "terminal-session.sh")

	run := func() error {
		cmd := exec.Command("sh", script, "--close", "term-test")
		cmd.Env = []string{
			"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"KUAYLE_FAKE_TMUX_STATE=" + statePath,
			"KUAYLE_FAKE_TMUX_LOG=" + logPath,
		}
		return cmd.Run()
	}

	require.NoError(t, run())
	require.NoFileExists(t, statePath)
	require.NoError(t, run())
	logBytes, err := os.ReadFile(logPath)
	require.NoError(t, err)
	log := string(logBytes)
	require.Equal(t, 2, strings.Count(log, "has-session -t term-test"))
	require.Equal(t, 1, strings.Count(log, "kill-session -t term-test"))
}

func TestTerminalSessionCloseReportsPersistentTmuxFailure(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o700))
	statePath := filepath.Join(tempDir, "tmux-state")
	logPath := filepath.Join(tempDir, "tmux.log")
	require.NoError(t, os.WriteFile(statePath, []byte("term-test"), 0o600))
	writeFakeTmux(t, filepath.Join(fakeBin, "tmux"))
	cmd := exec.Command("sh", filepath.Join(repoRoot, "devmachine", "ide", "terminal-session.sh"), "--close", "term-test")
	cmd.Env = []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"KUAYLE_FAKE_TMUX_STATE=" + statePath,
		"KUAYLE_FAKE_TMUX_LOG=" + logPath,
		"KUAYLE_FAKE_TMUX_FAIL_KILL=1",
	}

	err := cmd.Run()

	require.Error(t, err)
	require.FileExists(t, statePath)
}

type checkoutScriptResult struct {
	rawToken      string
	workspaceRoot string
	workspacePath string
	secretDir     string
	tmpDir        string
	homeDir       string
	helperPath    string
	stdout        string
	stderr        string
	err           error
}

func runCheckoutScriptFixture(t *testing.T, failFetch bool) checkoutScriptResult {
	t.Helper()

	repoRoot := repoRootFromCaller(t)
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	workspacePath := filepath.Join(workspaceRoot, "tasks", "SEC-02")
	secretDir := filepath.Join(tempDir, "secrets")
	tmpDir := filepath.Join(tempDir, "tmp")
	homeDir := filepath.Join(tempDir, "home")
	fakeBin := filepath.Join(tempDir, "bin")
	helperPath := filepath.Join(homeDir, ".kuayle-git-credential")
	for _, dir := range []string{filepath.Join(workspaceRoot, "repos"), filepath.Join(workspaceRoot, "tasks"), secretDir, tmpDir, homeDir, fakeBin} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	staleBareRepository := filepath.Join(workspaceRoot, "repos", "octo-repo.git")
	require.NoError(t, os.MkdirAll(staleBareRepository, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(staleBareRepository, "config"), []byte("credential.helper=!f() { cat "+removedCheckoutTokenPath+"; }; f\ncore.askPass="+removedCheckoutTokenPath+"\n"), 0o600))

	fakeGit := filepath.Join(fakeBin, "git")
	writeFakeGit(t, fakeGit)

	rawToken := "checkout-raw-token-SEC02"
	cmd := exec.Command("sh", filepath.Join(repoRoot, "devmachine", "ide", "checkout.sh"), "octo/repo", "main", "kuayle-sec-02", workspacePath)
	cmd.Stdin = strings.NewReader(rawToken + "\n")
	cmd.Env = []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"KUAYLE_WORKSPACE_ROOT=" + workspaceRoot,
		"KUAYLE_CHECKOUT_SECRET_DIR=" + secretDir,
		"KUAYLE_CHECKOUT_TMP_DIR=" + tmpDir,
		"KUAYLE_CHECKOUT_ALLOW_ROOT_OVERRIDE=1",
		"KUAYLE_GIT_CREDENTIAL_HELPER=" + helperPath,
		"KUAYLE_FAKE_GIT_LOG=" + filepath.Join(tempDir, "git.log"),
		"KUAYLE_FAKE_GIT_SECRET_DIR=" + secretDir,
	}
	if failFetch {
		cmd.Env = append(cmd.Env, "KUAYLE_FAKE_GIT_FAIL_FETCH=1")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		stderr.WriteString(fmt.Sprintf("\n%v", err))
	}

	return checkoutScriptResult{
		rawToken:      rawToken,
		workspaceRoot: workspaceRoot,
		workspacePath: workspacePath,
		secretDir:     secretDir,
		tmpDir:        tmpDir,
		homeDir:       homeDir,
		helperPath:    helperPath,
		stdout:        stdout.String(),
		stderr:        stderr.String(),
		err:           err,
	}
}

func writeFakeGit(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
log=${KUAYLE_FAKE_GIT_LOG:?}
printf '%s\n' "$*" >> "$log"

if [ -n "${KUAYLE_CHECKOUT_TOKEN_FILE:-}" ]; then
  case "$KUAYLE_CHECKOUT_TOKEN_FILE" in "$KUAYLE_FAKE_GIT_SECRET_DIR"/*) ;; *) echo "token file outside secret dir" >&2; exit 90 ;; esac
  [ -f "$KUAYLE_CHECKOUT_TOKEN_FILE" ] || { echo "missing token file" >&2; exit 91; }
  [ -x "${GIT_ASKPASS:-}" ] || { echo "missing askpass" >&2; exit 92; }
  user=$("$GIT_ASKPASS" Username)
  [ "$user" = x-access-token ] || { echo "bad askpass username" >&2; exit 93; }
  password=$("$GIT_ASKPASS" Password)
  [ -n "$password" ] || { echo "empty askpass password" >&2; exit 94; }
fi

if [ "${KUAYLE_FAKE_GIT_FAIL_FETCH:-}" = "1" ]; then
  for arg in "$@"; do
    [ "$arg" != fetch ] || exit 42
  done
fi

work_tree=
git_dir=
if [ "${1:-}" = "-C" ]; then
  work_tree=$2
  shift 2
fi
case "${1:-}" in
  --git-dir=*) git_dir=${1#--git-dir=}; shift ;;
esac
command=${1:-}
[ "$#" -eq 0 ] || shift

config_target() {
  if [ -n "$work_tree" ]; then
    mkdir -p "$work_tree/.git"
    printf '%s\n' "$work_tree/.git/config"
  elif [ -n "$git_dir" ]; then
    mkdir -p "$git_dir"
    printf '%s\n' "$git_dir/config"
  else
    printf '%s\n' "$HOME/.gitconfig"
  fi
}

write_config() {
  target=$(config_target)
  mkdir -p "$(dirname "$target")"
  printf '%s=%s\n' "$1" "$2" >> "$target"
}

unset_config() {
  target=$(config_target)
  [ -f "$target" ] || return 0
  tmp="$target.tmp"
  : > "$tmp"
  while IFS= read -r line; do
    case "$line" in
      "$1"=*) ;;
      *) printf '%s\n' "$line" >> "$tmp" ;;
    esac
  done < "$target"
  mv "$tmp" "$target"
}

case "$command" in
  clone)
    destination=
    for arg in "$@"; do destination=$arg; done
    mkdir -p "$destination"
    ;;
  config)
    if [ "${1:-}" = "--global" ]; then
      shift
      if [ "${1:-}" = "--unset-all" ]; then
        key=${2:-}
        work_tree=
        git_dir=
        unset_config "$key"
        exit 0
      fi
      if [ "${1:-}" = "--add" ]; then shift; fi
      key=${1:-}
      value=${2:-}
      work_tree=
      git_dir=
      write_config "$key" "$value"
      exit 0
    fi
    if [ "${1:-}" = "--unset-all" ]; then
      key=${2:-}
      unset_config "$key"
      exit 0
    fi
    key=${1:-}
    value=${2:-}
    write_config "$key" "$value"
    ;;
  remote)
    mkdir -p "$git_dir"
    ;;
  fetch)
    mkdir -p "$git_dir"
    ;;
  worktree)
    subcommand=${1:-}
    shift || true
    if [ "$subcommand" = add ]; then
      checkout_path=
      while [ "$#" -gt 0 ]; do
        case "$1" in
          -b|-B) shift 2 ;;
          *) checkout_path=$1; break ;;
        esac
      done
      mkdir -p "$checkout_path/.git"
    fi
    ;;
  show-ref)
    case "$*" in
      *refs/remotes/origin/kuayle-sec-02*) exit 1 ;;
      *refs/remotes/origin/main*) exit 0 ;;
    esac
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
}

func writeFakeTmux(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
state=${KUAYLE_FAKE_TMUX_STATE:?}
log=${KUAYLE_FAKE_TMUX_LOG:?}
printf '%s\n' "$*" >> "$log"
case "${1:-}" in
  has-session )
    [ "${2:-}" = "-t" ]
    [ -f "$state" ] && [ "$(cat "$state")" = "${3:-}" ]
    ;;
  kill-session )
    [ "${2:-}" = "-t" ]
    [ -f "$state" ] && [ "$(cat "$state")" = "${3:-}" ]
    [ "${KUAYLE_FAKE_TMUX_FAIL_KILL:-}" != "1" ]
    rm -f "$state"
    ;;
  * ) exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
}

func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(filepath.Join(workingDirectory, "..", "..", ".."))
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func assertNoSensitiveCheckoutData(t *testing.T, result checkoutScriptResult, forbidden ...string) {
	t.Helper()
	for _, root := range []string{result.workspaceRoot, result.homeDir, result.secretDir, result.tmpDir} {
		assertNoForbiddenFileContent(t, root, append([]string{result.rawToken}, forbidden...)...)
	}
}

func assertNoForbiddenFileContent(t *testing.T, root string, forbidden ...string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			target, readLinkErr := os.Readlink(path)
			require.NoError(t, readLinkErr)
			for _, value := range forbidden {
				require.NotContains(t, target, value, path)
			}
			return nil
		}
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		for _, value := range forbidden {
			require.NotContains(t, string(content), value, path)
		}
		return nil
	}))
}
