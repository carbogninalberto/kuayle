package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// testdata/usage holds real output lines captured on 2026-11-24 from the CLI
// versions P0.1 (KUA-254) pins, with Kuayle's own argv:
//   - claude-code-*.jsonl: Claude Code 2.1.330, a first run and the same
//     session resumed. codex-*.jsonl: Codex 0.171.0, a first run and the same
//     thread resumed. Both ran against a stand-in for the provider's streaming
//     API, so the event lines are the CLIs' own and the token numbers are the
//     stand-in's (1200 input, 800 cache read, 300 cache write, 42 output, 10
//     reasoning per request); each run made one request.
//   - opencode-*.jsonl: OpenCode 2.3.2 against a free model: a run with a
//     shell call and a final text, and the same session continued. Real
//     tokens; the cost is 0 because the model is free.
//
// Variants built from them by hand say so where they are made.
func usageSample(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "usage", name))
	require.NoError(t, err)
	return string(raw)
}

func count(n int64) *int64       { return &n }
func dollars(v float64) *float64 { return &v }

func TestClaudeCodeUsageIsWhatEachProcessCountedSinceItsLastResult(t *testing.T) {
	first := ParseUsage("claude-code", usageSample(t, "claude-code-first.jsonl"))
	require.False(t, first.SessionTotal, "a numbered result states only this process's calls")
	require.Equal(t, count(1200), first.InputTokens)
	require.Equal(t, count(42), first.OutputTokens, "not the assistant event's output_tokens as of message_start")
	require.Equal(t, count(800), first.CacheReadTokens)
	require.Equal(t, count(300), first.CacheWriteTokens)
	require.InDelta(t, 0.005595, *first.CostUSD, 1e-9)

	// Resumed, Claude Code reports the session's totals in total_cost_usd and
	// modelUsage (2400 input tokens, twice the cost): the run spent half.
	resumed := ParseUsage("claude-code", usageSample(t, "claude-code-resumed.jsonl"))
	require.Equal(t, first, resumed)

	// A second result in the same process states only what came after the
	// first one: both are counted.
	both := ParseUsage("claude-code", usageSample(t, "claude-code-first.jsonl")+usageSample(t, "claude-code-resumed.jsonl"))
	require.Equal(t, count(2400), both.InputTokens)
	require.InDelta(t, 0.01119, *both.CostUSD, 1e-9)

	// Variant: a subagent ran on another model. Every model counts, not only
	// the main loop that usage covers.
	line := strings.TrimSpace(usageSample(t, "claude-code-resumed.jsonl"))
	withSubagent := strings.Replace(line, `"model_usage_since_last_result":{`, `"model_usage_since_last_result":{"claude-haiku-4-5":{"inputTokens":800,"outputTokens":120,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.05},`, 1)
	usage := ParseUsage("claude-code", withSubagent)
	require.Equal(t, count(2000), usage.InputTokens)
	require.Equal(t, count(162), usage.OutputTokens)
	require.InDelta(t, 0.055595, *usage.CostUSD, 1e-9)

	// Variant: a numbered result without the field counted nothing.
	idle := strings.Replace(line, `"model_usage_since_last_result"`, `"unused"`, 1)
	usage = ParseUsage("claude-code", idle)
	require.False(t, usage.SessionTotal)
	require.Equal(t, count(0), usage.InputTokens)
	require.Equal(t, dollars(0), usage.CostUSD)
}

// Variant of the resumed line without result numbering (a Claude Code that
// does not number its results): only the session's running totals are left.
func TestClaudeCodeWithoutNumberedResultsReportsTheSessionTotal(t *testing.T) {
	line := strings.TrimSpace(usageSample(t, "claude-code-resumed.jsonl"))
	at := strings.Index(line, `,"result_index":0`)
	require.Positive(t, at)
	usage := ParseUsage("claude-code", line[:at]+"}")
	require.True(t, usage.SessionTotal, "the session's earlier run is in it")
	require.Equal(t, count(2400), usage.InputTokens)
	require.Equal(t, count(84), usage.OutputTokens)
	require.Equal(t, count(1600), usage.CacheReadTokens)
	require.Equal(t, count(600), usage.CacheWriteTokens)
	require.InDelta(t, 0.01119, *usage.CostUSD, 1e-9)
}

func TestCodexUsageIsTheThreadTotalOfItsLastTurn(t *testing.T) {
	first := ParseUsage("codex", usageSample(t, "codex-first.jsonl"))
	require.True(t, first.SessionTotal, "Codex reports the thread's running total")
	require.Equal(t, count(400), first.InputTokens, "Codex counts cached input inside its input")
	require.Equal(t, count(800), first.CacheReadTokens)
	require.Equal(t, count(0), first.CacheWriteTokens)
	require.Equal(t, count(42), first.OutputTokens, "reasoning is part of Codex's output count")
	require.Nil(t, first.CostUSD, "Codex reports no cost")

	// The resumed thread reports both runs (2400 input): the store takes the
	// first run's share off (domain.AgentRunUsage.Less).
	resumed := ParseUsage("codex", usageSample(t, "codex-resumed.jsonl"))
	require.True(t, resumed.SessionTotal)
	require.Equal(t, count(800), resumed.InputTokens)
	require.Equal(t, count(1600), resumed.CacheReadTokens)
	require.Equal(t, count(84), resumed.OutputTokens)

	// Each turn.completed is a running total: the last one counts, once.
	require.Equal(t, resumed, ParseUsage("codex", usageSample(t, "codex-first.jsonl")+usageSample(t, "codex-resumed.jsonl")))
	// Variant: an older Codex without cache_write_input_tokens.
	older := ParseUsage("codex", `{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}`)
	require.Equal(t, count(315), older.InputTokens)
	require.Nil(t, older.CacheWriteTokens, "not reported by that version")
}

func TestOpenCodeUsageIsTheSumOfItsSteps(t *testing.T) {
	usage := ParseUsage("opencode", usageSample(t, "opencode-run.jsonl"))
	require.False(t, usage.SessionTotal)
	require.Equal(t, count(4128+4220), usage.InputTokens)
	require.Equal(t, count(40+36+3+0), usage.OutputTokens, "output and reasoning")
	require.Equal(t, count(1920+1920), usage.CacheReadTokens)
	require.Equal(t, count(0), usage.CacheWriteTokens)
	require.Equal(t, dollars(0), usage.CostUSD, "a free model's cost is a reported zero")

	// A continued session reports only its own steps.
	usage = ParseUsage("opencode", usageSample(t, "opencode-resumed.jsonl"))
	require.Equal(t, count(147), usage.InputTokens)
	require.Equal(t, count(6), usage.OutputTokens)
	require.Equal(t, count(6016), usage.CacheReadTokens)

	// The production line of KUA-136 (an older OpenCode): no cache counts.
	usage = ParseUsage("opencode", openCodeStepFinish)
	require.Equal(t, count(1200), usage.InputTokens)
	require.Equal(t, count(80), usage.OutputTokens)
	require.Nil(t, usage.CacheReadTokens)
	require.Equal(t, dollars(0), usage.CostUSD)

	// Variant: a priced model adds up its steps' costs.
	priced := strings.ReplaceAll(usageSample(t, "opencode-run.jsonl"), `"cost":0,`, `"cost":0.0012,`)
	require.InDelta(t, 0.0024, *ParseUsage("opencode", priced).CostUSD, 1e-12)
}

// Pi states each assistant message's own usage (one model request) on its
// message_end: input without cache reads and writes, output with reasoning
// (pi-ai's Usage), and cost.total, its estimate from the model catalog. The
// lines are Pi 1.6.0's recorded ones (pi_test.go); the priced and cached
// variant is made by hand.
func TestPiUsageIsTheSumOfItsAssistantMessages(t *testing.T) {
	run := piStream(piSession, piAgentStart, piTurnStart, piUserMessage, piToolCall, piToolStart, piToolEnd, piToolResult, piTextEnd, piConclusion, piAgentSettled)
	usage := ParseUsage("pi", run)
	require.False(t, usage.SessionTotal, "Pi keeps no session, so a run reports only its own requests")
	require.Equal(t, count(100+150), usage.InputTokens, "the streamed message_update usage is not counted again")
	require.Equal(t, count(10+8), usage.OutputTokens)
	require.Equal(t, count(0), usage.CacheReadTokens)
	require.Equal(t, count(0), usage.CacheWriteTokens)
	require.Equal(t, dollars(0), usage.CostUSD, "an unpriced model's cost is a reported zero")

	priced := strings.ReplaceAll(run, `"cacheRead":0,"cacheWrite":0,"reasoning":0`, `"cacheRead":800,"cacheWrite":300,"reasoning":4`)
	priced = strings.ReplaceAll(priced, `"total":0,"totalFromProvider":false`, `"total":0.0012,"totalFromProvider":false`)
	usage = ParseUsage("pi", priced)
	require.Equal(t, count(1600), usage.CacheReadTokens)
	require.Equal(t, count(600), usage.CacheWriteTokens)
	require.Equal(t, count(18), usage.OutputTokens, "reasoning is part of Pi's output")
	require.InDelta(t, 0.0024, *usage.CostUSD, 1e-12)

	// A request that failed after an answer adds nothing to it: Pi gives the
	// failed message an empty usage.
	require.Equal(t, ParseUsage("pi", run), ParseUsage("pi", run+piStream(piProviderError)))
}

func TestRunsWithoutUsageReportNothing(t *testing.T) {
	for name, tc := range map[string]struct{ provider, stdout string }{
		"claude-code result without usage":      {"claude-code", `{"type":"result","subtype":"success","result":"Done."}`},
		"claude-code run cut before its result": {"claude-code", strings.Join(strings.Split(usageSample(t, "claude-code-first.jsonl"), "\n")[:2], "\n")},
		"codex turn that failed":                {"codex", `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.failed","error":{"message":"stream disconnected"}}`},
		"opencode error":                        {"opencode", `{"type":"error","timestamp":1795529441353,"sessionID":"ses_df21b00e8ffekWT27Y4U2wGBaH","error":{"type":"provider.no-route","message":"Model unavailable: bogus/model"}}`},
		"opencode step without tokens":          {"opencode", `{"type":"step_finish","part":{"type":"step-finish","reason":"error"}}`},
		"plain output":                          {"codex", "Segmentation fault"},
		"no output":                             {"opencode", ""},
		"pi request that kept failing":          {"pi", piStream(piSession, piAgentStart, piTurnStart, piUserMessage, piProviderError, piRetryFailed)},
		"harness without known usage":           {"goose", usageSample(t, "opencode-run.jsonl") + usageSample(t, "codex-first.jsonl")},
	} {
		t.Run(name, func(t *testing.T) {
			usage := ParseUsage(tc.provider, tc.stdout)
			require.False(t, usage.Reported())
			require.Equal(t, Usage{}, usage)
		})
	}
}

func TestAClosingContinuationAddsToTheRunsUsage(t *testing.T) {
	// Claude Code: the continuation is a process of its own, resuming the
	// session; what each process counted adds up.
	attempt := ParseUsage("claude-code", usageSample(t, "claude-code-first.jsonl"))
	continuation := ParseUsage("claude-code", usageSample(t, "claude-code-resumed.jsonl"))
	both := attempt.Then(continuation)
	require.Equal(t, count(2400), both.InputTokens)
	require.InDelta(t, 0.01119, *both.CostUSD, 1e-9)
	require.Equal(t, attempt, attempt.Then(Usage{}), "a continuation that reported nothing adds nothing")
	require.Equal(t, continuation, Usage{}.Then(continuation))

	// Codex continues the same thread: its total already holds the attempt.
	codexAttempt := ParseUsage("codex", usageSample(t, "codex-first.jsonl"))
	codexBoth := codexAttempt.Then(ParseUsage("codex", usageSample(t, "codex-resumed.jsonl")))
	require.True(t, codexBoth.SessionTotal)
	require.Equal(t, count(800), codexBoth.InputTokens)
	require.Equal(t, codexAttempt, codexAttempt.Then(ParseUsage("codex", `{"type":"turn.failed","error":{"message":"x"}}`)))
}
