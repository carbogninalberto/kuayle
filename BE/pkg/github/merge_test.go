package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergePullRequestPinsTheReviewedCommit(t *testing.T) {
	var sent map[string]string
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/repos/owner/repo/pulls/7/merge", r.URL.Path)
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		return commentResponse(200, `{"merged":true}`), nil
	})}}
	require.NoError(t, c.MergePullRequest("secret", "owner", "repo", 7, "abc"))
	require.Equal(t, map[string]string{"sha": "abc", "merge_method": "squash"}, sent)
}

func TestMergePullRequestReportsGitHubsReason(t *testing.T) {
	for _, code := range []int{405, 409} {
		c := &Client{httpClient: &http.Client{Transport: commentTransport(func(*http.Request) (*http.Response, error) {
			return commentResponse(code, `{"message":"Head branch was modified. Review and try the merge again."}`), nil
		})}}
		err := c.MergePullRequest("secret", "owner", "repo", 7, "abc")
		var refused *ErrMergeRefused
		require.True(t, errors.As(err, &refused), code)
		require.Equal(t, "Head branch was modified. Review and try the merge again.", refused.Reason)
	}
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(*http.Request) (*http.Response, error) {
		return commentResponse(500, `{}`), nil
	})}}
	var refused *ErrMergeRefused
	require.False(t, errors.As(c.MergePullRequest("secret", "owner", "repo", 7, "abc"), &refused))
}
