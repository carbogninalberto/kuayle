package machine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func usageCount(n int64) *int64 { return &n }

// usageSample is a real output capture of the agent package's usage tests
// (internal/agent/testdata/usage; provenance in usage_test.go there).
func usageSample(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "agent", "testdata", "usage", name))
	require.NoError(t, err)
	return string(raw)
}

const missingNotebook = `"instructions" is the people's instructions, which an agent cannot write; remove it from the update`

// A run records what its harness reported it spent, and the closing
// continuation that resumed its session adds to it: Claude Code's resumed
// result carries the session's totals, but only what that process counted is
// added.
func TestARunRecordsItsUsageWithTheClosingContinuations(t *testing.T) {
	completed, _, runtime := notebookRun(t, notebookScenario{provider: "claude-code", required: true, session: "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b", continuation: resumeCommand,
		stdout: usageSample(t, "claude-code-first.jsonl"), problem: missingNotebook,
		secondStdout: usageSample(t, "claude-code-resumed.jsonl"),
		afterAsk:     func(store *notebookStoreFake) { store.updates, store.problem = 1, "" }})
	require.Len(t, runtime.runs, 2, "the attempt and its closing continuation")
	require.Equal(t, usageCount(2400), completed.InputTokens)
	require.Equal(t, usageCount(84), completed.OutputTokens)
	require.Equal(t, usageCount(1600), completed.CacheReadTokens)
	require.Equal(t, usageCount(600), completed.CacheWriteTokens)
	require.InDelta(t, 0.01119, *completed.CostUSD, 1e-9, "twice the run's cost, not the resumed session's total added to it")
	require.False(t, completed.UsageSessionTotal)
	require.Equal(t, "done", *completed.Summary)
}

// Codex continues the same thread in the closing continuation and reports
// the thread's running total: the continuation's total stands for the run,
// and the store takes the thread's earlier runs off it.
func TestACodexRunRecordsItsThreadTotal(t *testing.T) {
	completed, _, runtime := notebookRun(t, notebookScenario{provider: "codex", required: true, session: "01a20dfc-3afe-72f0-808d-40d2daae0827", continuation: resumeCommand,
		stdout: usageSample(t, "codex-first.jsonl"), problem: missingNotebook,
		secondStdout: usageSample(t, "codex-resumed.jsonl"),
		afterAsk:     func(store *notebookStoreFake) { store.updates, store.problem = 1, "" }})
	require.Len(t, runtime.runs, 2)
	require.True(t, completed.UsageSessionTotal)
	require.Equal(t, usageCount(800), completed.InputTokens)
	require.Equal(t, usageCount(1600), completed.CacheReadTokens)
	require.Equal(t, usageCount(84), completed.OutputTokens)
	require.Nil(t, completed.CostUSD)
}

// A harness that reported nothing leaves the run's usage unreported; one
// without a closing continuation records its own.
func TestARunWithoutUsageRecordsNone(t *testing.T) {
	completed, _, runtime := notebookRun(t, notebookScenario{provider: "opencode"})
	require.Len(t, runtime.runs, 1)
	require.Equal(t, domain.AgentRunUsage{}, completed.AgentRunUsage)
	completed, _, _ = notebookRun(t, notebookScenario{provider: "opencode", stdout: usageSample(t, "opencode-run.jsonl")})
	require.Equal(t, usageCount(8348), completed.InputTokens)
	require.Equal(t, usageCount(79), completed.OutputTokens)
	require.Equal(t, usageCount(3840), completed.CacheReadTokens)
	require.Equal(t, 0.0, *completed.CostUSD)
}
