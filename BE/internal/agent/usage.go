package agent

import (
	"encoding/json"
	"strings"
)

// Usage is what a harness reported a run spent: tokens and the cost it
// estimated, in US dollars. A nil value was not reported. InputTokens never
// includes cache reads or writes, and OutputTokens includes reasoning.
//
// SessionTotal reports that the values are the native session's running
// total, so they include what earlier runs continuing the same session
// spent: a run's own share is that total less what those runs recorded.
type Usage struct {
	InputTokens      *int64
	OutputTokens     *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	CostUSD          *float64
	SessionTotal     bool
}

// Reported reports whether the harness reported anything.
func (u Usage) Reported() bool {
	return u.InputTokens != nil || u.OutputTokens != nil || u.CacheReadTokens != nil || u.CacheWriteTokens != nil || u.CostUSD != nil
}

// Then is the usage of a run whose session was continued after u (the
// closing continuation): the two added up, except that a session total
// already holds u and replaces it.
func (u Usage) Then(next Usage) Usage {
	if !next.Reported() {
		return u
	}
	if next.SessionTotal || !u.Reported() {
		return next
	}
	return Usage{
		InputTokens: addCount(u.InputTokens, next.InputTokens), OutputTokens: addCount(u.OutputTokens, next.OutputTokens),
		CacheReadTokens: addCount(u.CacheReadTokens, next.CacheReadTokens), CacheWriteTokens: addCount(u.CacheWriteTokens, next.CacheWriteTokens),
		CostUSD: addCost(u.CostUSD, next.CostUSD), SessionTotal: u.SessionTotal,
	}
}

// ParseUsage reads what a run's harness reported it spent from its standard
// output. Claude Code, Codex and OpenCode report usage; for any other
// harness, and for output without a usage event, nothing is reported.
func ParseUsage(providerID, stdout string) Usage {
	switch providerID {
	case "claude-code":
		return claudeCodeUsage(stdout)
	case "codex":
		return codexUsage(stdout)
	case "opencode":
		return openCodeUsage(stdout)
	}
	return Usage{}
}

// claudeModelUsage is one model's entry in a Claude Code result.
type claudeModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}

// claudeCodeUsage sums Claude Code's result events. A numbered result
// (result_index, a stream-json run) states in model_usage_since_last_result
// what its process counted since its previous result, for every model
// (subagents and compaction included); without the field it counted nothing.
// Summed over the run's results, that is what the run spent. modelUsage and
// total_cost_usd are instead the session's running totals, which a resumed
// session continues: only a Claude Code that does not number its results is
// read from them, as a session total. A result's usage is the main loop's
// alone and is not read.
func claudeCodeUsage(stdout string) Usage {
	var spent, total Usage
	numbered := false
	eachEvent(stdout, func(line []byte, kind string) {
		if kind != "result" {
			return
		}
		var result struct {
			ResultIndex  *int64                      `json:"result_index"`
			SinceLast    map[string]claudeModelUsage `json:"model_usage_since_last_result"`
			ModelUsage   map[string]claudeModelUsage `json:"modelUsage"`
			TotalCostUSD *float64                    `json:"total_cost_usd"`
		}
		if json.Unmarshal(line, &result) != nil {
			return
		}
		if result.ResultIndex != nil || result.SinceLast != nil {
			if !numbered {
				numbered, spent = true, zeroUsage()
			}
			spent = spent.Then(claudeModels(result.SinceLast))
			return
		}
		if result.ModelUsage != nil {
			total = claudeModels(result.ModelUsage)
			total.CostUSD = result.TotalCostUSD
		} else if result.TotalCostUSD != nil {
			total = Usage{CostUSD: result.TotalCostUSD}
		}
	})
	if numbered {
		return spent
	}
	if total.Reported() {
		total.SessionTotal = true
	}
	return total
}

func claudeModels(models map[string]claudeModelUsage) Usage {
	if len(models) == 0 {
		return Usage{}
	}
	sum := zeroUsage()
	for _, model := range models {
		*sum.InputTokens += model.InputTokens
		*sum.OutputTokens += model.OutputTokens
		*sum.CacheReadTokens += model.CacheReadInputTokens
		*sum.CacheWriteTokens += model.CacheCreationInputTokens
		*sum.CostUSD += model.CostUSD
	}
	return sum
}

// codexUsage reads the last turn.completed. Its usage is the thread's
// running total (ThreadTokenUsage.total, Codex 0.170 and later), which a
// resumed thread continues from its rollout, so it is a session total; one
// exec runs one turn, and adding turns would count earlier ones again. Codex
// counts cached input inside input_tokens, reasoning inside output_tokens,
// and reports no cost.
func codexUsage(stdout string) Usage {
	usage := Usage{}
	eachEvent(stdout, func(line []byte, kind string) {
		if kind != "turn.completed" {
			return
		}
		var turn struct {
			Usage *struct {
				InputTokens           int64  `json:"input_tokens"`
				CachedInputTokens     int64  `json:"cached_input_tokens"`
				CacheWriteInputTokens *int64 `json:"cache_write_input_tokens"`
				OutputTokens          int64  `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &turn) != nil || turn.Usage == nil {
			return
		}
		input := max(turn.Usage.InputTokens-turn.Usage.CachedInputTokens, 0)
		usage = Usage{InputTokens: &input, OutputTokens: &turn.Usage.OutputTokens, CacheReadTokens: &turn.Usage.CachedInputTokens,
			CacheWriteTokens: turn.Usage.CacheWriteInputTokens, SessionTotal: true}
	})
	return usage
}

// openCodeUsage sums OpenCode's step_finish events: each states one step's
// tokens (input without cache reads and writes, output without reasoning)
// and the cost OpenCode computed for it.
func openCodeUsage(stdout string) Usage {
	usage := Usage{}
	eachEvent(stdout, func(line []byte, kind string) {
		if kind != "step_finish" {
			return
		}
		var step struct {
			Part struct {
				Tokens *struct {
					Input     int64 `json:"input"`
					Output    int64 `json:"output"`
					Reasoning int64 `json:"reasoning"`
					Cache     *struct {
						Read  int64 `json:"read"`
						Write int64 `json:"write"`
					} `json:"cache"`
				} `json:"tokens"`
				Cost *float64 `json:"cost"`
			} `json:"part"`
		}
		if json.Unmarshal(line, &step) != nil || (step.Part.Tokens == nil && step.Part.Cost == nil) {
			return
		}
		spent := Usage{CostUSD: step.Part.Cost}
		if tokens := step.Part.Tokens; tokens != nil {
			output := tokens.Output + tokens.Reasoning
			spent.InputTokens, spent.OutputTokens = &tokens.Input, &output
			if tokens.Cache != nil {
				spent.CacheReadTokens, spent.CacheWriteTokens = &tokens.Cache.Read, &tokens.Cache.Write
			}
		}
		usage = usage.Then(spent)
	})
	return usage
}

// eachEvent calls fn with every JSON event line of a stream and its type.
func eachEvent(stdout string, fn func(line []byte, kind string)) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !json.Valid([]byte(line)) {
			continue
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &envelope) == nil && envelope.Type != "" {
			fn([]byte(line), envelope.Type)
		}
	}
}

func zeroUsage() Usage {
	var input, output, read, write int64
	var cost float64
	return Usage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &read, CacheWriteTokens: &write, CostUSD: &cost}
}

func addCount(a, b *int64) *int64 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	sum := *a + *b
	return &sum
}

func addCost(a, b *float64) *float64 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	sum := *a + *b
	return &sum
}
