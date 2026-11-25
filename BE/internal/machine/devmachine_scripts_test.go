package machine

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const removedCheckoutTokenPath = "/run/kuayle-secrets/github-token"

// A checkout leaves its token nowhere: not in the cache, the worktree or the
// home, whether its fetch succeeds or fails. People's IDE and terminals get
// a helper that reads GITHUB_TOKEN from their own environment for GitHub
// (TestCredentialHelpersGiveTheTokenOnlyToGitHub), and a stale
// cache configuration that named the removed token file is replaced.
func TestCheckoutLeavesNoCredentialBehind(t *testing.T) {
	e := newGitScriptEnv(t)
	e.remote("octo/repo", "README.md")
	workspace := filepath.Join(e.root, "workspace")
	stale := filepath.Join(workspace, "repos", "octo", "repo.git")
	e.git("", "init", "-q", "--bare", stale)
	e.git("", "--git-dir="+stale, "config", "credential.helper", "!f() { cat "+removedCheckoutTokenPath+"; }; f")
	e.git("", "--git-dir="+stale, "config", "core.askPass", removedCheckoutTokenPath)
	path := filepath.Join(workspace, "tasks", "sec-02")

	output, err := e.runCheckout(workspace, "octo/repo", path)
	require.NoError(t, err, output)
	require.FileExists(t, filepath.Join(path, "README.md"))
	for _, root := range []string{workspace, e.home} {
		assertNoForbiddenFileContent(t, root, "installation-token-", removedCheckoutTokenPath)
	}
	helperPath := filepath.Join(e.home, ".kuayle-git-credential")
	require.Equal(t, helperPath, e.git("", "--git-dir="+stale, "config", "--get-all", "credential.helper"))
	helperBytes, err := os.ReadFile(helperPath)
	require.NoError(t, err)
	require.Contains(t, string(helperBytes), "GITHUB_TOKEN")
	github := "protocol=https\nhost=github.com\n\n"
	missingTokenCmd := exec.Command(helperPath, "get")
	missingTokenCmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	missingTokenCmd.Stdin = strings.NewReader(github)
	require.Error(t, missingTokenCmd.Run())
	processTokenCmd := exec.Command(helperPath, "get")
	processTokenCmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_TOKEN=process-env-token"}
	processTokenCmd.Stdin = strings.NewReader(github)
	processTokenOutput, err := processTokenCmd.Output()
	require.NoError(t, err)
	require.Contains(t, string(processTokenOutput), "username=x-access-token")
	require.Contains(t, string(processTokenOutput), "password=process-env-token")

	// A fetch that fails leaves nothing behind either.
	output, err = e.runCheckout(workspace, "octo/missing", filepath.Join(workspace, "tasks", "sec-03"))
	require.Error(t, err, output)
	for _, root := range []string{workspace, e.home} {
		assertNoForbiddenFileContent(t, root, "installation-token-")
	}
}

func TestCredentialHelperScriptsDoNotReferencePersistentCheckoutToken(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	paths := []string{
		filepath.Join(repoRoot, "BE", "internal", "machine", "docker_checkout.go"),
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

func TestTerminalRuntimeConfiguresFullColorTUI(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	tmuxConfigBytes, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "ide", "tmux.conf"))
	require.NoError(t, err)
	tmuxConfig := string(tmuxConfigBytes)
	expectedTmuxOptions := []string{
		`set -g default-terminal "tmux-256color"`,
		`set -as terminal-features ",xterm-256color:RGB"`,
		// xterm.js 6 supports synchronized output, so tmux redraws arrive as one frame
		// instead of tearing; it also renders styled underlines and OSC 8 hyperlinks.
		`set -as terminal-features ",xterm-256color:sync:usstyle:hyperlinks:strikethrough:overline"`,
		`set -g focus-events on`,
		`set -g extended-keys on`,
		`set -g allow-passthrough on`,
		`set -g history-limit 50000`,
		`set-option -g destroy-unattached on`,
	}
	for _, option := range expectedTmuxOptions {
		require.Contains(t, tmuxConfig, option+"\n")
	}
	// Browser terminals receive escape sequences over a WebSocket, so a sequence can arrive
	// split across frames. A short escape-time makes tmux treat the ESC as a keypress and
	// type the rest (for example "[<35;59;28M") into the TUI; keep tmux's default. Mouse
	// mode would also make xterm.js stream motion reports and take over text selection.
	require.NotContains(t, tmuxConfig, "escape-time")
	require.NotContains(t, tmuxConfig, "mouse on")

	sessionScriptBytes, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "ide", "terminal-session.sh"))
	require.NoError(t, err)
	require.Contains(t, string(sessionScriptBytes), "export COLORTERM=truecolor\n")
	require.Contains(t, string(sessionScriptBytes), "export TERM=\"${TERM:-xterm-256color}\"\n")

	dockerfileBytes, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "ide", "Dockerfile"))
	require.NoError(t, err)
	require.Contains(t, string(dockerfileBytes), "ncurses-term")
}

func TestAgentCLIInstallerRejectsUnknownArchitecture(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	installer := filepath.Join(repoRoot, "devmachine", "common", "install-agent-clis.sh")
	cmd := exec.Command("sh", installer, "riscv64")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	require.Error(t, err)
	require.Contains(t, stderr.String(), "unsupported architecture: riscv64")
}

// Crush release binaries are built with a Go toolchain that has known stdlib CVEs,
// so both developer images build it from source with a patched toolchain.
// Without a UTF-8 locale tmux replaces every non-ASCII glyph (prompt chevrons, box and
// block characters) with "_", which breaks Codex, OpenCode and other TUIs.
func TestDeveloperTerminalsUseUTF8(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	for _, image := range []string{"ide", "headless"} {
		dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", image, "Dockerfile"))
		require.NoError(t, err)
		require.Regexp(t, `(?m)^ENV HOME=/home/kuayle \\\n\s+LANG=C\.UTF-8`, string(dockerfile), image)
	}
	session, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "ide", "terminal-session.sh"))
	require.NoError(t, err)
	require.Contains(t, string(session), "exec tmux -u new-session")
}

func TestDeveloperImagesBuildCrushWithPatchedGo(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	for _, image := range []string{"ide", "headless"} {
		dockerfileBytes, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", image, "Dockerfile"))
		require.NoError(t, err)
		dockerfile := string(dockerfileBytes)
		require.Contains(t, dockerfile, "FROM golang:1.27.4-bookworm AS crush-build", image)
		// Release bumps update the pin, so the test checks that one exists.
		require.Regexp(t, `(?m)^ARG CRUSH_VERSION=[0-9]+\.[0-9]+\.[0-9]+$`, dockerfile, image)
		require.Contains(t, dockerfile, "github.com/charmbracelet/crush@v${CRUSH_VERSION}", image)
		// A wrapper module raises the release's golang.org/x/net past CVE-2026-78661.
		require.Contains(t, dockerfile, "ARG CRUSH_XNET_VERSION=v0.62.0\n", image)
		require.Contains(t, dockerfile, "COPY --from=crush-build /out/crush /usr/local/bin/crush", image)
	}
	headless, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "headless", "Dockerfile"))
	require.NoError(t, err)
	// build-essential pulls linux-libc-dev, whose kernel CVEs fail the image scan.
	require.NotContains(t, string(headless), "build-essential")
}

// The images run check-agent-cli-flags.sh after installing the CLIs: it
// passes when a CLI's help lists what Kuayle passes it, fails naming what is
// missing (a longer flag that starts the same does not count), and removes
// whatever the help command wrote.
func TestAgentCLIFlagCheckNamesWhatAHelpLacks(t *testing.T) {
	script := filepath.Join(repoRootFromCaller(t), "devmachine", "common", "check-agent-cli-flags.sh")
	fakeBin, tmp := t.TempDir(), t.TempDir()
	crushHelp := func(help string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "crush"),
			[]byte("#!/bin/sh\n[ \"$*\" = 'run --help' ] || exit 2\ntouch \"$HOME/settings\"\nprintf '%s\\n' '"+help+"'\n"), 0o700))
	}
	check := func() (string, error) {
		t.Helper()
		cmd := exec.Command("sh", script, "crush")
		cmd.Env = []string{"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR=" + tmp}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stderr.String(), err
	}

	crushHelp("  -y --yolo    Skip permission prompts\n  -q --quiet   Hide spinner")
	stderr, err := check()
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)

	crushHelp("  -y --yolo    Skip permission prompts\n  --quiet-mode Hide spinner")
	stderr, err = check()
	require.Error(t, err)
	require.Equal(t, "crush run --help: missing --quiet\n", stderr)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Empty(t, entries, "the help command's files are removed")
}

func TestAgentCLIInstallerRejectsDigestMismatchBeforeExtraction(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o700))
	npmRoot := filepath.Join(tempDir, "npm-root")
	require.NoError(t, os.MkdirAll(filepath.Join(npmRoot, "@qwen-code", "qwen-code"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "npm"), []byte("#!/bin/sh\nif [ \"$1\" = root ]; then echo '"+npmRoot+"'; fi\nexit 0\n"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(`#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    output=$1
  fi
  shift
done
printf 'tampered release' > "$output"
`), 0o700))

	cmd := exec.Command("sh", filepath.Join(repoRoot, "devmachine", "common", "install-agent-clis.sh"), "amd64")
	cmd.Env = []string{"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	require.Error(t, err)
	require.Contains(t, stderr.String(), "sha256 mismatch for goose")
	// Release bumps rewrite the pinned digest, so read it from the script.
	script, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "install-agent-clis.sh"))
	require.NoError(t, err)
	pinned := regexp.MustCompile(`(?m)^\s*amd64\)[\s\S]*?goose_sha256=([0-9a-f]{64})`).FindSubmatch(script)
	require.NotNil(t, pinned, "amd64 goose digest pin")
	require.Contains(t, stderr.String(), "expected "+string(pinned[1]))
}

func TestAgentCLIInstallerPinsEveryCLIAndReleaseDigest(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	installerBytes, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "install-agent-clis.sh"))
	require.NoError(t, err)
	installer := string(installerBytes)

	// Release publishing refreshes exact values; require concrete pins rather than
	// freezing this test to the versions shipped when it was written.
	for _, name := range []string{"CLAUDE_CODE", "OPENCODE", "CODEX", "PI", "QWEN", "GOOSE", "KIMI"} {
		require.Regexp(t, `(?m)^`+name+`_VERSION=\d+\.\d+\.\d+$`, installer)
	}
	// Pi's advisories (CVE-2026-54325 to -54328) are fixed from 0.79.0 and only
	// under the @earendil-works scope; the @mariozechner package has no fix.
	require.Contains(t, installer, `"@earendil-works/pi-coding-agent@${PI_VERSION}"`)
	require.NotContains(t, installer, "@mariozechner")
	pi := regexp.MustCompile(`(?m)^PI_VERSION=(\d+)\.(\d+)\.\d+$`).FindStringSubmatch(installer)
	require.NotNil(t, pi)
	major, err := strconv.Atoi(pi[1])
	require.NoError(t, err)
	minor, err := strconv.Atoi(pi[2])
	require.NoError(t, err)
	require.True(t, major > 0 || minor >= 79, "Pi is pinned at 0.79.0 or later")
	for _, arch := range []string{"amd64", "arm64"} {
		require.Regexp(t, `(?s)`+arch+`\).*?goose_sha256=[a-f0-9]{64}\n.*?kimi_sha256=[a-f0-9]{64}\n`, installer)
	}
	for _, pin := range []string{

		"--allow-scripts=@anthropic-ai/claude-code",

		"--allow-scripts=@opencode/cli",

		"@qwen-code/qwen-code",

		// qwen-code 0.26.1 bundles sharp 0.35.4 (GHSA-wq5f-xc86-pv6w).
		"sharp@0.35.5",
		"goose-${goose_arch}-unknown-linux-gnu.tar.gz",

		"kimi-${KIMI_VERSION}-${kimi_arch}-unknown-linux-gnu.tar.gz",
	} {
		require.Contains(t, installer, pin)
	}
}

func TestDeveloperStartScriptsShareWorkspaceSetup(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	commonBytes, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "start-common.sh"))
	require.NoError(t, err)
	common := string(commonBytes)
	for _, function := range []string{
		"reset_ready_marker()",
		"install_git_credential_helper()",
		"prepare_developer_workspace()",
	} {
		require.Contains(t, common, function)
	}

	for _, relativePath := range []string{
		filepath.Join("devmachine", "ide", "start.sh"),
		filepath.Join("devmachine", "headless", "kuayle-headless-start"),
	} {
		scriptBytes, err := os.ReadFile(filepath.Join(repoRoot, relativePath))
		require.NoError(t, err)
		script := string(scriptBytes)
		require.Contains(t, script, `KUAYLE_START_COMMON=${KUAYLE_START_COMMON:-/usr/local/lib/kuayle/start-common.sh}`)
		require.Contains(t, script, `. "$KUAYLE_START_COMMON"`)
		require.Contains(t, script, "reset_ready_marker")
		require.Contains(t, script, "prepare_developer_workspace")
		require.NotContains(t, script, "username=x-access-token", relativePath)
	}
}

func TestRuntimeImagesBundleTheSharedStartHelper(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	for _, relativePath := range []string{
		filepath.Join("devmachine", "agent", "Dockerfile"),
		filepath.Join("devmachine", "browser", "Dockerfile"),
		filepath.Join("devmachine", "collector", "Dockerfile"),
		filepath.Join("devmachine", "egress", "Dockerfile"),
		filepath.Join("devmachine", "headless", "Dockerfile"),
		filepath.Join("devmachine", "ide", "Dockerfile"),
	} {
		content, err := os.ReadFile(filepath.Join(repoRoot, relativePath))
		require.NoError(t, err)
		require.Contains(t, string(content), "common/start-common.sh", relativePath)
	}
	// Images that write the model server Codex config also need its catalog generator.
	for _, image := range []string{"agent", "headless", "ide"} {
		content, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", image, "Dockerfile"))
		require.NoError(t, err)
		require.Contains(t, string(content), "common/codex-model-catalog.cjs", image)
	}
}

func TestHeadlessStartUsesSharedSetupAndWritesReadyMarker(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	homeDir := filepath.Join(tempDir, "home")
	fakeBin := filepath.Join(tempDir, "bin")
	for _, dir := range []string{workspace, homeDir, fakeBin} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "curl"), []byte("#!/bin/sh\nexit 0\n"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "git"), []byte("#!/bin/sh\nexit 0\n"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "ttyd"), []byte(`#!/bin/sh
exec >/dev/null 2>&1
trap 'exit 0' TERM INT
while :; do sleep 1; done
`), 0o700))

	cmd := exec.Command("sh", filepath.Join(repoRoot, "devmachine", "headless", "kuayle-headless-start"))
	cmd.Env = []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"KUAYLE_START_COMMON=" + filepath.Join(repoRoot, "devmachine", "common", "start-common.sh"),
		"KUAYLE_WORKSPACE_ROOT=" + workspace,
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		done <- cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		// Wait owns cmd.ProcessState; synchronize on its completion instead.
		select {
		case <-exited:
		default:
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	readyMarker := filepath.Join(workspace, ".kuayle-ready")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyMarker); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("headless start exited before ready: %v\n%s", err, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("headless start did not write ready marker\n%s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoFileExists(t, filepath.Join(homeDir, ".codex", "config.toml"))
}

func modelServerEnv(homeDir, model, thinking string) []string {
	return []string{
		"HOME=" + homeDir,
		"KUAYLE_MODEL_API_KEY=not-written-to-disk",
		"KUAYLE_MODEL_OPENAI_BASE_URL=https://models.example.test/v1",
		"KUAYLE_MODEL=" + model,
		"KUAYLE_MODEL_THINKING=" + thinking,
	}
}

func TestStartCommonWritesModelServerCodexConfigOnlyWhenAbsent(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	homeDir := t.TempDir()
	common := filepath.Join(repoRoot, "devmachine", "common", "start-common.sh")
	run := func(model, thinking string) {
		cmd := exec.Command("sh", "-eu", "-c", `. "$1"; configure_codex_model`, "kuayle", common)
		cmd.Env = append(os.Environ(), modelServerEnv(homeDir, model, thinking)...)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}

	run("qwen3-coder:30b", "off")
	configPath := filepath.Join(homeDir, ".codex", "config.toml")
	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	config := string(configBytes)
	require.Contains(t, config, `model = "qwen3-coder:30b"`)
	require.Contains(t, config, `model_provider = "kuayle"`)
	require.Contains(t, config, `model_reasoning_effort = "none"`)
	require.Contains(t, config, `base_url = "https://models.example.test/v1"`)
	require.Contains(t, config, `env_key = "KUAYLE_MODEL_API_KEY"`)
	require.NotContains(t, config, "not-written-to-disk")
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	userConfig := "model = \"user-owned\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(userConfig), 0o600))
	run("gpt-oss-120b", "high")
	configBytes, err = os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, userConfig, string(configBytes))
}

// Values are interpolated into TOML strings, so anything needing quoting is refused.
func TestStartCommonRefusesModelServerValuesThatNeedQuoting(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	common := filepath.Join(repoRoot, "devmachine", "common", "start-common.sh")
	for name, override := range map[string]string{
		"quoted URL":   `KUAYLE_MODEL_OPENAI_BASE_URL=https://a.test/" x`,
		"non-http URL": "KUAYLE_MODEL_OPENAI_BASE_URL=file:///etc/passwd",
		"quoted model": `KUAYLE_MODEL=m"x`,
	} {
		homeDir := t.TempDir()
		cmd := exec.Command("sh", "-eu", "-c", `. "$1"; configure_codex_model`, "kuayle", common)
		cmd.Env = append(append(os.Environ(), modelServerEnv(homeDir, "m", "high")...), override)
		_, err := cmd.CombinedOutput()
		require.Error(t, err, name)
		require.NoFileExists(t, filepath.Join(homeDir, ".codex", "config.toml"), name)
	}
}

// Codex's /model picker lists its bundled OpenAI catalog; the server may reject
// those, so the generated config points Codex at a catalog of the server's models.
func TestStartCommonWritesTheServersCodexModelCatalog(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	homeDir := t.TempDir()
	fakeBin := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(`#!/bin/sh
[ "$1 $2" = "debug models" ] || exit 2
printf '%s' '{"models":[{"slug":"gpt-5.5","display_name":"GPT","visibility":"list","priority":13,"shell_type":"unified_exec","context_window":272000,"base_instructions":"bundled"}]}'
`), 0o700))
	nodePath, err := exec.LookPath("node")
	require.NoError(t, err)
	common := filepath.Join(repoRoot, "devmachine", "common", "start-common.sh")
	cmd := exec.Command("sh", "-eu", "-c", `. "$1"; configure_codex_model`, "kuayle", common)
	cmd.Env = append([]string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + filepath.Dir(nodePath) + string(os.PathListSeparator) + os.Getenv("PATH"),
		"KUAYLE_MODEL_LIST=deepseek-flash,deepseek-v4-pro,bad model",
		"KUAYLE_CODEX_CATALOG_GENERATOR=" + filepath.Join(repoRoot, "devmachine", "common", "codex-model-catalog.cjs"),
	}, modelServerEnv(homeDir, "deepseek-v4-pro", "max")...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	catalogPath := filepath.Join(homeDir, ".codex", "model-catalogs", "kuayle.json")
	catalogBytes, err := os.ReadFile(catalogPath)
	require.NoError(t, err)
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(catalogBytes, &catalog))
	slugs := []string{}
	for _, model := range catalog.Models {
		slugs = append(slugs, model["slug"].(string))
		require.Equal(t, "bundled", model["base_instructions"])
		require.Equal(t, "shell_command", model["shell_type"])
	}
	require.Equal(t, []string{"deepseek-flash", "deepseek-v4-pro"}, slugs, "invalid names are dropped")

	configBytes, err := os.ReadFile(filepath.Join(homeDir, ".codex", "config.toml"))
	require.NoError(t, err)
	config := string(configBytes)
	catalogLine := `model_catalog_json = "` + catalogPath + `"`
	require.Contains(t, config, catalogLine)
	require.Less(t, strings.Index(config, catalogLine), strings.Index(config, "[model_providers.kuayle]"))
}

func TestStartCommonSkipsCodexConfigWithoutAnOpenAIServer(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	common := filepath.Join(repoRoot, "devmachine", "common", "start-common.sh")
	for _, drop := range []string{"KUAYLE_MODEL_API_KEY", "KUAYLE_MODEL_OPENAI_BASE_URL"} {
		homeDir := t.TempDir()
		env := []string{"PATH=" + os.Getenv("PATH")}
		for _, value := range modelServerEnv(homeDir, "m", "high") {
			if !strings.HasPrefix(value, drop+"=") {
				env = append(env, value)
			}
		}
		cmd := exec.Command("sh", "-eu", "-c", `. "$1"; configure_codex_model`, "kuayle", common)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
		require.NoFileExists(t, filepath.Join(homeDir, ".codex", "config.toml"), drop)
	}
}

// Codex's managed app-server (~400 MB per version) would fill the small
// in-memory home, so its package cache lives on the workspace volume.
func TestStartCommonKeepsCodexPackagesOnTheWorkspaceVolume(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	common := filepath.Join(repoRoot, "devmachine", "common", "start-common.sh")
	homeDir, workspace := t.TempDir(), t.TempDir()
	run := func() {
		cmd := exec.Command("sh", "-eu", "-c", `. "$1"; link_codex_packages`, "kuayle", common)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + homeDir, "KUAYLE_WORKSPACE_ROOT=" + workspace}
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	run()
	target, err := os.Readlink(filepath.Join(homeDir, ".codex", "packages"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(workspace, ".kuayle", "codex-packages"), target)
	require.DirExists(t, target)
	run() // idempotent

	// An existing packages directory is left alone.
	otherHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(otherHome, ".codex", "packages"), 0o700))
	cmd := exec.Command("sh", "-eu", "-c", `. "$1"; link_codex_packages`, "kuayle", common)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + otherHome, "KUAYLE_WORKSPACE_ROOT=" + workspace}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	info, err := os.Lstat(filepath.Join(otherHome, ".codex", "packages"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	for _, script := range []string{"common/kuayle-agent-runner", "headless/kuayle-headless-start", "ide/start.sh"} {
		content, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", script))
		require.NoError(t, err)
		require.Contains(t, string(content), "link_codex_packages", script)
	}
}

// A stored sign-in is written only into the run's in-memory home, and the file
// the agent refreshed is handed back through the run's own return path.
func TestStartCommonInstallsAndReturnsTheAgentSignIn(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	common := filepath.Join(repoRoot, "devmachine", "common", "start-common.sh")
	homeDir, workspace := t.TempDir(), t.TempDir()
	runID := "6f1c2a3b-0000-4000-8000-000000000001"
	script := `. "$1"
install_agent_sign_in
[ -z "${KUAYLE_CLAUDE_CREDENTIALS:-}" ] || { echo "secret still exported" >&2; exit 1; }
printf '%s' '{"claudeAiOauth":{"refreshToken":"refreshed"}}' > "$HOME/.claude/.credentials.json"
return_agent_sign_in`
	cmd := exec.Command("sh", "-eu", "-c", script, "kuayle", common)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + homeDir, "KUAYLE_WORKSPACE_ROOT=" + workspace,
		"KUAYLE_AGENT_RUN_ID=" + runID, `KUAYLE_CLAUDE_CREDENTIALS={"claudeAiOauth":{"refreshToken":"original"}}`,
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	info, err := os.Stat(filepath.Join(homeDir, ".claude", ".credentials.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	returned := filepath.Join(workspace, ".kuayle", "sign-in-return", runID+".json")
	content, err := os.ReadFile(returned)
	require.NoError(t, err)
	require.JSONEq(t, `{"claudeAiOauth":{"refreshToken":"refreshed"}}`, string(content))
	info, err = os.Stat(returned)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Without a sign-in, or with a malformed run ID, nothing is written back.
	for name, env := range map[string][]string{
		"no sign-in":     {"KUAYLE_AGENT_RUN_ID=" + runID},
		"invalid run id": {"KUAYLE_AGENT_RUN_ID=../escape", `KUAYLE_CODEX_AUTH={"tokens":{}}`},
	} {
		otherWorkspace := t.TempDir()
		cmd := exec.Command("sh", "-eu", "-c", `. "$1"; install_agent_sign_in; return_agent_sign_in`, "kuayle", common)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "KUAYLE_WORKSPACE_ROOT=" + otherWorkspace}, env...)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, name+": "+string(output))
		require.NoDirExists(t, filepath.Join(otherWorkspace, ".kuayle", "sign-in-return"), name)
	}

	// The runner keeps the harness's exit status and hands the sign-in back.
	stub := filepath.Join(t.TempDir(), "start-common.sh")
	require.NoError(t, os.WriteFile(stub, []byte(`. "`+common+`"
load_scoped_secrets() { :; }
install_git_credential_helper() { credential_helper=true; }
`), 0o600))
	runnerHome, runnerWorkspace := t.TempDir(), t.TempDir()
	runner := exec.Command("sh", filepath.Join(repoRoot, "devmachine", "common", "kuayle-agent-runner"),
		"sh", "-c", `printf '%s' '{"tokens":{"refresh_token":"rotated"}}' > "$HOME/.codex/auth.json"; exit 3`)
	runner.Stdin = strings.NewReader("")
	runner.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + runnerHome, "KUAYLE_WORKSPACE_ROOT=" + runnerWorkspace,
		"KUAYLE_START_COMMON=" + stub, "KUAYLE_AGENT_RUN_ID=" + runID, `KUAYLE_CODEX_AUTH={"tokens":{"refresh_token":"original"}}`,
		"GIT_CONFIG_GLOBAL=" + filepath.Join(runnerHome, ".gitconfig"),
	}
	output, err = runner.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, string(output))
	require.Equal(t, 3, exitErr.ExitCode(), "the harness's exit status is kept")
	content, err = os.ReadFile(filepath.Join(runnerWorkspace, ".kuayle", "sign-in-return", runID+".json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"tokens":{"refresh_token":"rotated"}}`, string(content))
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
