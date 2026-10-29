package github

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreatePullRequestCanOpenADraft(t *testing.T) {
	var sent map[string]any
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/repos/owner/repo/pulls", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		return commentResponse(201, `{"number":4,"draft":true,"html_url":"https://github.com/owner/repo/pull/4"}`), nil
	})}}
	pr, err := c.CreatePullRequest("secret", "owner", "repo", "Title", "feature", "main", "Body", true)
	require.NoError(t, err)
	require.True(t, pr.Draft)
	require.Equal(t, true, sent["draft"])
}

func TestUpdatePullRequestBodyChangesOnlyTheDescription(t *testing.T) {
	var sent map[string]any
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPatch, r.Method)
		require.Equal(t, "/repos/owner/repo/pulls/4", r.URL.Path)
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		return commentResponse(200, `{}`), nil
	})}}
	require.NoError(t, c.UpdatePullRequestBody("secret", "owner", "repo", 4, "New body"))
	require.Equal(t, map[string]any{"body": "New body"}, sent)
}
