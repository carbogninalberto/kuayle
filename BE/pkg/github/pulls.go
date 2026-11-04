package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// APIError is a GitHub response the caller can act on: 403 means the App lacks
// a permission, 404 a missing resource, 409/422 a state GitHub refuses.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API error %d: %s", e.Status, e.Message)
}

// do sends one authenticated request and decodes a JSON answer into result
// when it is not nil. Any status outside 2xx is an *APIError.
func (c *Client) do(method, endpoint, token string, body, result any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &failure)
		return &APIError{Status: response.StatusCode, Message: failure.Message}
	}
	if result == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, result)
}

// graphQL runs one query or mutation. GraphQL reports failures with HTTP 200
// and an errors list.
func (c *Client) graphQL(token, query string, variables map[string]any, result any) error {
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.do(http.MethodPost, githubAPIBaseURL+"/graphql", token, map[string]any{"query": query, "variables": variables}, &answer); err != nil {
		return err
	}
	if len(answer.Errors) > 0 {
		return &APIError{Status: http.StatusUnprocessableEntity, Message: answer.Errors[0].Message}
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(answer.Data, result)
}

// RepositoryMergeSettings are the merge methods a repository allows.
type RepositoryMergeSettings struct {
	AllowSquashMerge bool `json:"allow_squash_merge"`
	AllowMergeCommit bool `json:"allow_merge_commit"`
	AllowRebaseMerge bool `json:"allow_rebase_merge"`
}

// Allows reports whether the repository permits a merge method.
func (s RepositoryMergeSettings) Allows(method string) bool {
	switch method {
	case "squash":
		return s.AllowSquashMerge
	case "merge":
		return s.AllowMergeCommit
	case "rebase":
		return s.AllowRebaseMerge
	}
	return false
}

// GetRepositoryMergeSettings reads which merge methods the repository allows.
func (c *Client) GetRepositoryMergeSettings(token, owner, repo string) (*RepositoryMergeSettings, error) {
	var settings RepositoryMergeSettings
	if err := c.do(http.MethodGet, fmt.Sprintf("%s/repos/%s/%s", githubAPIBaseURL, owner, repo), token, nil, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// CheckRun is one CI check of a commit.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	DetailsURL string `json:"details_url"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

// ListCheckRuns lists the check runs of a commit.
func (c *Client) ListCheckRuns(token, owner, repo, sha string) ([]CheckRun, error) {
	var page struct {
		CheckRuns []CheckRun `json:"check_runs"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs?per_page=100", githubAPIBaseURL, owner, repo, url.PathEscape(sha))
	if err := c.do(http.MethodGet, endpoint, token, nil, &page); err != nil {
		return nil, err
	}
	return page.CheckRuns, nil
}

// CommitStatus is one legacy commit status (for CI that does not use checks).
type CommitStatus struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
}

// ListCommitStatuses lists the latest status of every context of a commit.
func (c *Client) ListCommitStatuses(token, owner, repo, sha string) ([]CommitStatus, error) {
	var combined struct {
		Statuses []CommitStatus `json:"statuses"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s/status?per_page=100", githubAPIBaseURL, owner, repo, url.PathEscape(sha))
	if err := c.do(http.MethodGet, endpoint, token, nil, &combined); err != nil {
		return nil, err
	}
	return combined.Statuses, nil
}

// ReviewComment is one inline review comment of a pull request.
type ReviewComment struct {
	ID          int64      `json:"id"`
	InReplyToID int64      `json:"in_reply_to_id"`
	User        ReviewUser `json:"user"`
	Body        string     `json:"body"`
	Path        string     `json:"path"`
	Line        int        `json:"line"`
	HTMLURL     string     `json:"html_url"`
}

// ReviewUser is a comment's author; Type is "Bot" for apps such as Codex.
type ReviewUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// ListReviewComments lists the inline review comments of a pull request.
func (c *Client) ListReviewComments(token, owner, repo string, number int) ([]ReviewComment, error) {
	var comments []ReviewComment
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments?per_page=100", githubAPIBaseURL, owner, repo, number)
	if err := c.do(http.MethodGet, endpoint, token, nil, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

// ReplyToReviewComment replies in the thread of an inline review comment.
func (c *Client) ReplyToReviewComment(token, owner, repo string, number int, commentID int64, body string) (*ReviewComment, error) {
	var reply ReviewComment
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments/%d/replies", githubAPIBaseURL, owner, repo, number, commentID)
	if err := c.do(http.MethodPost, endpoint, token, map[string]string{"body": body}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// ReviewThread is a conversation on a diff, which can be resolved.
type ReviewThread struct {
	ID         string  `json:"id"`
	IsResolved bool    `json:"isResolved"`
	IsOutdated bool    `json:"isOutdated"`
	Path       string  `json:"path"`
	CommentIDs []int64 `json:"-"`
}

// ListReviewThreads lists the first 100 review threads of a pull request with
// the database IDs of their comments, which the REST comments share.
func (c *Client) ListReviewThreads(token, owner, repo string, number int) ([]ReviewThread, error) {
	var data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ReviewThread
						Comments struct {
							Nodes []struct {
								DatabaseID int64 `json:"databaseId"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	const query = `query($owner: String!, $name: String!, $number: Int!) { repository(owner: $owner, name: $name) { pullRequest(number: $number) { reviewThreads(first: 100) { nodes { id isResolved isOutdated path comments(first: 100) { nodes { databaseId } } } } } } }`
	if err := c.graphQL(token, query, map[string]any{"owner": owner, "name": repo, "number": number}, &data); err != nil {
		return nil, err
	}
	var threads []ReviewThread
	for _, node := range data.Repository.PullRequest.ReviewThreads.Nodes {
		thread := node.ReviewThread
		for _, comment := range node.Comments.Nodes {
			thread.CommentIDs = append(thread.CommentIDs, comment.DatabaseID)
		}
		threads = append(threads, thread)
	}
	return threads, nil
}

// ResolveReviewThread marks a review thread resolved.
func (c *Client) ResolveReviewThread(token, threadID string) error {
	return c.graphQL(token, `mutation($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { isResolved } } }`, map[string]any{"id": threadID}, nil)
}

// ConvertPullRequestToDraft puts a pull request back into draft.
func (c *Client) ConvertPullRequestToDraft(token, nodeID string) error {
	return c.graphQL(token, `mutation($id: ID!) { convertPullRequestToDraft(input: {pullRequestId: $id}) { pullRequest { isDraft } } }`, map[string]any{"id": nodeID}, nil)
}

// UpdatePullRequestBranch merges the base branch into the pull request's head
// branch, only while the head is still expectedHead. GitHub answers 202 and
// completes the merge asynchronously.
func (c *Client) UpdatePullRequestBranch(token, owner, repo string, number int, expectedHead string) error {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/update-branch", githubAPIBaseURL, owner, repo, number)
	return c.do(http.MethodPut, endpoint, token, map[string]string{"expected_head_sha": expectedHead}, nil)
}
