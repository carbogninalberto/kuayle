package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// treeResponseMax bounds a tree answer read from GitHub: a recursive tree
// holds up to 100,000 entries of about 250 bytes each.
const treeResponseMax = 32 << 20

// TreeNode is one entry of a git tree as GitHub's trees API lists it: the
// name (or, in a recursive listing, the path), its file mode and type
// (blob, tree or commit, a submodule), its SHA and a blob's size.
type TreeNode struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size,omitempty"`
}

// TreeListing is one git tree. Truncated: GitHub cut the listing at its own
// limit (100,000 entries or 7 MB) and later entries are missing.
type TreeListing struct {
	SHA       string     `json:"sha"`
	Tree      []TreeNode `json:"tree"`
	Truncated bool       `json:"truncated"`
}

// GetTree reads the git tree sha (the entries of one directory, or with
// recursive every path below it) through the trees API. A tree's SHA names
// its content, so the answer never changes. A partial listing is returned
// as such, marked Truncated.
func (c *Client) GetTree(ctx context.Context, token, owner, repo, sha string, recursive bool) (*TreeListing, error) {
	if !gitObjectSHA.MatchString(sha) {
		return nil, errors.New("invalid tree SHA")
	}
	endpoint := gitDataURL(owner, repo) + "/trees/" + sha
	if recursive {
		endpoint += "?recursive=1"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &failure)
		return nil, &APIError{Status: response.StatusCode, Message: failure.Message}
	}
	var listing TreeListing
	limited := &io.LimitedReader{R: response.Body, N: treeResponseMax + 1}
	if err := json.NewDecoder(limited).Decode(&listing); err != nil {
		if limited.N <= 0 {
			return nil, fmt.Errorf("the GitHub tree at %s is too large", sha)
		}
		return nil, err
	}
	if listing.SHA != sha {
		return nil, errors.New("GitHub returned an unexpected tree")
	}
	return &listing, nil
}

// GetCommitTree returns the SHA of the root tree of a commit.
func (c *Client) GetCommitTree(ctx context.Context, token, owner, repo, commit string) (string, error) {
	if !gitObjectSHA.MatchString(commit) {
		return "", errors.New("invalid commit SHA")
	}
	var result struct {
		SHA  string    `json:"sha"`
		Tree CommitRef `json:"tree"`
	}
	if err := c.doContext(ctx, http.MethodGet, gitDataURL(owner, repo)+"/commits/"+commit, token, nil, &result); err != nil {
		return "", err
	}
	if result.SHA != commit || !gitObjectSHA.MatchString(result.Tree.SHA) {
		return "", errors.New("GitHub returned an unexpected commit tree")
	}
	return result.Tree.SHA, nil
}
