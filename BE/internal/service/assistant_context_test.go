package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func toolRound(id, arguments, result string) []chatCompletionMessage {
	call := chatToolCall{ID: id, Type: "function"}
	call.Function.Name, call.Function.Arguments = "search_issues", arguments
	return []chatCompletionMessage{
		{Role: "assistant", Content: "Looking.", ToolCalls: []chatToolCall{call}},
		{Role: "tool", ToolCallID: id, Content: result},
	}
}

func TestAssistantCompactsOlderToolResultsWithinBudget(t *testing.T) {
	prefix := []chatCompletionMessage{{Role: "system", Content: strings.Repeat("s", 5000)}, {Role: "user", Content: "Search"}}
	messages := append([]chatCompletionMessage{}, prefix...)
	for i := range 10 {
		messages = append(messages, toolRound(fmt.Sprintf("call_%d", i), `{"query":"dark mode"}`, fmt.Sprintf(`{"untrusted_data":"%d%s"}`, i, strings.Repeat("x", 1000)))...)
	}
	compactToolContext(messages, len(prefix), 3500)
	require.Equal(t, prefix, messages[:len(prefix)], "the conversation itself is never compacted")
	kept, omitted := 0, 0
	for i, m := range messages[len(prefix):] {
		switch m.Role {
		case "tool":
			require.NotEmpty(t, m.ToolCallID, "every call keeps its paired result")
			if m.Content == assistantOmittedToolResult {
				omitted++
				require.Less(t, kept, 1, "older results are omitted before newer ones")
			} else {
				kept++
			}
		case "assistant":
			require.Len(t, m.ToolCalls, 1, fmt.Sprint(i))
			require.NotEmpty(t, m.ToolCalls[0].ID)
		}
	}
	require.Equal(t, 3, kept, "the newest results that fit the budget stay complete")
	require.Equal(t, 7, omitted)
	require.Contains(t, messages[len(messages)-1].Content, `"9`)
	require.Equal(t, "{}", messages[len(prefix)].ToolCalls[0].Function.Arguments, "omitted rounds drop their arguments too")
	require.Equal(t, `{"query":"dark mode"}`, messages[len(messages)-2].ToolCalls[0].Function.Arguments)

	again := append([]chatCompletionMessage{}, messages...)
	compactToolContext(again, len(prefix), 3500)
	require.Equal(t, messages, again, "compaction is stable across rounds")
}

func TestAssistantStopsOfferingToolsNearTheReplyDeadline(t *testing.T) {
	require.Equal(t, 10*time.Minute, assistantReplyTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), assistantFinalAnswerReserve/2)
	defer cancel()
	require.False(t, toolsAllowed(ctx, 0, 0))
	ctx, cancel = context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	require.True(t, toolsAllowed(ctx, 0, 0))
	require.False(t, toolsAllowed(ctx, assistantMaxToolRounds, 0))
	require.False(t, toolsAllowed(ctx, 0, assistantMaxToolCalls))
	require.True(t, toolsAllowed(context.Background(), 1, 1))
}
