package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Pi 1.6.0's events, recorded with `pi --print --mode json --no-session
// --no-approve --model fake/m1 -- "say hello"` against a local OpenAI
// compatible server: the agent says something, runs a tool in the same
// message, then concludes. Only the working directory was shortened.
const (
	piSession       = `{"type":"session","version":3,"id":"01a20e43-c759-73db-aaf2-88fa380c58b8","timestamp":"2026-11-24T15:54:12.699Z","cwd":"/workspace"}`
	piAgentStart    = `{"type":"agent_start"}`
	piTurnStart     = `{"type":"turn_start"}`
	piUserMessage   = `{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"say hello"}],"timestamp":1795535652761}}`
	piToolCall      = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hello"}}],"api":"openai-completions","provider":"fake","model":"m1","usage":{"input":100,"output":10,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":110,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0,"totalFromProvider":false}},"stopReason":"toolUse","timestamp":1795535652795,"responseId":"c1","rawStopReason":"tool_calls","durationMs":43,"thinkingLevel":"high"}}`
	piToolStart     = `{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hello"}}`
	piToolEnd       = `{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hello\n"}],"structuredContent":{"output":"hello\n","truncated":false,"exit_code":0,"wall_time_seconds":0}},"isError":false,"durationMs":10}`
	piToolResult    = `{"type":"message_end","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"hello\n"}],"isError":false,"durationMs":10,"timestamp":1795535652851}}`
	piTextEnd       = `{"type":"message_update","usage":{"input":150,"output":8,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":158,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0,"totalFromProvider":false}},"assistantMessageEvent":{"type":"text_end","contentIndex":0,"content":"Done: the command printed hello."}}`
	piConclusion    = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Done: the command printed hello."}],"api":"openai-completions","provider":"fake","model":"m1","usage":{"input":150,"output":8,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":158,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0,"totalFromProvider":false}},"stopReason":"stop","timestamp":1795535652853,"responseId":"c1","rawStopReason":"stop","durationMs":4,"thinkingLevel":"high"}}`
	piAgentSettled  = `{"type":"agent_settled","aborted":false}`
	piProviderError = `{"type":"message_end","message":{"role":"assistant","content":[],"api":"openai-completions","provider":"fake","model":"m1","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"error","timestamp":1795535681220,"errorMessage":"500: {\"message\":\"upstream exploded\"}","durationMs":7,"thinkingLevel":"medium"}}`
	piRetryFailed   = `{"type":"auto_retry_end","success":false,"attempt":3,"finalError":"500: {\"message\":\"upstream exploded\"}"}`
)

func piStream(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestPiRunsOnePromptWithJSONEventsAndNoSession(t *testing.T) {
	invocation, err := NewPiProvider("").BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "--help is not a flag here"})

	require.NoError(t, err)
	require.Equal(t, []string{"pi", "--print", "--mode", "json", "--no-session", "--no-approve", "--", "--help is not a flag here"}, invocation.Argv)
	require.False(t, invocation.Interactive)
	require.Empty(t, invocation.Environment)
}

// Pi names a model provider/model and takes Kuayle's effort names as its
// thinking levels. Extra arguments are Pi's flags, so they go before the
// prompt separator.
func TestPiTakesTheModelEffortAndExtraArgumentsBeforeThePrompt(t *testing.T) {
	for _, level := range []string{"off", "low", "medium", "high", "xhigh", "max"} {
		invocation, err := NewPiProvider("").BuildInvocation(RunInput{
			Mode: ModeAutonomous, Prompt: "fix the issue", Model: "opencode-go/deepseek-v4.1-flash", Thinking: level,
			ExtraArgs: []string{"--offline"},
		})
		require.NoError(t, err, level)
		require.Equal(t, []string{
			"pi", "--print", "--mode", "json", "--no-session", "--no-approve",
			"--model", "opencode-go/deepseek-v4.1-flash", "--thinking", level, "--offline", "--", "fix the issue",
		}, invocation.Argv, level)
	}
	for _, input := range []RunInput{
		{Mode: ModeAutonomous, Prompt: "x", Model: "--extension=/tmp/evil.ts"},
		{Mode: ModeAutonomous, Prompt: "x", Thinking: "ultra"},
		{Mode: ModeAutonomous, Prompt: "x", OpenAIBaseURL: "https://models.example.test/v1"},
	} {
		_, err := NewPiProvider("").BuildInvocation(input)
		require.Error(t, err, input)
	}
}

func TestPiRunThatConcludesReportsItsLastMessage(t *testing.T) {
	stdout := piStream(piSession, piAgentStart, piTurnStart, piUserMessage, piToolCall, piToolStart, piToolEnd, piToolResult,
		piTurnStart, piTextEnd, piConclusion, piAgentSettled)

	result := NewPiProvider("").ParseResult(stdout, "", 0)

	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, "Done: the command printed hello.", result.Summary)
	require.True(t, result.FinalMessage)
	require.False(t, result.NoFinalMessage)
	require.Empty(t, result.FailureReason)
}

// What the agent says beside a tool call is what it said on the way.
func TestPiRunEndingOnAToolCallHasNoConclusion(t *testing.T) {
	result := NewPiProvider("").ParseResult(piStream(piSession, piUserMessage, piToolCall, piToolStart, piToolEnd, piToolResult), "", 0)

	require.Equal(t, "failed", result.Status)
	require.True(t, result.NoFinalMessage)
	require.False(t, result.FinalMessage)
	require.Empty(t, result.FailureReason)
}

// Pi exits 0 when the model provider keeps failing; its last assistant
// message carries the error.
func TestPiRunEndingInAProviderErrorSaysWhichError(t *testing.T) {
	stdout := piStream(piSession, piUserMessage, piProviderError, piRetryFailed, piAgentSettled)
	for _, exit := range []int{0, 1} {
		result := NewPiProvider("").ParseResult(stdout, "", exit)
		require.Equal(t, "failed", result.Status, exit)
		require.False(t, result.FinalMessage, exit)
		require.Equal(t, `The harness reported an error: 500: {"message":"upstream exploded"}`, result.FailureReason, exit)
	}
	// A conclusion after a recovered error is the run's result.
	result := NewPiProvider("").ParseResult(piStream(piSession, piUserMessage, piProviderError, piConclusion, piAgentSettled), "", 0)
	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, "Done: the command printed hello.", result.Summary)
	require.Empty(t, result.FailureReason)
}

// Pi reports a refusal before the run starts (an unknown model, no API key) on
// standard error and exits 1.
func TestPiRunRefusedAtStartKeepsTheHarnessMessage(t *testing.T) {
	result := NewPiProvider("").ParseResult(piStream(piSession), "No API key found for anthropic.\n", 1)

	require.Equal(t, "failed", result.Status)
	require.Equal(t, "No API key found for anthropic.", result.Summary)
	require.False(t, result.FinalMessage)
}
