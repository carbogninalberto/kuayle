package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestSlackConflictResponse(t *testing.T) {
	for _, tc := range []struct {
		err  error
		body string
	}{
		{repository.ErrSlackTeamInUse, `{"error":{"code":"SLACK_TEAM_IN_USE","message":"This Slack workspace is already connected to another Kuayle workspace."}}`},
		{service.ErrSlackCleanup, `{"error":{"code":"ALREADY_CONNECTED","message":"Another installation finished first. Check the app in the Slack workspace you just authorized and remove it if unused."}}`},
	} {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest("POST", "/", nil), rec)
		require.NoError(t, slackError(c, tc.err))
		require.Equal(t, 409, rec.Code)
		require.JSONEq(t, tc.body, rec.Body.String())
	}
}

func TestSlackEventSignatureAndChallenge(t *testing.T) {
	secret := "test signing key"
	h := NewSlackHandler(service.NewSlackService(nil, config.SlackConfig{SigningSecret: secret}, nil, ""))
	challenge := `{"type":"url_verification","challenge":"verify me"}`
	for _, tc := range []struct {
		name, body, signed string
		offset             time.Duration
		status             int
	}{
		{"valid", challenge, challenge, 0, 200},
		{"changed", challenge, challenge + " ", 0, 401},
		{"stale", challenge, challenge, -6 * time.Minute, 401},
		{"future", challenge, challenge, 6 * time.Minute, 401},
		{"malformed", "{", "{", 0, 400},
		{"irrelevant event", `{"type":"event_callback","team_id":"T","event_id":"E","event":{"type":"message"}}`, `{"type":"event_callback","team_id":"T","event_id":"E","event":{"type":"message"}}`, 0, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := strconv.FormatInt(time.Now().Add(tc.offset).Unix(), 10)
			mac := hmac.New(sha256.New, []byte(secret))
			fmt.Fprintf(mac, "v0:%s:%s", ts, tc.signed)
			req := httptest.NewRequest("POST", "/api/slack/events", strings.NewReader(tc.body))
			req.Header.Set("X-Slack-Request-Timestamp", ts)
			req.Header.Set("X-Slack-Signature", fmt.Sprintf("v0=%x", mac.Sum(nil)))
			rec := httptest.NewRecorder()
			require.NoError(t, h.Events(echo.New().NewContext(req, rec)))
			require.Equal(t, tc.status, rec.Code)
			if tc.name == "valid" {
				require.JSONEq(t, `{"challenge":"verify me"}`, rec.Body.String())
			}
		})
	}
}
