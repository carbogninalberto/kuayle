package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestTelegramErrorsCarryTelegramsReasonWithoutTheToken(t *testing.T) {
	token := "123456789:" + strings.Repeat("A", 35)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"ok":false,"error_code":401,"description":"Unauthorized: `+token+`\n"}`)
	}))
	defer remote.Close()
	svc := NewTelegramService(nil, nil, nil, nil, "https://kuayle.example.test")
	svc.endpoint, svc.client = remote.URL, remote.Client()
	err := svc.call(context.Background(), token, "getMe", struct{}{}, nil)
	require.ErrorIs(t, err, ErrTelegramAPI)
	var refused *TelegramAPIError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, "Telegram getMe failed: Unauthorized: [redacted]", refused.Error())
	require.NotContains(t, err.Error(), token)
}

// Enabling checks the address Telegram will call: a relay may serve a local
// HTTP installation, which alone cannot receive webhooks.
func TestTelegramWebhookAddressMustBePublicHTTPS(t *testing.T) {
	ws := uuid.New()
	local := NewTelegramService(nil, nil, nil, nil, "http://localhost:5173")
	require.False(t, publicHTTPSURL(local.webhookURL(ws)))
	require.NoError(t, local.SetWebhookURL("https://smee.io/fixture-channel"))
	require.True(t, publicHTTPSURL(local.webhookURL(ws)))
	require.False(t, publicHTTPSURL("https://user@example.test/hook"))
}
