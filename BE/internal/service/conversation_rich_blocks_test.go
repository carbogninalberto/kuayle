package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Both the tools and the conversation-only prompt tell the model about the two
// rich fences the web chat renders, so a request for a chart or a widget gets
// a block the UI can draw instead of Markdown prose.
func TestGuidanceMessagesDescribeRichChatBlocks(t *testing.T) {
	messages := guidanceMessages(false)
	require.Len(t, messages, 1)
	require.Equal(t, "system", messages[0].Role)
	require.Equal(t, richBlocksPrompt, messages[0].Content)
	require.Contains(t, messages[0].Content, "Kuayle web chat")
	require.Contains(t, messages[0].Content, "```chart")
	require.Contains(t, messages[0].Content, "```html")
	require.Contains(t, messages[0].Content, "sandboxed with no network")
	require.Contains(t, messages[0].Content, "never invent data")

	withImages := guidanceMessages(true)
	require.Len(t, withImages, 2)
	require.Contains(t, withImages[0].Content, "Users can attach images")
	require.Equal(t, richBlocksPrompt, withImages[1].Content)
}
