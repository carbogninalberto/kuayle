package service

import (
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Web replies learn the two rich fences the chat renders, so a request for a
// chart or a widget gets a block the UI can draw; Telegram shows them as raw
// code, so its guidance keeps only the attachment note shared by every channel.
func TestGuidanceMessagesMatchTheChannel(t *testing.T) {
	tests := []struct {
		name    string
		images  bool
		channel string
	}{
		{"web", false, domain.ConversationChannelWeb},
		{"web with images", true, domain.ConversationChannelWeb},
		{"telegram", false, domain.ConversationChannelTelegram},
		{"telegram with images", true, domain.ConversationChannelTelegram},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var rich, attachments bool
			for _, message := range guidanceMessages(test.images, test.channel) {
				require.Equal(t, "system", message.Role)
				if strings.Contains(message.Content, "```chart") {
					rich = true
					require.Contains(t, message.Content, "```html")
					require.Contains(t, message.Content, "for a pie at most 12 categories")
				}
				if strings.Contains(message.Content, "Users can attach images") {
					attachments = true
				}
			}
			require.Equal(t, test.channel == domain.ConversationChannelWeb, rich, "rich blocks are web-only")
			require.Equal(t, test.images, attachments, "the attachment note follows the images setting, not the channel")
		})
	}
}
