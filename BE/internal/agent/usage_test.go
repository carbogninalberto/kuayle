package agent

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The fixtures below follow each harness's event shapes at the pinned CLI
// versions. None is a capture of a real model call (that needs a sign-in);
// each says where its shape comes from.

// claudeNumberedResult is Claude Code's stream-json result as its 2.1.33x
// schema describes it: a numbered result (result_index) carries what this
// process counted since its previous result in model_usage_since_last_result,
// keyed by model. modelUsage and total_cost_usd are running totals that a
// resumed session continues, and usage covers the main loop only.
const claudeNumberedResult = `{"type":"result","subtype":"success","is_error":false,"duration_ms":65000,"duration_api_ms":61000,"num_turns":7,"result":"Added the column; tests pass.","stop_reason":"end_turn","session_id":"0c9a6c1e-2f4b-4c1d-9a7e-5b3c2d1e0f9a","total_cost_usd":1.25,"usage":{"input_tokens":12,"cache_creation_input_tokens":5120,"cache_read_input_tokens":88213,"output_tokens":2210,"server_tool_use":{"web_search_requests":0},"service_tier":"standard"},"modelUsage":{"claude-opus-5-5":{"inputTokens":90,"outputTokens":6000,"cacheReadInputTokens":300000,"cacheCreationInputTokens":20000,"webSearchRequests":0,"costUSD":1.2,"contextWindow":200000,"maxOutputTokens":32000},"claude-haiku-4-5":{"inputTokens":800,"outputTokens":120,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.05,"contextWindow":200000,"maxOutputTokens":8192}},"model_usage_since_last_result":{"claude-opus-5-5":{"inputTokens":40,"outputTokens":2900,"cacheReadInputTokens":120000,"cacheCreationInputTokens":9000,"webSearchRequests":0,"costUSD":0.6,"contextWindow":200000,"maxOutputTokens":32000},"claude-haiku-4-5":{"inputTokens":800,"outputTokens":120,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.05,"contextWindow":200000,"maxOutputTokens":8192}},"permission_denials":[],"result_index":0,"uuid":"6f1d2c3b-4a5e-4f60-8172-93a4b5c6d7e8"}`

// claudeUnnumberedResult is a result of a Claude Code without result
// numbering: only the session's running totals.
const claudeUnnumberedResult = `{"type":"result","subtype":"success","is_error":false,"duration_ms":9000,"num_turns":2,"result":"Done.","session_id":"0c9a6c1e-2f4b-4c1d-9a7e-5b3c2d1e0f9a","total_cost_usd":0.42,"usage":{"input_tokens":3,"cache_creation_input_tokens":100,"cache_read_input_tokens":9000,"output_tokens":300},"modelUsage":{"claude-opus-5-5":{"inputTokens":30,"outputTokens":700,"cacheReadInputTokens":15000,"cacheCreationInputTokens":2000,"webSearchRequests":0,"costUSD":0.42,"contextWindow":200000}},"permission_denials":[]}`

// codexTurnCompleted is Codex's `exec --json` line as printed by the
// repository's Codex review fixture (agent_output_redaction_test.go). From
// 0.170.0 its usage is the thread's running total (ThreadTokenUsage.total),
// which a resumed thread continues.
const codexTurnCompleted = `{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}`

// codexTurnCompletedWithCacheWrites is the same event as Codex 0.170.0
// serializes its Usage struct (exec_events.rs), every field present.
const codexTurnCompletedWithCacheWrites = `{"type":"turn.completed","usage":{"input_tokens":61000,"cached_input_tokens":52000,"cache_write_input_tokens":3000,"output_tokens":900,"reasoning_output_tokens":400}}`

// openCodeFinish is a step_finish as OpenCode 1.19.6 writes it
// (session/processor.ts, Session.getUsage): input without cached tokens,
// output without reasoning, both cache counts, and the cost it computed.
func openCodeFinish(input, output, reasoning, read, write int, cost string) string {
	return `{"type":"step_finish","timestamp":1762855202000,"sessionID":"ses_1","part":{"id":"prt_3","sessionID":"ses_1","messageID":"msg_1","type":"step-finish","reason":"tool-calls","tokens":{"total":0,"input":` +
		strconv.Itoa(input) + `,"output":` + strconv.Itoa(output) + `,"reasoning":` + strconv.Itoa(reasoning) + `,"cache":{"write":` + strconv.Itoa(write) + `,"read":` + strconv.Itoa(read) + `}},"cost":` + cost + `}}`
}

func count(n int64) *int64       { return &n }
func dollars(v float64) *float64 { return &v }

func TestClaudeCodeUsageIsWhatEachProcessCountedSinceItsLastResult(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"system","subtype":"init","cwd":"/workspace/repo","session_id":"0c9a6c1e-2f4b-4c1d-9a7e-5b3c2d1e0f9a","model":"claude-opus-5-5"}`,
		`{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":3,"cache_read_input_tokens":9000},"content":[{"type":"text","text":"Added the column; tests pass."}]}}`,
		claudeNumberedResult,
	}, "\n")
	usage := ParseUsage("claude-code", stdout)
	require.False(t, usage.SessionTotal, "a numbered result states only this process's calls")
	require.Equal(t, count(840), usage.InputTokens, "every model, not only the main loop's usage")
	require.Equal(t, count(3020), usage.OutputTokens)
	require.Equal(t, count(120000), usage.CacheReadTokens)
	require.Equal(t, count(9000), usage.CacheWriteTokens)
	require.InDelta(t, 0.65, *usage.CostUSD, 1e-9, "not total_cost_usd, which a resumed session continues")

	// A second result in the same stream states only what came after the first.
	second := strings.Replace(claudeNumberedResult, `"result_index":0`, `"result_index":1`, 1)
	usage = ParseUsage("claude-code", stdout+"\n"+second)
	require.Equal(t, count(1680), usage.InputTokens)
	require.InDelta(t, 1.3, *usage.CostUSD, 1e-9)

	// A numbered result without the field counted nothing.
	idle := `{"type":"result","subtype":"error_during_execution","is_error":true,"total_cost_usd":1.25,"modelUsage":{"claude-opus-5-5":{"inputTokens":90,"outputTokens":6000,"cacheReadInputTokens":300000,"cacheCreationInputTokens":20000,"costUSD":1.2}},"result_index":0,"session_id":"s"}`
	usage = ParseUsage("claude-code", idle)
	require.False(t, usage.SessionTotal)
	require.Equal(t, count(0), usage.InputTokens)
	require.Equal(t, dollars(0), usage.CostUSD)
}

func TestClaudeCodeWithoutNumberedResultsReportsTheSessionTotal(t *testing.T) {
	usage := ParseUsage("claude-code", claudeUnnumberedResult)
	require.True(t, usage.SessionTotal, "an earlier attempt of the session is in it")
	require.Equal(t, count(30), usage.InputTokens)
	require.Equal(t, count(700), usage.OutputTokens)
	require.Equal(t, count(15000), usage.CacheReadTokens)
	require.Equal(t, count(2000), usage.CacheWriteTokens)
	require.Equal(t, dollars(0.42), usage.CostUSD)
}

func TestCodexUsageIsTheThreadTotalOfItsLastTurn(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"thread.started","thread_id":"019a8f2e-1c4b-7d10-9e55-3f2a1b0c4d5e"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Done."}}`,
		codexTurnCompleted,
	}, "\n")
	usage := ParseUsage("codex", stdout)
	require.True(t, usage.SessionTotal, "Codex reports the thread's running total")
	require.Equal(t, count(315), usage.InputTokens, "Codex counts cached input inside its input")
	require.Equal(t, count(24448), usage.CacheReadTokens)
	require.Nil(t, usage.CacheWriteTokens, "not reported by this version")
	require.Equal(t, count(122), usage.OutputTokens)
	require.Nil(t, usage.CostUSD, "Codex reports no cost")

	// Each turn.completed is a running total: the last one counts, once.
	usage = ParseUsage("codex", stdout+"\n"+codexTurnCompletedWithCacheWrites)
	require.Equal(t, count(9000), usage.InputTokens)
	require.Equal(t, count(52000), usage.CacheReadTokens)
	require.Equal(t, count(3000), usage.CacheWriteTokens)
	require.Equal(t, count(900), usage.OutputTokens, "reasoning is part of Codex's output count")
}

func TestOpenCodeUsageIsTheSumOfItsSteps(t *testing.T) {
	stdout := strings.Join([]string{
		openCodeStepStart, openCodeToolUse,
		openCodeFinish(1000, 150, 50, 800, 0, "0.0012"),
		openCodeStepStart,
		openCodeText("Added the column and its test; go test ./... passes."),
		openCodeFinish(200, 40, 0, 1600, 300, "0.0008"),
	}, "\n")
	usage := ParseUsage("opencode", stdout)
	require.False(t, usage.SessionTotal)
	require.Equal(t, count(1200), usage.InputTokens)
	require.Equal(t, count(240), usage.OutputTokens, "output and reasoning")
	require.Equal(t, count(2400), usage.CacheReadTokens)
	require.Equal(t, count(300), usage.CacheWriteTokens)
	require.InDelta(t, 0.002, *usage.CostUSD, 1e-12)

	// The production line of KUA-136: tokens without cache counts, cost 0.
	usage = ParseUsage("opencode", openCodeStepFinish)
	require.Equal(t, count(1200), usage.InputTokens)
	require.Equal(t, count(80), usage.OutputTokens)
	require.Nil(t, usage.CacheReadTokens)
	require.Equal(t, dollars(0), usage.CostUSD, "a reported zero is a cost")
}

func TestRunsWithoutUsageReportNothing(t *testing.T) {
	for name, tc := range map[string]struct{ provider, stdout string }{
		"claude-code result without usage": {"claude-code", `{"type":"result","subtype":"success","result":"Done."}`},
		"codex turn that failed":           {"codex", `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.failed","error":{"message":"stream disconnected"}}`},
		"opencode step without tokens":     {"opencode", `{"type":"step_finish","part":{"type":"step-finish","reason":"error"}}`},
		"plain output":                     {"codex", "Segmentation fault"},
		"no output":                        {"opencode", ""},
		"harness without known usage":      {"goose", openCodeFinish(1, 1, 0, 0, 0, "0.1") + "\n" + codexTurnCompleted},
	} {
		t.Run(name, func(t *testing.T) {
			usage := ParseUsage(tc.provider, tc.stdout)
			require.False(t, usage.Reported())
			require.Equal(t, Usage{}, usage)
		})
	}
}

func TestAClosingContinuationAddsToTheRunsUsage(t *testing.T) {
	attempt := ParseUsage("claude-code", claudeNumberedResult)
	continuation := ParseUsage("claude-code", strings.Replace(claudeNumberedResult, `"claude-haiku-4-5":{"inputTokens":800`, `"claude-haiku-4-5":{"inputTokens":0`, 2))
	both := attempt.Then(continuation)
	require.Equal(t, count(840+40), both.InputTokens)
	require.InDelta(t, 1.3, *both.CostUSD, 1e-9)
	require.Equal(t, attempt, attempt.Then(Usage{}), "a continuation that reported nothing adds nothing")
	require.Equal(t, continuation, Usage{}.Then(continuation))

	// Codex continues the same thread: its total already holds the attempt.
	codexAttempt := ParseUsage("codex", codexTurnCompleted)
	codexBoth := codexAttempt.Then(ParseUsage("codex", codexTurnCompletedWithCacheWrites))
	require.True(t, codexBoth.SessionTotal)
	require.Equal(t, count(9000), codexBoth.InputTokens)
	require.Equal(t, codexAttempt, codexAttempt.Then(ParseUsage("codex", `{"type":"turn.failed","error":{"message":"x"}}`)))
}
