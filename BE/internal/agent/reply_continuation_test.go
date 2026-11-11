package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The one continuation that asks a finished attempt for its reply runs the
// same harness with the same model and limits on the same session, with the
// new prompt only: exactly what the provider builds for a resumed attempt.
func TestReplyContinuationResumesTheSameSessionWithTheNewPromptOnly(t *testing.T) {
	const session = "0199a213-81c0-7800-8aa1-bbab2a035a53"
	dir := "/workspace/.kuayle/agent-sessions/0199a213-81c0-7800-8aa1-bbab2a035a54"
	for name, provider := range map[string]Provider{"claude-code": NewClaudeCodeProvider(""), "codex": NewCodexProvider("")} {
		input := func(prompt string, s SessionInput) RunInput {
			s.Dir = dir
			return RunInput{Mode: ModeAutonomous, Prompt: prompt, Model: "model-x", WorkspacePath: "/workspace/tasks/eng-1", Session: &s}
		}
		want, err := provider.BuildInvocation(input("Reply only", SessionInput{ResumeID: session}))
		require.NoError(t, err, name)
		starts := map[string]SessionInput{"a resumed attempt": {ResumeID: "0199a213-81c0-7800-8aa1-bbab2a035a55"}, "a first attempt": {}}
		if name == "claude-code" {
			starts["a first attempt"] = SessionInput{NewID: session}
		}
		for kind, start := range starts {
			first, err := provider.BuildInvocation(input("Implement dark mode", start))
			require.NoError(t, err, name)
			got, ok := ReplyContinuation(name, first.Argv, session, "Reply only")
			require.True(t, ok, "%s, %s", name, kind)
			require.Equal(t, want.Argv, got, "%s, %s", name, kind)
			require.NotContains(t, got, "Implement dark mode")
		}
		first, err := provider.BuildInvocation(input("Implement dark mode", SessionInput{}))
		require.NoError(t, err)
		_, ok := ReplyContinuation(name, first.Argv, "not a session; rm -rf /", "Reply only")
		require.False(t, ok)
		// An attempt without the session wrapper (not a task's) is not continued.
		_, ok = ReplyContinuation(name, first.Argv[5:], session, "Reply only")
		require.False(t, ok)
	}
	opencode, err := NewOpenCodeProvider("").BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "Implement", WorkspacePath: "/workspace/tasks/eng-1"})
	require.NoError(t, err)
	_, ok := ReplyContinuation("opencode", append([]string{"/bin/sh", "-c", "script", "kuayle-session", dir}, opencode.Argv...), session, "Reply only")
	require.False(t, ok, "OpenCode cannot continue a session")
}
