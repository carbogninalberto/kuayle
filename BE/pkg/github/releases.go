package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Release is a GitHub release and its tag.
type Release struct {
	ID              int64      `json:"id"`
	TagName         string     `json:"tag_name"`
	Name            string     `json:"name"`
	Body            string     `json:"body"`
	HTMLURL         string     `json:"html_url"`
	TargetCommitish string     `json:"target_commitish"`
	Draft           bool       `json:"draft"`
	Prerelease      bool       `json:"prerelease"`
	PublishedAt     *time.Time `json:"published_at"`
}

// NewRelease is what CreateRelease publishes. The tag is created at
// TargetCommitish, which callers pin to a full commit SHA.
type NewRelease struct {
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Name            string `json:"name"`
	Body            string `json:"body"`
}

// ListReleases lists the newest releases, drafts included when the token may
// see them.
func (c *Client) ListReleases(token, owner, repo string, limit int) ([]Release, error) {
	var releases []Release
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", githubAPIBaseURL, owner, repo, limit)
	if err := c.do(http.MethodGet, endpoint, token, nil, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// GetReleaseByTag reads one published release by its tag.
func (c *Client) GetReleaseByTag(token, owner, repo, tag string) (*Release, error) {
	var release Release
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBaseURL, owner, repo, url.PathEscape(tag))
	if err := c.do(http.MethodGet, endpoint, token, nil, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

// TagExists reports whether the repository already has the tag.
func (c *Client) TagExists(token, owner, repo, tag string) (bool, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/git/ref/tags/%s", githubAPIBaseURL, owner, repo, url.PathEscape(tag))
	err := c.do(http.MethodGet, endpoint, token, nil, nil)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

// CreateRelease publishes a release, creating its tag at the target commit, and
// marks it the latest release.
func (c *Client) CreateRelease(token, owner, repo string, release NewRelease) (*Release, error) {
	body := map[string]any{
		"tag_name": release.TagName, "target_commitish": release.TargetCommitish, "name": release.Name, "body": release.Body,
		"draft": false, "prerelease": false, "make_latest": "true",
	}
	var created Release
	if err := c.do(http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/releases", githubAPIBaseURL, owner, repo), token, body, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// WorkflowRun is one GitHub Actions run.
type WorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HTMLURL    string    `json:"html_url"`
	HeadSHA    string    `json:"head_sha"`
	HeadBranch string    `json:"head_branch"`
	RunAttempt int       `json:"run_attempt"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// WorkflowStep is one step of a job.
type WorkflowStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// WorkflowJob is one job of a run. Steps are returned by GetWorkflowJob, not
// by the run's job list.
type WorkflowJob struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	Conclusion string         `json:"conclusion"`
	HTMLURL    string         `json:"html_url"`
	Steps      []WorkflowStep `json:"steps"`
}

// ListWorkflowRuns lists the newest runs (at most 30) for a commit.
func (c *Client) ListWorkflowRuns(token, owner, repo, sha string) ([]WorkflowRun, error) {
	var batch struct {
		Runs []WorkflowRun `json:"workflow_runs"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/actions/runs?head_sha=%s&per_page=30", githubAPIBaseURL, owner, repo, url.QueryEscape(sha))
	if err := c.do(http.MethodGet, endpoint, token, nil, &batch); err != nil {
		return nil, err
	}
	return batch.Runs, nil
}

// GetWorkflowRun reads one run.
func (c *Client) GetWorkflowRun(token, owner, repo string, id int64) (*WorkflowRun, error) {
	var run WorkflowRun
	if err := c.do(http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", githubAPIBaseURL, owner, repo, id), token, nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// GetWorkflowJob reads one job, including the steps that failed. It is the
// single call Kuayle makes per failed check when it classifies CI (KUA-137).
func (c *Client) GetWorkflowJob(token, owner, repo string, id int64) (*WorkflowJob, error) {
	return c.GetWorkflowJobContext(context.Background(), token, owner, repo, id)
}

// GetWorkflowJobContext is GetWorkflowJob bounded by ctx.
func (c *Client) GetWorkflowJobContext(ctx context.Context, token, owner, repo string, id int64) (*WorkflowJob, error) {
	var job WorkflowJob
	if err := c.doContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d", githubAPIBaseURL, owner, repo, id), token, nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// maxJobPages bounds the jobs read for one run.
const maxJobPages = 3

// ListWorkflowJobs lists the jobs of a run's latest attempt; truncated is true
// when the page limit cut the list short.
func (c *Client) ListWorkflowJobs(token, owner, repo string, id int64) (jobs []WorkflowJob, truncated bool, err error) {
	for page := 1; page <= maxJobPages; page++ {
		var batch struct {
			TotalCount int           `json:"total_count"`
			Jobs       []WorkflowJob `json:"jobs"`
		}
		endpoint := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs?per_page=100&page=%d", githubAPIBaseURL, owner, repo, id, page)
		if err = c.do(http.MethodGet, endpoint, token, nil, &batch); err != nil {
			return nil, false, err
		}
		jobs = append(jobs, batch.Jobs...)
		if len(batch.Jobs) == 0 || len(jobs) >= batch.TotalCount {
			return jobs, false, nil
		}
	}
	return jobs, true, nil
}

// MaxWorkflowJobLogBytes bounds how much of the end of an Actions job log
// Kuayle reads: the reason a runner failed is printed last, and a log can be
// very large.
const MaxWorkflowJobLogBytes = 256 << 10

// GetWorkflowJobLog reads the end of an Actions job's log. The API answers
// with a redirect to the log file; the HTTP client follows it. Kuayle asks
// for the last limit bytes with a Range header, so a large log is not
// downloaded whole; a server that ignores it still leaves at most the end
// (MaxWorkflowJobLogBytes when limit is not positive).
func (c *Client) GetWorkflowJobLog(ctx context.Context, token, owner, repo string, id int64, limit int64) (string, error) {
	if limit <= 0 {
		limit = MaxWorkflowJobLogBytes
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", githubAPIBaseURL, owner, repo, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Range", fmt.Sprintf("bytes=-%d", limit))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", &APIError{Status: resp.StatusCode, Message: string(body)}
	}
	if resp.StatusCode == http.StatusPartialContent {
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
		return string(body), err
	}
	return readEnd(resp.Body, limit)
}

// readEnd reads a stream and keeps only its last limit bytes.
func readEnd(r io.Reader, limit int64) (string, error) {
	kept := make([]byte, 0, min(limit, 64<<10))
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			kept = append(kept, buf[:n]...)
			if int64(len(kept)) > limit {
				kept = append(kept[:0], kept[int64(len(kept))-limit:]...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return string(kept), nil
}
