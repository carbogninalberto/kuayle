package github

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// RepositoryFile is one file of a commit made through the Git data API.
type RepositoryFile struct {
	Path    string
	Content []byte
}

var gitObjectSHA = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// PublishFilesToBranch commits files as the whole tree of a commit without
// parents, so it shares no history with the repository's code, and points
// branch at it: the branch is created, or moved to the new commit when it
// already exists. It returns the commit's SHA.
func (c *Client) PublishFilesToBranch(token, owner, repo, branch, message string, files []RepositoryFile) (string, error) {
	if len(files) == 0 {
		return "", errors.New("no files to publish")
	}
	base := fmt.Sprintf("%s/repos/%s/%s/git", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo))
	// create posts one object and returns the SHA GitHub answered with.
	create := func(kind string, body any) (string, error) {
		var created struct {
			SHA string `json:"sha"`
		}
		if err := c.do(http.MethodPost, base+"/"+kind, token, body, &created); err != nil {
			return "", err
		}
		if !gitObjectSHA.MatchString(created.SHA) {
			return "", fmt.Errorf("GitHub returned an unexpected %s answer", kind)
		}
		return created.SHA, nil
	}
	entries := make([]map[string]string, 0, len(files))
	for _, file := range files {
		blob, err := create("blobs", map[string]string{"content": base64.StdEncoding.EncodeToString(file.Content), "encoding": "base64"})
		if err != nil {
			return "", err
		}
		entries = append(entries, map[string]string{"path": file.Path, "mode": "100644", "type": "blob", "sha": blob})
	}
	tree, err := create("trees", map[string]any{"tree": entries})
	if err != nil {
		return "", err
	}
	commit, err := create("commits", map[string]any{"message": message, "tree": tree, "parents": []string{}})
	if err != nil {
		return "", err
	}
	err = c.do(http.MethodPost, base+"/refs", token, map[string]string{"ref": "refs/heads/" + branch, "sha": commit}, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
		// The branch exists: move it to this commit, which shares no
		// history with its previous one.
		err = c.do(http.MethodPatch, base+"/refs/heads/"+escapePath(branch), token, map[string]any{"sha": commit, "force": true}, nil)
	}
	if err != nil {
		return "", err
	}
	return commit, nil
}

// escapePath escapes each segment of a slash-separated path.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
