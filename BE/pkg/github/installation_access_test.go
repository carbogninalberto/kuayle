package github

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func installationAccessTransport(t *testing.T, installationPages [][]int64) http.RoundTripper {
	return commentTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "github.com/login/oauth/access_token":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "client-id", r.PostForm.Get("client_id"))
			require.Equal(t, "client-secret", r.PostForm.Get("client_secret"))
			if r.PostForm.Get("code") != "good-code" {
				return commentResponse(200, `{"error":"bad_verification_code"}`), nil
			}
			return commentResponse(200, `{"access_token":"user-token","token_type":"bearer"}`), nil
		case "api.github.com/user/installations":
			require.Equal(t, "Bearer user-token", r.Header.Get("Authorization"))
			page := 1
			if p := r.URL.Query().Get("page"); p == "2" {
				page = 2
			}
			items := []map[string]any{}
			if page <= len(installationPages) {
				for _, id := range installationPages[page-1] {
					items = append(items, map[string]any{"id": id})
				}
			}
			body, _ := json.Marshal(map[string]any{"total_count": 0, "installations": items})
			return commentResponse(200, string(body)), nil
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		return nil, nil
	})
}

// An installation_id in the setup redirect is caller-controlled. Only the
// installing user's OAuth grant proves which installation the flow created.
func TestUserCanAccessInstallation(t *testing.T) {
	full := make([]int64, 100)
	for i := range full {
		full[i] = int64(1000 + i)
	}
	client := &http.Client{Transport: installationAccessTransport(t, [][]int64{full, {77}})}
	ctx := context.Background()

	ok, err := UserCanAccessInstallation(ctx, client, "client-id", "client-secret", "good-code", 77)
	require.NoError(t, err)
	require.True(t, ok, "installations on later pages must be found")

	ok, err = UserCanAccessInstallation(ctx, client, "client-id", "client-secret", "good-code", 42)
	require.NoError(t, err)
	require.False(t, ok, "another account's installation must be refused")

	_, err = UserCanAccessInstallation(ctx, client, "client-id", "client-secret", "stolen-code", 77)
	require.Error(t, err)
}
