package machine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type liveHook struct {
	t      *testing.T
	script string
	inbox  string
	state  string
}

func newLiveHook(t *testing.T) liveHook {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	root := t.TempDir()
	hook := liveHook{
		t: t, script: filepath.Join(repoRootFromCaller(t), "devmachine", "common", "kuayle-live-inbox-hook.cjs"),
		inbox: filepath.Join(root, "inbox"), state: filepath.Join(root, "home", ".kuayle", "live-inbox.json"),
	}
	require.NoError(t, os.MkdirAll(hook.inbox, 0o755))
	return hook
}

// write stores a message as the manager does: domain.AgentLiveMessage.InboxFile.
func (h liveHook) write(seq int64, sender, kind, body string) {
	h.t.Helper()
	file, err := domain.AgentLiveMessage{Seq: &seq, Sender: sender, Kind: kind, Body: body, CreatedAt: time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)}.InboxFile()
	require.NoError(h.t, err)
	require.NoError(h.t, os.WriteFile(filepath.Join(h.inbox, file.Name), file.Data, 0o644))
}

func (h liveHook) run(input string) map[string]any {
	h.t.Helper()
	cmd := exec.Command("node", h.script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(filepath.Dir(h.state)), "KUAYLE_LIVE_INBOX=" + h.inbox}
	cmd.Stdin = strings.NewReader(input)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(h.t, err, stderr.String())
	require.Empty(h.t, stderr.String())
	if strings.TrimSpace(string(output)) == "" {
		return nil
	}
	var decoded map[string]any
	require.NoError(h.t, json.Unmarshal(output, &decoded), string(output))
	return decoded
}

func (h liveHook) delivered() int {
	h.t.Helper()
	data, err := os.ReadFile(h.state)
	require.NoError(h.t, err)
	var state struct {
		Seq int `json:"seq"`
	}
	require.NoError(h.t, json.Unmarshal(data, &state))
	return state.Seq
}

const postToolUse = `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`

// After a tool call Claude receives each new inbox message once, in order,
// as additional context naming its sender; before stopping it is asked to
// continue with messages that arrived meanwhile.
func TestLiveInboxHookDeliversEachMessageOnce(t *testing.T) {
	hook := newLiveHook(t)
	require.Nil(t, hook.run(postToolUse), "an empty inbox adds nothing")
	hook.write(1, domain.AgentLiveSenderUser, domain.AgentLiveKindMessage, "Also add tests")
	hook.write(2, domain.AgentLiveSenderOrchestrator, domain.AgentLiveKindQuestion, "Is the v1 route still used?\n~~~\nfenced")

	output := hook.run(postToolUse)
	specific := output["hookSpecificOutput"].(map[string]any)
	require.Equal(t, "PostToolUse", specific["hookEventName"])
	context := specific["additionalContext"].(string)
	require.Contains(t, context, "Kuayle live channel")
	require.Contains(t, context, "Message 1 from the person who owns this task, 2026-10-29T09:30:00Z:\n~~~text\nAlso add tests\n~~~")
	require.Contains(t, context, "Message 2 from the Kuayle orchestrator, for the person who owns this task (question)")
	require.Contains(t, context, "~ ~ ~\nfenced", "a message cannot close its fence")
	require.Contains(t, context, `"to":"orchestrator"`)
	require.Less(t, strings.Index(context, "Message 1"), strings.Index(context, "Message 2"))
	require.Equal(t, 2, hook.delivered())
	info, err := os.Stat(hook.state)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Nil(t, hook.run(postToolUse), "delivered messages are not repeated")

	hook.write(3, domain.AgentLiveSenderUser, domain.AgentLiveKindMessage, "Use the shared client")
	stop := hook.run(`{"hook_event_name":"Stop","stop_hook_active":false}`)
	require.Equal(t, "block", stop["decision"])
	require.Contains(t, stop["reason"], "Use the shared client")
	require.NotContains(t, stop["reason"], "Also add tests")
	require.Nil(t, hook.run(`{"hook_event_name":"Stop","stop_hook_active":true}`), "Claude may stop once nothing is new")
}

// A file the manager is still writing waits for the next call, and later
// messages wait behind it; one that stays unreadable is skipped. Other
// entries are ignored.
func TestLiveInboxHookWaitsForCompleteFilesInOrder(t *testing.T) {
	hook := newLiveHook(t)
	require.NoError(t, os.WriteFile(filepath.Join(hook.inbox, "1.json"), []byte(`{"seq":1,"from":"user","bo`), 0o644))
	hook.write(2, domain.AgentLiveSenderUser, domain.AgentLiveKindMessage, "second")
	require.NoError(t, os.WriteFile(filepath.Join(hook.inbox, "notes.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hook.inbox, "03.json"), []byte(`{"seq":3,"body":"leading zero"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hook.inbox, "4.json"), []byte(`{"seq":5,"body":"wrong sequence"}`), 0o644))
	require.Nil(t, hook.run(postToolUse))
	require.NoFileExists(t, hook.state)

	old := time.Now().Add(-time.Minute)
	require.NoError(t, os.Chtimes(filepath.Join(hook.inbox, "1.json"), old, old))
	output := hook.run(postToolUse)
	context := output["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	require.Contains(t, context, "second")
	require.NotContains(t, context, "leading zero")
	require.NotContains(t, context, "wrong sequence")
	require.Equal(t, 2, hook.delivered())
}

func TestLiveInboxHookIsSilentWithoutAnInbox(t *testing.T) {
	hook := newLiveHook(t)
	require.NoError(t, os.Remove(hook.inbox))
	require.Nil(t, hook.run(postToolUse))
	require.Nil(t, hook.run(`not json`))
	require.NoFileExists(t, hook.state)
}

// Claude Code reads the hook from the agent image's managed settings, which
// the repository's .claude settings cannot replace and the agent cannot edit
// (read-only root filesystem).
func TestClaudeAgentImageInstallsTheLiveInboxHook(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "claude-managed-settings.json"))
	require.NoError(t, err)
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(data, &settings))
	for _, event := range []string{"PostToolUse", "Stop"} {
		require.Len(t, settings.Hooks[event], 1, event)
		require.Len(t, settings.Hooks[event][0].Hooks, 1, event)
		hook := settings.Hooks[event][0].Hooks[0]
		require.Equal(t, "command", hook.Type)
		require.Equal(t, "node /usr/local/lib/kuayle/kuayle-live-inbox-hook.cjs", hook.Command)
		require.Positive(t, hook.Timeout)
	}
	require.Equal(t, "*", settings.Hooks["PostToolUse"][0].Matcher, "every tool call can deliver")

	dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "agent", "Dockerfile"))
	require.NoError(t, err)
	require.Contains(t, string(dockerfile), "common/kuayle-live-inbox-hook.cjs")
	require.Contains(t, string(dockerfile), "common/claude-managed-settings.json")
	require.Contains(t, string(dockerfile), "/etc/claude-code/managed-settings.json")
}
