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

	withImages := guidanceMessages(true)
	require.Len(t, withImages, 2)
	require.Equal(t, "system", withImages[0].Role)
	require.NotEmpty(t, withImages[0].Content)
	require.NotEqual(t, richBlocksPrompt, withImages[0].Content)
	require.Equal(t, "system", withImages[1].Role)
	require.Equal(t, richBlocksPrompt, withImages[1].Content)
}
