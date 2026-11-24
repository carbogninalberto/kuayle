package machine

import (
	"strconv"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func usageCount(n int64) *int64 { return &n }

// claudeResult is a numbered Claude Code result whose process counted the
// given tokens and cost since its previous result.
func claudeResult(text string, input, output, read, write int, cost string) string {
	return `{"type":"result","subtype":"success","is_error":false,"result":"` + text + `","total_cost_usd":9.99,"modelUsage":{"claude-opus-5-5":{"inputTokens":99999,"outputTokens":99999,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":9.99}},` +
		`"model_usage_since_last_result":{"claude-opus-5-5":{"inputTokens":` + strconv.Itoa(input) + `,"outputTokens":` + strconv.Itoa(output) + `,"cacheReadInputTokens":` + strconv.Itoa(read) + `,"cacheCreationInputTokens":` + strconv.Itoa(write) + `,"costUSD":` + cost + `}},"result_index":0,"session_id":"0c9a6c1e-session"}`
}

// A run records what its harness reported it spent, and the closing
// continuation that continued its session adds to it.
func TestARunRecordsItsUsageWithTheClosingContinuations(t *testing.T) {
	completed, _, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout:       claudeResult("Implemented the toggle and ran the tests.", 40, 2900, 120000, 9000, "0.6"),
		problem:      `"instructions" is the people's instructions, which an agent cannot write; remove it from the update`,
		secondStdout: claudeResult("Updated the notebook.", 5, 70, 130000, 0, "0.07"),
		afterAsk:     func(store *notebookStoreFake) { store.updates, store.problem = 1, "" }})
	require.Len(t, runtime.runs, 2, "the attempt and its closing continuation")
	require.Equal(t, usageCount(45), completed.InputTokens)
	require.Equal(t, usageCount(2970), completed.OutputTokens)
	require.Equal(t, usageCount(250000), completed.CacheReadTokens)
	require.Equal(t, usageCount(9000), completed.CacheWriteTokens)
	require.InDelta(t, 0.67, *completed.CostUSD, 1e-9)
	require.False(t, completed.UsageSessionTotal)
	require.Equal(t, "Implemented the toggle and ran the tests.", *completed.Summary, "the continuation's reply is still not the summary")
}

// Codex continues the same thread in the closing continuation and reports
// the thread's running total: the continuation's total stands for the run,
// and the store takes the session's earlier runs off it.
func TestACodexRunRecordsItsThreadTotal(t *testing.T) {
	conclusion := `{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Implemented the toggle and ran the tests."}}`
	completed, _, runtime := notebookRun(t, notebookScenario{provider: "codex", required: true, session: "019a8f2e-1c4b-7d10-9e55-3f2a1b0c4d5e", continuation: resumeCommand,
		stdout:       conclusion + "\n" + `{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}`,
		problem:      `"instructions" is the people's instructions, which an agent cannot write; remove it from the update`,
		secondStdout: `{"type":"turn.completed","usage":{"input_tokens":50000,"cached_input_tokens":49000,"cache_write_input_tokens":0,"output_tokens":300,"reasoning_output_tokens":0}}`,
		afterAsk:     func(store *notebookStoreFake) { store.updates, store.problem = 1, "" }})
	require.Len(t, runtime.runs, 2)
	require.True(t, completed.UsageSessionTotal)
	require.Equal(t, usageCount(1000), completed.InputTokens)
	require.Equal(t, usageCount(49000), completed.CacheReadTokens)
	require.Equal(t, usageCount(300), completed.OutputTokens)
	require.Nil(t, completed.CostUSD)
}

// A harness that reported nothing leaves the run's usage unreported.
func TestARunWithoutUsageRecordsNone(t *testing.T) {
	completed, _, runtime := notebookRun(t, notebookScenario{provider: "opencode"})
	require.Len(t, runtime.runs, 1)
	require.Equal(t, domain.AgentRunUsage{}, completed.AgentRunUsage)
	finish := `{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":1000,"output":150,"reasoning":50,"cache":{"write":0,"read":800}},"cost":0.0012}}`
	completed, _, _ = notebookRun(t, notebookScenario{provider: "opencode", stdout: openCodeConclusion + "\n" + finish})
	require.Equal(t, usageCount(1000), completed.InputTokens)
	require.Equal(t, usageCount(200), completed.OutputTokens)
	require.Equal(t, usageCount(800), completed.CacheReadTokens)
}
