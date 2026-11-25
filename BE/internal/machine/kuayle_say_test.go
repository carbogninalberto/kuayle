package machine

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// kuayleSay runs devmachine/common/kuayle-say.cjs in dir with its outbox in
// outbox and returns its exit code and standard error.
func kuayleSay(t *testing.T, dir, outbox, stdin string, args ...string) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	script := filepath.Join(repoRootFromCaller(t), "devmachine", "common", "kuayle-say.cjs")
	cmd := exec.Command("node", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "KUAYLE_OUTBOX_DIR="+outbox)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stderr.String()
	}
	require.NoError(t, err)
	return 0, stderr.String()
}

// outboxFiles parses every finished file of the outbox as Kuayle does.
func outboxFiles(t *testing.T, outbox string) []domain.AgentOutboxMessage {
	t.Helper()
	entries, err := os.ReadDir(outbox)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var messages []domain.AgentOutboxMessage
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), "."), "no half-written file is left")
		data, err := os.ReadFile(filepath.Join(outbox, entry.Name()))
		require.NoError(t, err)
		message, err := domain.ParseAgentOutboxMessage(data)
		require.NoError(t, err, string(data))
		messages = append(messages, message)
	}
	return messages
}

func TestKuayleSayWritesRoomMessagesKuayleReads(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		args  []string
		stdin string
		want  domain.AgentOutboxMessage
	}{
		{[]string{"--to", "@reviewer", "--ask", "Is", "v1", "still", "used?"}, "",
			domain.AgentOutboxMessage{To: "@reviewer", Body: "Is v1 still used?", Room: &domain.AgentOutboxRoom{ToRoles: []string{"reviewer"}, Kind: domain.RoomAsk}}},
		{[]string{"--to", "@ALL", "--reply-to", "4", "-"}, "Pushed the fix.\n", domain.AgentOutboxMessage{To: "@all", Body: "Pushed the fix.", Room: &domain.AgentOutboxRoom{Kind: domain.RoomSay}}},
		{[]string{"--to", "@implementer", "--verdict", "request_changes", "--commit", "abc1234", "--file", "-"}, "1. The v1 route has no test.",
			domain.AgentOutboxMessage{To: "@implementer", Body: "1. The v1 route has no test.", Room: &domain.AgentOutboxRoom{ToRoles: []string{"implementer"}, Kind: domain.RoomVerdict, Data: domain.TaskRoomData{Verdict: "request_changes", Commit: "abc1234"}}}},
		{[]string{"--to", "@reviewer", "--request", "review"}, "", domain.AgentOutboxMessage{To: "@reviewer", Room: &domain.AgentOutboxRoom{ToRoles: []string{"reviewer"}, Kind: domain.RoomRequest, Data: domain.TaskRoomData{Action: "review"}}}},
	}
	for _, c := range cases {
		outbox := filepath.Join(t.TempDir(), "outbox")
		code, stderr := kuayleSay(t, dir, outbox, c.stdin, c.args...)
		require.Zero(t, code, stderr)
		messages := outboxFiles(t, outbox)
		require.Len(t, messages, 1)
		got := messages[0]
		got.Channel = c.want.Channel
		if c.want.Channel == "" {
			got.Channel = ""
		}
		if got.ReplyTo != nil {
			require.Equal(t, int64(4), *got.ReplyTo)
			got.ReplyTo = nil
		}
		require.Equal(t, c.want, got, strings.Join(c.args, " "))
	}
}

func TestKuayleSayRefusesWhatKuayleWouldRefuseAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	for name, args := range map[string][]string{
		"no recipient":         {"hello"},
		"an issue":             {"--to", "ENG-12", "hello"},
		"an empty message":     {"--to", "@reviewer"},
		"an unknown kind":      {"--to", "@reviewer", "--kind", "shout", "x"},
		"a bad verdict":        {"--to", "@reviewer", "--verdict", "lgtm", "--commit", "abc1234"},
		"a commit that is not": {"--to", "@reviewer", "--verdict", "approve", "--commit", "HEAD~1"},
		"a bad reply":          {"--to", "@reviewer", "--reply-to", "zero", "x"},
		"too long":             {"--to", "@reviewer", strings.Repeat("x", domain.AgentMessageMaxRunes+1)},
		"text and a file":      {"--to", "@reviewer", "--file", "notes.md", "x"},
		"a missing file":       {"--to", "@reviewer", "--file", "missing.md"},
	} {
		outbox := filepath.Join(t.TempDir(), "outbox")
		code, stderr := kuayleSay(t, dir, outbox, "", args...)
		require.Equal(t, 2, code, name)
		require.Contains(t, stderr, "kuayle-say: ", name)
		require.Empty(t, outboxFiles(t, outbox), name)
	}
}

func TestTheAgentImagesInstallKuayleSay(t *testing.T) {
	for _, image := range []string{"agent", "ide", "headless"} {
		dockerfile, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "devmachine", image, "Dockerfile"))
		require.NoError(t, err)
		require.Contains(t, string(dockerfile), "COPY --chmod=0755 common/kuayle-say.cjs /usr/local/bin/kuayle-say", image)
	}
}

func TestKuayleSaySplitRequest(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "outbox")
	oversize := []string{"--to", "@human", "--request", "split"}
	for range 10 {
		oversize = append(oversize, "--issue", "Child", strings.Repeat("界", 600))
	}
	for _, args := range [][]string{
		oversize,
		{"--to", "@human", "--request", "split", "the owner approves"},
		{"--to", "@human", "--issue", "One", "Detail"},
		{"--to", "@human", "--request", "split", "--issue", "", "Detail"},
		{"--to", "@human", "--request", "split", "--issue", "One", strings.Repeat("x", 1001)},
	} {
		code, stderr := kuayleSay(t, dir, outbox, "", args...)
		require.Equal(t, 2, code, stderr)
		require.Empty(t, outboxFiles(t, outbox))
	}
	code, stderr := kuayleSay(t, dir, outbox, "", "--to", "@human", "--request", "split", "--issue", "One", "First part", "--issue", "Two", "Second part")
	require.Zero(t, code, stderr)
	messages := outboxFiles(t, outbox)
	require.Len(t, messages, 1)
	data, err := json.Marshal(messages[0].Room.Data)
	require.NoError(t, err)
	require.JSONEq(t, `{"action":"split","issues":[{"title":"One","description":"First part"},{"title":"Two","description":"Second part"}]}`, string(data))
}
