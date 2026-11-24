package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func tokens(n int64) *int64 { return &n }

// A harness that reports its session's running total (Codex) states a run's
// own share once the session's earlier runs are taken off.
func TestASessionTotalLessTheEarlierRunsIsTheRunsShare(t *testing.T) {
	total := AgentRunUsage{InputTokens: tokens(900), OutputTokens: tokens(300), CacheReadTokens: tokens(5000)}
	earlier := AgentRunUsage{InputTokens: tokens(600), OutputTokens: tokens(350), CacheWriteTokens: tokens(10)}
	share := total.Less(earlier)
	require.Equal(t, tokens(300), share.InputTokens)
	require.Equal(t, tokens(0), share.OutputTokens, "never below zero, when the session lost its count")
	require.Equal(t, tokens(5000), share.CacheReadTokens, "an earlier run that reported nothing takes nothing off")
	require.Nil(t, share.CacheWriteTokens, "what the run did not report stays unreported")
	require.Nil(t, share.CostUSD)
	require.Equal(t, total, total.Less(AgentRunUsage{}))
}
