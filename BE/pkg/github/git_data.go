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

// TreeEntry is one entry in a branch snapshot's root tree.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

var gitObjectSHA = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

func gitDataURL(owner, repo string) string {
	return fmt.Sprintf("%s/repos/%s/%s/git", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo))
}

// branchHead returns the branch's commit SHA, or an empty SHA if absent.
func (c *Client) branchHead(token, owner, repo, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	endpoint := gitDataURL(owner, repo) + "/ref/heads/" + escapePath(branch)
	if err := c.do(http.MethodGet, endpoint, token, nil, &ref); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	if ref.Object.Type != "commit" || !gitObjectSHA.MatchString(ref.Object.SHA) {
		return "", errors.New("GitHub returned an unexpected branch head")
	}
	return ref.Object.SHA, nil
}

func (c *Client) commitTree(token, owner, repo, commit string) (string, error) {
	var result struct {
		Tree CommitRef `json:"tree"`
	}
	if err := c.do(http.MethodGet, gitDataURL(owner, repo)+"/commits/"+commit, token, nil, &result); err != nil {
		return "", err
	}
	if !gitObjectSHA.MatchString(result.Tree.SHA) {
		return "", errors.New("GitHub returned an unexpected commit tree")
	}
	return result.Tree.SHA, nil
}

// ListBranchSnapshot returns the branch's current commit and its root tree.
// The tree is read by SHA so a later branch update cannot change the snapshot.
func (c *Client) ListBranchSnapshot(token, owner, repo, branch string) (string, []TreeEntry, error) {
	head, err := c.branchHead(token, owner, repo, branch)
	if err != nil || head == "" {
		return "", nil, err
	}
	tree, err := c.commitTree(token, owner, repo, head)
	if err != nil {
		return "", nil, err
	}
	entries, err := c.ListTree(token, owner, repo, tree)
	return head, entries, err
}

// ListTree reads one directory at the given revision, rejecting a partial listing.
func (c *Client) ListTree(token, owner, repo, ref string) ([]TreeEntry, error) {
	var result struct {
		Tree      []TreeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := c.do(http.MethodGet, gitDataURL(owner, repo)+"/trees/"+escapePath(ref), token, nil, &result); err != nil {
		return nil, err
	}
	if result.Truncated {
		return nil, fmt.Errorf("the GitHub tree at %s was truncated", ref)
	}
	return result.Tree, nil
}

// PublishFilesToBranch commits files as the whole tree of a branch with its
// own history. Existing snapshots stay reachable for commit-pinned links.
func (c *Client) PublishFilesToBranch(token, owner, repo, branch, message string, files []RepositoryFile) (string, error) {
	if len(files) == 0 {
		return "", errors.New("no files to publish")
	}
	head, err := c.branchHead(token, owner, repo, branch)
	if err != nil {
		return "", err
	}
	base := gitDataURL(owner, repo)
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
	parents := []string{}
	if head != "" {
		previousTree, err := c.commitTree(token, owner, repo, head)
		if err != nil {
			return "", err
		}
		if previousTree == tree {
			return head, nil
		}
		parents = append(parents, head)
	}
	commit, err := create("commits", map[string]any{"message": message, "tree": tree, "parents": parents})
	if err != nil {
		return "", err
	}
	if head == "" {
		err = c.do(http.MethodPost, base+"/refs", token, map[string]string{"ref": "refs/heads/" + branch, "sha": commit}, nil)
	} else {
		err = c.do(http.MethodPatch, base+"/refs/heads/"+escapePath(branch), token, map[string]any{"sha": commit, "force": false}, nil)
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
