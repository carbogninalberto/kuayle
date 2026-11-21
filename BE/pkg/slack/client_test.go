package slack

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBotOAuthAndChannelPages(t *testing.T) {
	ctx := context.Background()
	c := NewClient("test-client", "test-secret", "https://kuayle.test/integrations/slack/callback")
	u, err := url.Parse(c.InstallURL("nonce"))
	require.NoError(t, err)
	require.Equal(t, "slack.com", u.Host)
	require.Equal(t, "nonce", u.Query().Get("state"))
	require.Equal(t, Scope, u.Query().Get("scope"))
	require.Empty(t, u.Query().Get("client_secret"))
	token := "test-bot-token"
	calls := []string{}
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "https", r.URL.Scheme)
		require.Equal(t, "slack.com", r.URL.Host)
		require.NoError(t, r.ParseForm())
		calls = append(calls, r.URL.Path)
		var body string
		switch r.URL.Path {
		case "/api/oauth.v2.access":
			require.Equal(t, "test-code", r.Form.Get("code"))
			require.Equal(t, u.Query().Get("redirect_uri"), r.Form.Get("redirect_uri"))
			require.Equal(t, "test-secret", r.Form.Get("client_secret"))
			body = `{"ok":true,"access_token":"test-bot-token","token_type":"bot","scope":"channels:read,groups:read","team":{"id":"T1","name":"Workspace"},"authed_user":{"id":"U1"}}`
		case "/api/users.conversations":
			require.Equal(t, "true", r.Form.Get("exclude_archived"))
			require.Equal(t, "public_channel,private_channel", r.Form.Get("types"))
			require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
			if r.Form.Get("cursor") == "" {
				body = `{"ok":true,"channels":[],"response_metadata":{"next_cursor":"next"}}`
			} else {
				require.Equal(t, "next", r.Form.Get("cursor"))
				body = `{"ok":true,"channels":[{"id":"C1","name":"releases"}]}`
			}
		case "/api/apps.uninstall":
			require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
			require.Equal(t, "test-secret", r.Form.Get("client_secret"))
			body = `{"ok":true}`
		default:
			t.Fatalf("unexpected Slack method %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	auth, err := c.Exchange(ctx, "test-code")
	require.NoError(t, err)
	require.Equal(t, token, auth.Token)
	require.Equal(t, "U1", auth.User.ID)
	require.Equal(t, "T1", auth.Team.ID)
	channels, err := c.Channels(ctx, auth.Token)
	require.NoError(t, err)
	require.Equal(t, []Channel{{ID: "C1", Name: "releases"}}, channels)
	require.NoError(t, c.Uninstall(ctx, auth.Token))
	require.Equal(t, []string{"/api/oauth.v2.access", "/api/users.conversations", "/api/users.conversations", "/api/apps.uninstall"}, calls)
}

func TestSlackAPIFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"expired", 200, `{"ok":false,"error":"token_revoked"}`, ErrRevoked},
		{"rate limit", 429, "", ErrRateLimited}, {"provider error", 503, "", ErrAPI},
		{"user token", 200, `{"ok":true,"token_type":"user","access_token":"sensitive"}`, ErrAPI},
		{"rotating token", 200, `{"ok":true,"token_type":"bot","access_token":"sensitive","refresh_token":"sensitive","team":{"id":"T1"},"authed_user":{"id":"U1"}}`, ErrAPI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient("", "", "")
			c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			_, err := c.Exchange(context.Background(), "test-code")
			require.ErrorIs(t, err, tc.want)
			require.NotContains(t, err.Error(), "sensitive")
		})
	}
}
