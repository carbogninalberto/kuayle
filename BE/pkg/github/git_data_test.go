package github

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishFilesToBranchCommitsOnlyTheFilesWithoutParents(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "new branch", true: "existing branch"}[exists], func(t *testing.T) {
			var calls []string
			var tree, commit, ref map[string]any
			blobs := 0
			c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
				require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				switch {
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
					if exists {
						return 422, `{"message":"Reference already exists"}`
					}
					return 201, `{}`
				case r.Method == http.MethodPatch && r.URL.EscapedPath() == "/repos/acme/app/git/refs/heads/kuayle/screenshots/task-1":
					require.Equal(t, map[string]any{"sha": strings.Repeat("b", 40), "force": true}, body)
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
			require.Equal(t, map[string]any{"message": "REC-1 screenshots", "tree": strings.Repeat("a", 40), "parents": []any{}}, commit)
			require.Equal(t, map[string]any{"ref": "refs/heads/kuayle/screenshots/task-1", "sha": strings.Repeat("b", 40)}, ref)
			if exists {
				require.Equal(t, "PATCH /repos/acme/app/git/refs/heads/kuayle/screenshots/task-1", calls[len(calls)-1], "an existing branch is moved to the new commit")
			} else {
				require.Len(t, calls, 5)
			}
		})
	}
}

func TestPublishFilesToBranchRefusesUnexpectedAnswers(t *testing.T) {
	c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
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
