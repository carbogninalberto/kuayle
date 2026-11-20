package github

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListBranchSnapshotReadsTheTreeOfTheResolvedCommit(t *testing.T) {
	head, tree := strings.Repeat("b", 40), strings.Repeat("a", 40)
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		require.Equal(t, http.MethodGet, r.Method)
		switch r.URL.Path {
		case "/repos/acme/app/git/ref/heads/kuayle/screenshots/task-1":
			return 200, `{"object":{"sha":"` + head + `","type":"commit"}}`
		case "/repos/acme/app/git/commits/" + head:
			return 200, `{"tree":{"sha":"` + tree + `"}}`
		case "/repos/acme/app/git/trees/" + tree:
			return 200, `{"tree":[{"path":"demo.png","type":"blob"}]}`
		default:
			return 404, `{}`
		}
	})
	commit, entries, err := c.ListBranchSnapshot("secret", "acme", "app", "kuayle/screenshots/task-1")
	require.NoError(t, err)
	require.Equal(t, head, commit)
	require.Equal(t, []TreeEntry{{Path: "demo.png", Type: "blob"}}, entries)

	commit, entries, err = c.ListBranchSnapshot("secret", "acme", "app", "kuayle/screenshots/missing")
	require.NoError(t, err)
	require.Empty(t, commit)
	require.Empty(t, entries)
}

func TestPublishFilesToBranchKeepsPreviousScreenshotCommits(t *testing.T) {
	previous := strings.Repeat("f", 40)
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "new branch", true: "existing branch"}[exists], func(t *testing.T) {
			var calls []string
			var tree, commit, ref map[string]any
			blobs := 0
			c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
				require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/git/ref/heads/kuayle/screenshots/task-1":
					if exists {
						return 200, `{"object":{"sha":"` + previous + `","type":"commit"}}`
					}
					return 404, `{"message":"Not Found"}`
				case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/git/commits/"+previous:
					return 200, `{"tree":{"sha":"` + strings.Repeat("e", 40) + `"}}`
				case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/git/blobs":
					blobs++
					require.Equal(t, "base64", body["encoding"])
					data, err := base64.StdEncoding.DecodeString(body["content"].(string))
					require.NoError(t, err)
					require.Equal(t, "png-"+string(rune('0'+blobs)), string(data))
					return 201, `{"sha":"` + strings.Repeat(string(rune('0'+blobs)), 40) + `"}`
				case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/git/trees":
					tree = body
					return 201, `{"sha":"` + strings.Repeat("a", 40) + `"}`
				case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/git/commits":
					commit = body
					return 201, `{"sha":"` + strings.Repeat("b", 40) + `"}`
				case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/git/refs":
					ref = body
					return 201, `{}`
				case r.Method == http.MethodPatch && r.URL.EscapedPath() == "/repos/acme/app/git/refs/heads/kuayle/screenshots/task-1":
					require.Equal(t, map[string]any{"sha": strings.Repeat("b", 40), "force": false}, body)
					return 200, `{}`
				}
				return 404, `{"message":"Not Found"}`
			})
			sha, err := c.PublishFilesToBranch("secret", "acme", "app", "kuayle/screenshots/task-1", "REC-1 screenshots", []RepositoryFile{{Path: "a.png", Content: []byte("png-1")}, {Path: "b.png", Content: []byte("png-2")}})
			require.NoError(t, err)
			require.Equal(t, strings.Repeat("b", 40), sha)
			require.Equal(t, []any{
				map[string]any{"path": "a.png", "mode": "100644", "type": "blob", "sha": strings.Repeat("1", 40)},
				map[string]any{"path": "b.png", "mode": "100644", "type": "blob", "sha": strings.Repeat("2", 40)},
			}, tree["tree"])
			require.NotContains(t, tree, "base_tree", "the tree holds only these files")
			require.Equal(t, "REC-1 screenshots", commit["message"])
			require.Equal(t, strings.Repeat("a", 40), commit["tree"])
			if exists {
				require.Equal(t, []any{previous}, commit["parents"], "prior links stay reachable")
				require.Nil(t, ref)
				require.Equal(t, "PATCH /repos/acme/app/git/refs/heads/kuayle/screenshots/task-1", calls[len(calls)-1])
			} else {
				require.Equal(t, []any{}, commit["parents"])
				require.Equal(t, map[string]any{"ref": "refs/heads/kuayle/screenshots/task-1", "sha": strings.Repeat("b", 40)}, ref)
				require.Len(t, calls, 6)
			}
		})
	}
}

func TestPublishFilesToBranchReusesAnIdenticalSnapshot(t *testing.T) {
	head, tree := strings.Repeat("b", 40), strings.Repeat("a", 40)
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		switch r.URL.Path {
		case "/repos/acme/app/git/ref/heads/kuayle/screenshots/task-1":
			return 200, `{"object":{"sha":"` + head + `","type":"commit"}}`
		case "/repos/acme/app/git/blobs":
			return 201, `{"sha":"` + strings.Repeat("c", 40) + `"}`
		case "/repos/acme/app/git/trees":
			return 201, `{"sha":"` + tree + `"}`
		case "/repos/acme/app/git/commits/" + head:
			return 200, `{"tree":{"sha":"` + tree + `"}}`
		default:
			t.Fatalf("unexpected write during publication retry: %s %s", r.Method, r.URL.Path)
			return 500, `{}`
		}
	})
	sha, err := c.PublishFilesToBranch("secret", "acme", "app", "kuayle/screenshots/task-1", "m", []RepositoryFile{{Path: "a.png", Content: []byte("x")}})
	require.NoError(t, err)
	require.Equal(t, head, sha)
}

func TestPublishFilesToBranchRefusesUnexpectedAnswers(t *testing.T) {
	c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
		if r.Method == http.MethodGet {
			return 404, `{"message":"Not Found"}`
		}
		return 201, `{"sha":"../../evil"}`
	})
	_, err := c.PublishFilesToBranch("secret", "acme", "app", "kuayle/screenshots/task-1", "m", []RepositoryFile{{Path: "a.png", Content: []byte("x")}})
	require.Error(t, err)

	_, err = c.PublishFilesToBranch("secret", "acme", "app", "kuayle/screenshots/task-1", "m", nil)
	require.Error(t, err, "nothing to publish")

	forbidden := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
		return 403, `{"message":"Resource not accessible by integration"}`
	})
	_, err = forbidden.PublishFilesToBranch("secret", "acme", "app", "kuayle/screenshots/task-1", "m", []RepositoryFile{{Path: "a.png", Content: []byte("x")}})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 403, apiErr.Status)
}

func TestListTreeReadsOneLevel(t *testing.T) {
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/repos/owner/repo/git/trees/feature/x", r.URL.Path)
		require.Empty(t, r.URL.Query().Get("recursive"))
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		return commentResponse(200, `{"tree":[{"path":"go.mod","type":"blob","sha":"a"},{"path":"UI","type":"tree","sha":"b"}],"truncated":false}`), nil
	})}}
	entries, err := c.ListTree("secret", "owner", "repo", "feature/x")
	require.NoError(t, err)
	require.Equal(t, []TreeEntry{{Path: "go.mod", Type: "blob", SHA: "a"}, {Path: "UI", Type: "tree", SHA: "b"}}, entries)
}

func TestListTreeRejectsTruncatedResults(t *testing.T) {
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(*http.Request) (*http.Response, error) {
		return commentResponse(200, `{"tree":[],"truncated":true}`), nil
	})}}
	_, err := c.ListTree("secret", "owner", "repo", "main")
	require.ErrorContains(t, err, "truncated")
}
