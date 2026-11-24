package domain

// AgentRunUsage is what an agent run's harness reported it spent: tokens,
// and the cost it estimated in US dollars (an estimate, not a bill). A nil
// value was not reported. InputTokens excludes cache reads and writes, and
// OutputTokens includes reasoning. A run's usage includes its closing
// continuation; a task's is the sum over its runs.
type AgentRunUsage struct {
	InputTokens      *int64   `json:"input_tokens,omitempty" db:"input_tokens"`
	OutputTokens     *int64   `json:"output_tokens,omitempty" db:"output_tokens"`
	CacheReadTokens  *int64   `json:"cache_read_tokens,omitempty" db:"cache_read_tokens"`
	CacheWriteTokens *int64   `json:"cache_write_tokens,omitempty" db:"cache_write_tokens"`
	CostUSD          *float64 `json:"cost_usd,omitempty" db:"cost_usd"`
}

// Less is a session's running total less what its earlier runs recorded:
// the run's own share. A value the run did not report stays unreported, and
// none goes below zero.
func (u AgentRunUsage) Less(earlier AgentRunUsage) AgentRunUsage {
	count := func(total, spent *int64) *int64 {
		if total == nil || spent == nil {
			return total
		}
		share := max(*total-*spent, 0)
		return &share
	}
	share := AgentRunUsage{InputTokens: count(u.InputTokens, earlier.InputTokens), OutputTokens: count(u.OutputTokens, earlier.OutputTokens),
		CacheReadTokens: count(u.CacheReadTokens, earlier.CacheReadTokens), CacheWriteTokens: count(u.CacheWriteTokens, earlier.CacheWriteTokens), CostUSD: u.CostUSD}
	if u.CostUSD != nil && earlier.CostUSD != nil {
		cost := max(*u.CostUSD-*earlier.CostUSD, 0)
		share.CostUSD = &cost
	}
	return share
}
