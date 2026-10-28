package service

import (
	"context"
	"time"
)

const (
	// assistantReplyTimeout bounds one reply, including every model round and
	// tool call. The attempt's lease is renewed while it runs.
	assistantReplyTimeout = 10 * time.Minute
	// assistantFinalAnswerReserve is kept free for the final, tool-free answer:
	// closer to the deadline the model is no longer offered tools.
	assistantFinalAnswerReserve = 90 * time.Second
	// assistantLeaseRenewal is how often a live reply renews its lease; it is
	// well below the repository's lease, so a slow round never loses it.
	assistantLeaseRenewal = 30 * time.Second
	// assistantContextBytes bounds the tool traffic (the model's text and
	// arguments of each tool round, and the tool results) resent every round.
	assistantContextBytes = 128 << 10
)

// assistantOmittedToolResult replaces older tool results once the context
// budget is spent. The call and its pairing stay, so the transcript remains a
// valid tool conversation; the model can call the tool again if it needs it.
const assistantOmittedToolResult = `{"omitted":"This earlier tool result was removed to keep the conversation within its size limit. Call the tool again if you still need it."}`

// compactToolContext keeps the newest tool rounds of messages[from:] complete
// within budget bytes. Everything older than the first round that does not fit
// keeps its calls and pairing but loses its text, arguments and results. The
// conversation before from (system prompt, history, user turn) is unchanged.
func compactToolContext(messages []chatCompletionMessage, from, budget int) {
	used, full := 0, true
	for i := len(messages) - 1; i >= from; i-- {
		message := &messages[i]
		switch message.Role {
		case "tool":
			if message.Content == assistantOmittedToolResult {
				continue
			}
			if full && used+len(message.Content) <= budget {
				used += len(message.Content)
				continue
			}
			full = false
			message.Content = assistantOmittedToolResult
		case "assistant":
			size := len(message.Content)
			for _, call := range message.ToolCalls {
				size += len(call.Function.Arguments)
			}
			if full && used+size <= budget {
				used += size
				continue
			}
			full = false
			message.Content = ""
			for j := range message.ToolCalls {
				message.ToolCalls[j].Function.Arguments = "{}"
			}
		}
	}
}

// toolsAllowed reports whether the next model round may still call tools. The
// final round offers none, so the model must answer with what it has.
func toolsAllowed(ctx context.Context, round, calls int) bool {
	if round >= assistantMaxToolRounds || calls >= assistantMaxToolCalls {
		return false
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < assistantFinalAnswerReserve {
		return false
	}
	return true
}
