package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// OpenCode's `run --format json` events as recorded in production (KUA-136):
// a step starts, tools run, the step finishes with reason "tool-calls".
const (
	openCodeStepStart  = `{"type":"step_start","timestamp":1762855200000,"sessionID":"ses_1","part":{"id":"prt_1","sessionID":"ses_1","messageID":"msg_1","type":"step-start"}}`
	openCodeToolUse    = `{"type":"tool_use","timestamp":1762855201000,"sessionID":"ses_1","part":{"id":"prt_2","sessionID":"ses_1","messageID":"msg_1","type":"tool","callID":"call_1","tool":"bash","state":{"status":"completed","input":{"command":"git status --short"},"output":" M BE/main.go\n","title":"git status","metadata":{"exit":0},"time":{"start":1762855200500,"end":1762855201000}}}}`
	openCodeStepFinish = `{"type":"step_finish","timestamp":1762855202000,"sessionID":"ses_1","part":{"id":"prt_3","sessionID":"ses_1","messageID":"msg_1","type":"step-finish","reason":"tool-calls","cost":0,"tokens":{"input":1200,"output":80}}}`
	openCodeError      = `{"type":"error","timestamp":1762855203000,"sessionID":"ses_1","error":{"name":"APIError","data":{"message":"429 The model is overloaded, try again later","statusCode":429,"isRetryable":true}}}`
)

func openCodeText(text string) string {
	return `{"type":"text","timestamp":1762855204000,"sessionID":"ses_1","part":{"id":"prt_4","sessionID":"ses_1","messageID":"msg_2","type":"text","text":"` + text + `","time":{"start":1762855203000,"end":1762855204000}}}`
}

// Production: five OpenCode runs ended on a tool call with exit code 0, were
// recorded as succeeded, and their summary was 16 KB of event JSON.
func TestOpenCodeRunWithOnlyToolCallsHasNoSummaryAndIsNotASuccess(t *testing.T) {
	stdout := strings.Join([]string{openCodeStepStart, openCodeToolUse, openCodeStepFinish, openCodeStepStart, openCodeToolUse, openCodeStepFinish}, "\n") + "\n"
	result := NewOpenCodeProvider("").ParseResult(stdout, "", 0)
	require.Empty(t, result.Summary, "event JSON is never a summary")
	require.False(t, result.FinalMessage)
	require.Equal(t, "failed", result.Status)
	require.True(t, result.NoFinalMessage)
	require.Empty(t, result.FailureReason)
}

func TestOpenCodeRunEndingInAnErrorSaysWhichError(t *testing.T) {
	stdout := strings.Join([]string{openCodeStepStart, openCodeToolUse, openCodeStepFinish, openCodeError}, "\n") + "\n"
	for _, exit := range []int{0, 1} {
		result := NewOpenCodeProvider("").ParseResult(stdout, "", exit)
		require.Empty(t, result.Summary)
		require.Equal(t, "failed", result.Status)
		require.Equal(t, "The harness reported an error: APIError: 429 The model is overloaded, try again later", result.FailureReason)
		require.Equal(t, exit == 0, result.NoFinalMessage, "a nonzero exit already is a failure")
	}
	// Text the agent wrote before the error is not its conclusion.
	result := NewOpenCodeProvider("").ParseResult(openCodeText("I'll start by inspecting the repository.")+"\n"+stdout, "", 0)
	require.False(t, result.FinalMessage)
	require.Equal(t, "failed", result.Status)
	// An error the harness recovered from does not fail a run that concluded.
	result = NewOpenCodeProvider("").ParseResult(stdout+openCodeText("Added the column; tests pass.")+"\n", "", 0)
	require.Equal(t, "succeeded", result.Status)
	require.True(t, result.FinalMessage)
	require.Empty(t, result.FailureReason)
}

func TestOpenCodeRunWithAFinalTextSucceedsWithItAsTheSummary(t *testing.T) {
	stdout := strings.Join([]string{openCodeStepStart, openCodeText("I'll start by inspecting the repository."), openCodeToolUse, openCodeStepFinish,
		openCodeStepStart, openCodeText("Added the column and its test; go test ./... passes."), strings.Replace(openCodeStepFinish, "tool-calls", "stop", 1)}, "\n") + "\n"
	result := NewOpenCodeProvider("").ParseResult(stdout, "", 0)
	require.Equal(t, "succeeded", result.Status)
	require.True(t, result.FinalMessage)
	require.False(t, result.NoFinalMessage)
	require.Equal(t, "Added the column and its test; go test ./... passes.", result.Summary)
}

// Every harness whose final-message event Kuayle knows fails a run that
// exits 0 without one; none stores its events as the summary.
func TestRunsWithoutAFinalMessageAreNotSuccessesOnAnyKnownHarness(t *testing.T) {
	for name, tc := range map[string]struct {
		provider Provider
		stdout   string
	}{
		"codex":       {NewCodexProvider(""), `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"item.completed","item":{"id":"i","type":"command_execution","command":"ls","aggregated_output":"a\nb"}}` + "\n" + `{"type":"turn.completed"}`},
		"claude-code": {NewClaudeCodeProvider(""), `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}` + "\n" + `{"type":"result","subtype":"success","result":""}`},
		"opencode":    {NewOpenCodeProvider(""), openCodeStepStart + "\n" + openCodeToolUse + "\n" + openCodeStepFinish},
	} {
		t.Run(name, func(t *testing.T) {
			result := tc.provider.ParseResult(tc.stdout, "", 0)
			require.Equal(t, "failed", result.Status)
			require.True(t, result.NoFinalMessage)
			require.Empty(t, result.Summary)
			require.NotContains(t, result.Summary, `{"type"`)
		})
	}
}

// Harnesses that print plain text keep it as the summary and their status;
// event lines mixed into it are left out.
func TestPlainOutputStaysTheSummaryWithoutItsEventLines(t *testing.T) {
	result := NewCrushProvider("").ParseResult("Done: added the column.\n", "", 0)
	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, "Done: added the column.", result.Summary)
	require.False(t, result.NoFinalMessage)

	result = NewCustomCLIProvider("img:1").ParseResult(`{"type":"progress","step":1}`+"\nDone.\n"+`{"type":"progress","step":2}`+"\n", "", 0)
	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, "Done.", result.Summary)

	result = NewQwenCodeProvider("").ParseResult(`{"type":"system","subtype":"init"}`+"\n", "boom\n", 1)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "boom", result.Summary, "what the harness printed on stderr still explains a failure")
}

// A Claude-style stream ends with a result event, which is the conclusion
// even when the last assistant message also called a tool.
func TestAResultEventOutranksInterimAssistantText(t *testing.T) {
	stdout := `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me check."},{"type":"tool_use","name":"Bash"}]}}` + "\n" + `{"type":"result","subtype":"success","result":"Done: column added."}`
	result := NewQwenCodeProvider("").ParseResult(stdout, "", 0)
	require.True(t, result.FinalMessage)
	require.Equal(t, "Done: column added.", result.Summary)
}
