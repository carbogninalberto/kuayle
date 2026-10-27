package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTelegramWebhookURLDefaultsToTheAppOriginAndAcceptsARelay(t *testing.T) {
	ws := uuid.New()
	svc := NewTelegramService(nil, nil, nil, nil, "https://kuayle.example.test/")
	require.Equal(t, "https://kuayle.example.test/api/telegram/webhook/"+ws.String(), svc.webhookURL(ws))
	// A development relay receives Telegram's updates at its own fixed URL.
	require.NoError(t, svc.SetWebhookURL("https://smee.io/fixture-channel"))
	require.Equal(t, "https://smee.io/fixture-channel", svc.webhookURL(ws))
	for _, invalid := range []string{"http://smee.io/x", "smee.io/x", "https://"} {
		require.Error(t, svc.SetWebhookURL(invalid), invalid)
	}
	require.NoError(t, svc.SetWebhookURL(""))
	require.Equal(t, "https://kuayle.example.test/api/telegram/webhook/"+ws.String(), svc.webhookURL(ws))
}
