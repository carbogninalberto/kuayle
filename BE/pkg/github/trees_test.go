package github

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// GetTree reads one tree by SHA through the trees API with the token as a
// bearer, keeps each entry's mode, type and size, and returns a partial
// listing marked truncated rather than as a failure.
func TestGetTreeReadsOneDirectoryOrTheWholeTree(t *testing.T) {
	tree := strings.Repeat("a", 40)
	var queries []string
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.Equal(t, "/repos/acme/app/git/trees/"+tree, r.URL.Path)
		queries = append(queries, r.URL.RawQuery)
		return 200, `{"sha":"` + tree + `","tree":[
			{"path":"src","mode":"040000","type":"tree","sha":"` + strings.Repeat("b", 40) + `"},
			{"path":"main.go","mode":"100755","type":"blob","sha":"` + strings.Repeat("c", 40) + `","size":120}],"truncated":true}`
	})
	listing, err := c.GetTree(context.Background(), "secret", "acme", "app", tree, false)
	require.NoError(t, err)
	require.True(t, listing.Truncated)
	require.Equal(t, []TreeNode{
		{Path: "src", Mode: "040000", Type: "tree", SHA: strings.Repeat("b", 40)},
		{Path: "main.go", Mode: "100755", Type: "blob", SHA: strings.Repeat("c", 40), Size: ptr(int64(120))},
	}, listing.Tree)
	_, err = c.GetTree(context.Background(), "secret", "acme", "app", tree, true)
	require.NoError(t, err)
	require.Equal(t, []string{"", "recursive=1"}, queries)
}

func TestGetTreeRefusesWhatIsNotAnObjectIDOrAnswerGitHubDidNotGive(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, strings.Repeat("a", 40)):
			return 404, `{"message":"Not Found"}`
		case strings.HasSuffix(r.URL.Path, strings.Repeat("b", 40)):
			return 200, `{"sha":"` + strings.Repeat("c", 40) + `","tree":[]}`
		}
		return 500, `{}`
	})
	for _, sha := range []string{"main", "../x", strings.Repeat("A", 40), "abc123", ""} {
		_, err := c.GetTree(context.Background(), "secret", "acme", "app", sha, false)
		require.Error(t, err, sha)
	}
	_, err := c.GetTree(context.Background(), "secret", "acme", "app", strings.Repeat("a", 40), false)
	var api *APIError
	require.ErrorAs(t, err, &api)
	require.Equal(t, 404, api.Status)
	_, err = c.GetTree(context.Background(), "secret", "acme", "app", strings.Repeat("b", 40), false)
	require.Error(t, err, "a tree other than the one asked for")
}

func TestGetCommitTreeReturnsTheRootTreeOfThatCommit(t *testing.T) {
	commit, tree := strings.Repeat("1", 40), strings.Repeat("2", 40)
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		require.Equal(t, "/repos/acme/app/git/commits/"+commit, r.URL.Path)
		return 200, `{"sha":"` + commit + `","tree":{"sha":"` + tree + `"}}`
	})
	got, err := c.GetCommitTree(context.Background(), "secret", "acme", "app", commit)
	require.NoError(t, err)
	require.Equal(t, tree, got)
	_, err = c.GetCommitTree(context.Background(), "secret", "acme", "app", "main")
	require.Error(t, err)
}

func ptr[T any](value T) *T { return &value }
