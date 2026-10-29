package github

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListTreeReadsOneLevel(t *testing.T) {
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/repos/owner/repo/git/trees/feature/x", r.URL.Path)
		require.Empty(t, r.URL.Query().Get("recursive"))
		require.Equal(t, "token secret", r.Header.Get("Authorization"))
		return commentResponse(200, `{"tree":[{"path":"go.mod","type":"blob","sha":"a"},{"path":"UI","type":"tree","sha":"b"}],"truncated":false}`), nil
	})}}
	entries, err := c.ListTree("secret", "owner", "repo", "feature/x")
	require.NoError(t, err)
	require.Equal(t, []TreeEntry{{Path: "go.mod", Type: "blob", SHA: "a"}, {Path: "UI", Type: "tree", SHA: "b"}}, entries)
}

func TestGetFileContentIsRawAndCapped(t *testing.T) {
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/repos/owner/repo/contents/UI/package.json", r.URL.Path)
		require.Equal(t, "main", r.URL.Query().Get("ref"))
		require.Equal(t, "application/vnd.github.raw+json", r.Header.Get("Accept"))
		return commentResponse(200, `{"scripts":{}}`+strings.Repeat(" ", 100)), nil
	})}}
	content, err := c.GetFileContent("secret", "owner", "repo", "UI/package.json", "main", 14)
	require.NoError(t, err)
	require.Equal(t, `{"scripts":{}}`, string(content))

	missing := &Client{httpClient: &http.Client{Transport: commentTransport(func(*http.Request) (*http.Response, error) {
		return commentResponse(404, `{"message":"Not Found"}`), nil
	})}}
	_, err = missing.GetFileContent("secret", "owner", "repo", "x", "main", 10)
	require.Error(t, err)
}
