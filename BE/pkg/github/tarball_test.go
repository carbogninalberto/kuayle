package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepositoryTarballFollowsOnlyCodeloadWithoutTheToken(t *testing.T) {
	var hosts []string
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Host)
		switch r.URL.Host {
		case "api.github.com":
			require.Equal(t, "/repos/acme/app/tarball/0123abc", r.URL.Path)
			require.Equal(t, "token secret", r.Header.Get("Authorization"))
			response := commentResponse(http.StatusFound, "")
			response.Header.Set("Location", "https://codeload.github.com/acme/app/legacy.tar.gz/0123abc?token=short")
			return response, nil
		case "codeload.github.com":
			require.Empty(t, r.Header.Get("Authorization"), "the installation token never leaves api.github.com")
			return commentResponse(http.StatusOK, "tarball"), nil
		}
		t.Fatalf("unexpected host %s", r.URL.Host)
		return nil, nil
	})}}
	body, err := c.RepositoryTarball(context.Background(), "secret", "acme", "app", "0123abc")
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	require.NoError(t, body.Close())
	require.Equal(t, "tarball", string(data))
	require.Equal(t, []string{"api.github.com", "codeload.github.com"}, hosts)
}

func TestRepositoryTarballRefusesRedirectsElsewhere(t *testing.T) {
	for _, location := range []string{"http://codeload.github.com/x", "https://169.254.169.254/latest", "https://codeload.github.com.evil.test/x", "/relative"} {
		c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "api.github.com" {
				t.Fatalf("followed a redirect to %s", r.URL)
			}
			response := commentResponse(http.StatusFound, "")
			response.Header.Set("Location", location)
			return response, nil
		})}}
		_, err := c.RepositoryTarball(context.Background(), "secret", "acme", "app", "main")
		require.Error(t, err, location)
	}
	missing := &Client{httpClient: &http.Client{Transport: commentTransport(func(*http.Request) (*http.Response, error) {
		return commentResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
	})}}
	_, err := missing.RepositoryTarball(context.Background(), "secret", "acme", "app", "main")
	var api *APIError
	require.ErrorAs(t, err, &api)
	require.Equal(t, http.StatusNotFound, api.Status)
}

func TestReadOnlyRepositoryTokenAsksOnlyToRead(t *testing.T) {
	c, _ := appClient(t, func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/app/installations/99/access_tokens", r.URL.Path)
		var body struct {
			Repositories []string          `json:"repositories"`
			Permissions  map[string]string `json:"permissions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, []string{"app"}, body.Repositories)
		require.Equal(t, map[string]string{"contents": "read", "metadata": "read"}, body.Permissions)
		return commentResponse(http.StatusCreated, `{"token":"ghs_read","expires_at":"2030-01-01T00:00:00Z"}`), nil
	})
	token, _, err := c.GetReadOnlyRepositoryInstallationToken(context.Background(), 99, "app")
	require.NoError(t, err)
	require.Equal(t, "ghs_read", token)
}
