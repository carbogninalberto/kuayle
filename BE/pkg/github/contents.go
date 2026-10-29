package github

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TreeEntry is one entry of a repository tree.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// ListTree lists one level of the tree named by ref: a branch, commit or tree SHA.
func (c *Client) ListTree(token, owner, repo, ref string) ([]TreeEntry, error) {
	var tree struct {
		Tree []TreeEntry `json:"tree"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", githubAPIBaseURL, owner, repo, escapePath(ref))
	if err := c.doGet(token, endpoint, &tree); err != nil {
		return nil, err
	}
	return tree.Tree, nil
}

// GetFileContent reads a file at ref, keeping at most limit bytes.
func (c *Client) GetFileContent(token, owner, repo, path, ref string, limit int64) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", githubAPIBaseURL, owner, repo, escapePath(path), url.QueryEscape(ref))
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "token "+token)
	request.Header.Set("Accept", "application/vnd.github.raw+json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("GitHub API error %d: %s", response.StatusCode, body)
	}
	return io.ReadAll(io.LimitReader(response.Body, limit))
}

// escapePath escapes each segment of a slash-separated path.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
