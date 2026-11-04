package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func appClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) (*Client, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return &Client{appID: 42, privateKey: key, httpClient: &http.Client{Transport: commentTransport(handler)}}, key
}

// requireAppJWT asserts the request authenticates as the app itself, which
// is the only credential GitHub accepts for /app and /app/installations/{id}.
func requireAppJWT(t *testing.T, r *http.Request, key *rsa.PrivateKey) {
	t.Helper()
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	require.True(t, ok, "app endpoints need a bearer app JWT")
	claims := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
	require.NoError(t, err)
	require.Equal(t, "42", claims.Issuer)
}

func TestGetAppReadsConfiguredEventsAndPermissions(t *testing.T) {
	var key *rsa.PrivateKey
	c, key := appClient(t, func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "api.github.com", r.URL.Host)
		require.Equal(t, "/app", r.URL.Path)
		requireAppJWT(t, r, key)
		return commentResponse(200, `{"id":42,"slug":"kuayle-acme","owner":{"login":"acme","type":"Organization"},
			"events":["pull_request","push"],"permissions":{"pull_requests":"write","metadata":"read"}}`), nil
	})

	app, err := c.GetApp(context.Background())
	require.NoError(t, err)
	require.Equal(t, "kuayle-acme", app.Slug)
	require.Equal(t, "acme", app.Owner.Login)
	require.Equal(t, "Organization", app.Owner.Type)
	require.Equal(t, []string{"pull_request", "push"}, app.Events)
	require.Equal(t, map[string]string{"pull_requests": "write", "metadata": "read"}, app.Permissions)
}

func TestGetInstallationReadsAcceptedEventsAndPermissions(t *testing.T) {
	var key *rsa.PrivateKey
	c, key := appClient(t, func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/app/installations/77", r.URL.Path)
		requireAppJWT(t, r, key)
		return commentResponse(200, `{"id":77,"account":{"login":"acme","type":"Organization"},
			"html_url":"https://github.com/organizations/acme/settings/installations/77",
			"events":["push"],"permissions":{"metadata":"read"}}`), nil
	})

	inst, err := c.GetInstallationContext(context.Background(), 77)
	require.NoError(t, err)
	require.EqualValues(t, 77, inst.ID)
	require.Equal(t, "https://github.com/organizations/acme/settings/installations/77", inst.HTMLURL)
	require.Equal(t, []string{"push"}, inst.Events)
	require.Equal(t, map[string]string{"metadata": "read"}, inst.Permissions)
}

func TestAppEndpointsReportGitHubFailures(t *testing.T) {
	c, _ := appClient(t, func(*http.Request) (*http.Response, error) {
		return commentResponse(401, `{"message":"A JSON web token could not be decoded"}`), nil
	})
	_, err := c.GetApp(context.Background())
	require.ErrorContains(t, err, "401")
	_, err = c.GetInstallationContext(context.Background(), 77)
	require.ErrorContains(t, err, "401")
	require.NotErrorIs(t, err, ErrNotFound)
}

// An installation removed on GitHub answers 404, which callers must be able
// to tell apart from GitHub being unreachable.
func TestGetInstallationReportsUninstalledAndSuspended(t *testing.T) {
	c, _ := appClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/app/installations/1" {
			return commentResponse(404, `{"message":"Not Found"}`), nil
		}
		return commentResponse(200, `{"id":2,"suspended_at":"2026-01-02T03:04:05Z"}`), nil
	})
	_, err := c.GetInstallationContext(context.Background(), 1)
	require.ErrorIs(t, err, ErrNotFound)
	inst, err := c.GetInstallationContext(context.Background(), 2)
	require.NoError(t, err)
	require.NotNil(t, inst.SuspendedAt)
}
