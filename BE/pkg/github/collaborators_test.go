package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetCollaboratorPermission(t *testing.T) {
	answer := func(code int, body string) *Client {
		return &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "api.github.com", r.URL.Host)
			require.Equal(t, "/repos/acme/app/collaborators/octo%2F..%2Fadmin/permission", r.URL.EscapedPath(), "the login cannot change the path")
			require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			return commentResponse(code, body), nil
		})}}
	}
	ctx := context.Background()
	get := func(c *Client) (string, error) {
		return c.GetCollaboratorPermission(ctx, "tok", "acme", "app", "octo/../admin")
	}

	permission, err := get(answer(200, `{"permission":"write","role_name":"maintain"}`))
	require.NoError(t, err)
	require.Equal(t, "write", permission)

	permission, err = get(answer(404, `{"message":"Not Found"}`))
	require.NoError(t, err)
	require.Equal(t, PermissionNone, permission, "an unknown user has no access")

	for _, c := range []*Client{answer(403, `{"message":"private detail"}`), answer(500, `oops`), answer(200, `{}`), answer(200, `not json`)} {
		permission, err = get(c)
		require.Error(t, err)
		require.Empty(t, permission, "a failed lookup grants nothing")
		require.NotContains(t, err.Error(), "private detail")
	}
	var apiErr *APIError
	_, err = get(answer(403, `{}`))
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 403, apiErr.Status)

	failing := &Client{httpClient: &http.Client{Transport: commentTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp 10.0.0.1: refused")
	})}}
	_, err = get(failing)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "10.0.0.1"))

	_, err = failing.GetCollaboratorPermission(ctx, "tok", "acme", "app", "")
	require.Error(t, err)
}
