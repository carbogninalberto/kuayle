package machine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// terminalHome is a machine user's in-memory home for the terminal scripts.
type terminalHome struct {
	repoRoot, home, bin string
}

func newTerminalHome(t *testing.T) terminalHome {
	t.Helper()
	dir := t.TempDir()
	home := terminalHome{repoRoot: repoRootFromCaller(t), home: filepath.Join(dir, "home"), bin: filepath.Join(dir, "bin")}
	require.NoError(t, os.MkdirAll(home.home, 0o700))
	require.NoError(t, os.MkdirAll(home.bin, 0o700))
	return home
}

func (h terminalHome) env(extra ...string) []string {
	return append([]string{
		"HOME=" + h.home, "PATH=" + h.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"KUAYLE_AGENT_SIGN_IN=" + filepath.Join(h.repoRoot, "devmachine", "common", "kuayle-agent-sign-in.sh"),
		"KUAYLE_CLAUDE_CONFIG=" + filepath.Join(h.repoRoot, "devmachine", "common", "kuayle-claude-config.cjs"),
	}, extra...)
}

// sync runs the manager's sync script on the archive the manager would send.
func (h terminalHome) sync(t *testing.T, files map[string]string) {
	t.Helper()
	archive, err := signInArchive(files)
	require.NoError(t, err)
	cmd := exec.Command("/bin/sh", "-c", syncAgentSignInsScript, "kuayle-agent-sign-ins")
	cmd.Env = h.env()
	cmd.Stdin = strings.NewReader(archive)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

// shell sources the sign-in hook like kuayle-shell and reports the token it
// exported and what it printed.
func (h terminalHome) shell(t *testing.T, extra ...string) (token, output string) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", `. "$KUAYLE_AGENT_SIGN_IN"; printf 'TOKEN=%s\n' "${CLAUDE_CODE_OAUTH_TOKEN:-}"`)
	cmd.Env = h.env(extra...)
	raw, err := cmd.CombinedOutput()
	require.NoError(t, err, string(raw))
	text := string(raw)
	index := strings.LastIndex(text, "TOKEN=")
	require.GreaterOrEqual(t, index, 0)
	return strings.TrimSpace(text[index+len("TOKEN="):]), text[:index]
}

func (h terminalHome) read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(h.home, path))
	require.NoError(t, err)
	return string(content)
}

// A synced token reaches each new shell, is replaced when it changes and is
// gone once removed in Kuayle. The files stay readable only by the user.
func TestTerminalShellUsesTheSyncedClaudeToken(t *testing.T) {
	home := newTerminalHome(t)
	home.sync(t, map[string]string{"managed": "claude-code\n", "claude-code.token": "fixture-token-0001"})
	info, err := os.Stat(filepath.Join(home.home, ".kuayle", "agent-sign-ins"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(home.home, ".kuayle", "agent-sign-ins", "claude-code.token"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	token, output := home.shell(t)
	require.Equal(t, "fixture-token-0001", token)
	require.Empty(t, output, "no hint once signed in")

	home.sync(t, map[string]string{"managed": "claude-code\n", "claude-code.token": "fixture-token-0002"})
	token, _ = home.shell(t)
	require.Equal(t, "fixture-token-0002", token, "the next shell uses the updated token")

	home.sync(t, map[string]string{"managed": "claude-code\n"})
	token, output = home.shell(t)
	require.Empty(t, token, "a revoked token is not used by the next shell")
	require.Contains(t, output, "claude setup-token")
	require.Contains(t, output, "Settings > Agent configuration > Your sign-ins")

	// The machine's own key or model server wins, and needs no hint.
	token, output = home.shell(t, "ANTHROPIC_AUTH_TOKEN=model-server-key")
	require.Empty(t, token)
	require.Empty(t, output)
}

// A captured credentials file is installed only where the user has no login
// of their own, follows changes in Kuayle and is removed when revoked; a
// login the user made in the terminal is never overwritten or removed.
func TestTerminalShellInstallsCapturedSignInsWithoutOverwritingTheUsers(t *testing.T) {
	home := newTerminalHome(t)
	home.sync(t, map[string]string{"managed": "claude-code\ncodex\n", "claude-code.credentials.json": `{"a":1}`, "codex.auth.json": `{"c":1}`})
	_, output := home.shell(t)
	require.Empty(t, output)
	require.Equal(t, `{"a":1}`, home.read(t, ".claude/.credentials.json"))
	require.Equal(t, `{"c":1}`, home.read(t, ".codex/auth.json"))
	info, err := os.Stat(filepath.Join(home.home, ".codex", "auth.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	home.sync(t, map[string]string{"managed": "claude-code\ncodex\n", "claude-code.credentials.json": `{"a":2}`})
	_, output = home.shell(t)
	require.Equal(t, `{"a":2}`, home.read(t, ".claude/.credentials.json"), "an update replaces the installed file")
	require.NoFileExists(t, filepath.Join(home.home, ".codex", "auth.json"), "a revoked sign-in is removed")
	require.Contains(t, output, "codex login --device-auth")

	// The user signs in to Claude Code in the terminal themselves.
	require.NoError(t, os.WriteFile(filepath.Join(home.home, ".claude", ".credentials.json"), []byte(`{"own":1}`), 0o600))
	home.sync(t, map[string]string{"managed": "claude-code\n", "claude-code.credentials.json": `{"a":3}`})
	home.shell(t)
	require.Equal(t, `{"own":1}`, home.read(t, ".claude/.credentials.json"))
	home.sync(t, map[string]string{"managed": "claude-code\n", "claude-code.token": "fixture-token-0001"})
	token, _ := home.shell(t)
	require.Empty(t, token, "the user's own login keeps precedence over a saved token")
	require.Equal(t, `{"own":1}`, home.read(t, ".claude/.credentials.json"))
}

// Without anything synced (an older manager, or an agent the policy does not
// allow) the shell stays quiet.
func TestTerminalShellIsQuietForAgentsKuayleDoesNotManage(t *testing.T) {
	home := newTerminalHome(t)
	_, output := home.shell(t)
	require.Empty(t, output)
	home.sync(t, map[string]string{})
	_, output = home.shell(t)
	require.Empty(t, output)
}

// The Claude Code config is pre-seeded to skip onboarding and trust the
// workspace, keeping everything the user set.
func TestClaudeConfigSeedKeepsTheUsersSettings(t *testing.T) {
	home := newTerminalHome(t)
	seed := func() map[string]any {
		cmd := exec.Command("node", filepath.Join(home.repoRoot, "devmachine", "common", "kuayle-claude-config.cjs"), filepath.Join(home.home, ".claude.json"), "/workspace")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(home.read(t, ".claude.json")), &config))
		return config
	}
	config := seed()
	require.Equal(t, true, config["hasCompletedOnboarding"])
	require.Equal(t, map[string]any{"/workspace": map[string]any{"hasTrustDialogAccepted": true}}, config["projects"])
	info, err := os.Stat(filepath.Join(home.home, ".claude.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.WriteFile(filepath.Join(home.home, ".claude.json"), []byte(`{"hasCompletedOnboarding":false,"theme":"light","oauthAccount":{"emailAddress":"own@example.test"},"projects":{"/workspace":{"hasTrustDialogAccepted":false,"allowedTools":["x"]}}}`), 0o600))
	config = seed()
	require.Equal(t, false, config["hasCompletedOnboarding"])
	require.Equal(t, "light", config["theme"])
	require.NotNil(t, config["oauthAccount"])
	require.Equal(t, map[string]any{"hasTrustDialogAccepted": false, "allowedTools": []any{"x"}}, config["projects"].(map[string]any)["/workspace"])

	require.NoError(t, os.WriteFile(filepath.Join(home.home, ".claude.json"), []byte(`not json`), 0o600))
	cmd := exec.Command("node", filepath.Join(home.repoRoot, "devmachine", "common", "kuayle-claude-config.cjs"), filepath.Join(home.home, ".claude.json"), "/workspace")
	require.NoError(t, cmd.Run())
	require.Equal(t, "not json", home.read(t, ".claude.json"), "an unreadable config is left alone")
}

// A ktask- terminal session runs Claude Code on that task; anything else in
// the name is refused.
func TestTerminalSessionOpensATasksClaudeCode(t *testing.T) {
	home := newTerminalHome(t)
	log := filepath.Join(home.home, "tmux.log")
	require.NoError(t, os.WriteFile(filepath.Join(home.bin, "tmux"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$KUAYLE_FAKE_TMUX_LOG\"\n"), 0o700))
	script := filepath.Join(home.repoRoot, "devmachine", "ide", "terminal-session.sh")
	run := func(session string) error {
		cmd := exec.Command("sh", script, session, "/workspace/tasks/rec-1")
		cmd.Env = home.env("KUAYLE_FAKE_TMUX_LOG="+log, "KUAYLE_TASK_CLAUDE=kuayle-task-claude")
		return cmd.Run()
	}
	task, native := "0b0c7e0e-1d1f-4c3a-9d6e-2f3a4b5c6d7e", "5e6f7a8b-9c0d-4e1f-8a2b-3c4d5e6f7a8b"
	require.NoError(t, run("ktask-0a1b2c3d-"+task+"-"+native))
	require.NoError(t, run("ktask-0a1b2c3d-"+task))
	require.NoError(t, run("term-plain"))
	require.Error(t, run("ktask-0a1b2c3d-"+task+"x"))
	require.Error(t, run("ktask-"+task))
	lines := strings.Split(strings.TrimSpace(home.read(t, "tmux.log")), "\n")
	require.Len(t, lines, 3)
	require.True(t, strings.HasSuffix(lines[0], "kuayle-task-claude "+task+" "+native), lines[0])
	require.True(t, strings.HasSuffix(lines[1], "kuayle-task-claude "+task+" "), lines[1])
	require.True(t, strings.HasSuffix(lines[2], "-s term-plain -c /workspace/tasks"), lines[2])
}

// The task launcher resumes the task's own session from its transcripts on
// the workspace volume, with a config directory in the in-memory home, and
// starts a new session when the transcript is not on this machine.
func TestTaskClaudeResumesTheTasksSession(t *testing.T) {
	home := newTerminalHome(t)
	workspace := filepath.Join(filepath.Dir(home.home), "workspace")
	task, native := "0b0c7e0e-1d1f-4c3a-9d6e-2f3a4b5c6d7e", "5e6f7a8b-9c0d-4e1f-8a2b-3c4d5e6f7a8b"
	transcripts := filepath.Join(workspace, ".kuayle", "agent-sessions", task, "claude")
	require.NoError(t, os.MkdirAll(filepath.Join(transcripts, "-workspace-tasks-rec-1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home.bin, "claude"), []byte("#!/bin/sh\nprintf 'ARGS=%s CONFIG=%s TOKEN=%s\\n' \"$*\" \"$CLAUDE_CONFIG_DIR\" \"${CLAUDE_CODE_OAUTH_TOKEN:-}\"\n"), 0o700))
	home.sync(t, map[string]string{"managed": "claude-code\n", "claude-code.token": "fixture-token-0001"})
	run := func(args ...string) string {
		cmd := exec.Command("sh", append([]string{filepath.Join(home.repoRoot, "devmachine", "common", "kuayle-task-claude")}, args...)...)
		cmd.Env = home.env("KUAYLE_WORKSPACE_ROOT="+workspace, "KUAYLE_TASK_SHELL=true")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
		return string(output)
	}
	config := filepath.Join(home.home, ".kuayle", "claude-tasks", task)

	output := run(task, native)
	require.Contains(t, output, "ARGS= CONFIG="+config+" TOKEN=fixture-token-0001", "no transcript yet: a new session")
	require.Contains(t, output, "starting a new session")

	require.NoError(t, os.WriteFile(filepath.Join(transcripts, "-workspace-tasks-rec-1", native+".jsonl"), []byte("{}\n"), 0o600))
	output = run(task, native)
	require.Contains(t, output, "ARGS=--resume "+native+" CONFIG="+config+" TOKEN=fixture-token-0001")
	link, err := os.Readlink(filepath.Join(config, "projects"))
	require.NoError(t, err)
	require.Equal(t, transcripts, link, "the terminal shares the task's transcripts")
	var seeded map[string]any
	require.NoError(t, json.Unmarshal([]byte(home.read(t, ".kuayle/claude-tasks/"+task+"/.claude.json")), &seeded))
	require.Equal(t, true, seeded["hasCompletedOnboarding"])
	entries, err := os.ReadDir(filepath.Join(workspace, ".kuayle", "agent-sessions", task))
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing but transcripts goes to the workspace volume")

	cmd := exec.Command("sh", filepath.Join(home.repoRoot, "devmachine", "common", "kuayle-task-claude"), "../etc", native)
	cmd.Env = home.env("KUAYLE_WORKSPACE_ROOT=" + workspace)
	require.Error(t, cmd.Run())
}
