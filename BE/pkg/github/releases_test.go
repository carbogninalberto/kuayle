package github

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleasesAreListedReadAndCreatedAtTheChosenCommit(t *testing.T) {
	var created map[string]any
	c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/releases":
			require.Equal(t, "5", r.URL.Query().Get("per_page"))
			return 200, `[{"id":1,"tag_name":"v1.2.0","name":"v1.2.0","body":"### Features","html_url":"https://github.com/owner/repo/releases/tag/v1.2.0","target_commitish":"main","draft":false,"prerelease":false,"published_at":"2026-10-01T10:00:00Z"}]`
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/releases/tags/v1.2.0":
			return 200, `{"id":1,"tag_name":"v1.2.0","body":"### Features\n- One"}`
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/git/ref/tags/v1.2.0":
			return 200, `{"ref":"refs/tags/v1.2.0","object":{"sha":"abc","type":"commit"}}`
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/git/ref/tags/v1.3.0":
			return 404, `{"message":"Not Found"}`
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/releases":
			created = body
			return 201, `{"id":2,"tag_name":"v1.3.0","html_url":"https://github.com/owner/repo/releases/tag/v1.3.0"}`
		}
		return 500, `{"message":"unexpected"}`
	})
	releases, err := c.ListReleases("secret", "owner", "repo", 5)
	require.NoError(t, err)
	require.Len(t, releases, 1)
	require.Equal(t, "v1.2.0", releases[0].TagName)
	require.NotNil(t, releases[0].PublishedAt)

	release, err := c.GetReleaseByTag("secret", "owner", "repo", "v1.2.0")
	require.NoError(t, err)
	require.Equal(t, "### Features\n- One", release.Body)

	exists, err := c.TagExists("secret", "owner", "repo", "v1.2.0")
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = c.TagExists("secret", "owner", "repo", "v1.3.0")
	require.NoError(t, err)
	require.False(t, exists, "a 404 means the tag is free")

	release, err = c.CreateRelease("secret", "owner", "repo", NewRelease{TagName: "v1.3.0", TargetCommitish: "0123456789abcdef0123456789abcdef01234567", Name: "v1.3.0", Body: "notes"})
	require.NoError(t, err)
	require.Equal(t, "https://github.com/owner/repo/releases/tag/v1.3.0", release.HTMLURL)
	require.Equal(t, map[string]any{"tag_name": "v1.3.0", "target_commitish": "0123456789abcdef0123456789abcdef01234567", "name": "v1.3.0", "body": "notes", "draft": false, "prerelease": false, "make_latest": "true"}, created)
}

func TestWorkflowRunsAreFoundByCommitWithTheirJobs(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs":
			require.Equal(t, "abc", r.URL.Query().Get("head_sha"))
			return 200, `{"total_count":1,"workflow_runs":[{"id":9,"name":"Build & Push","event":"release","status":"completed","conclusion":"success","html_url":"https://github.com/owner/repo/actions/runs/9","head_sha":"abc","run_attempt":1}]}`
		case "/repos/owner/repo/actions/runs/9":
			return 200, `{"id":9,"name":"Build & Push","event":"release","status":"in_progress","html_url":"https://github.com/owner/repo/actions/runs/9","head_sha":"abc"}`
		case "/repos/owner/repo/actions/runs/9/jobs":
			return 200, `{"total_count":2,"jobs":[{"name":"api","status":"completed","conclusion":"failure","html_url":"https://github.com/owner/repo/actions/runs/9/job/1"},{"name":"web","status":"in_progress"}]}`
		}
		return 404, `{"message":"Not Found"}`
	})
	runs, err := c.ListWorkflowRuns("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "Build & Push", runs[0].Name)
	require.Equal(t, "success", runs[0].Conclusion)

	run, err := c.GetWorkflowRun("secret", "owner", "repo", 9)
	require.NoError(t, err)
	require.Equal(t, "in_progress", run.Status)
	jobs, truncated, err := c.ListWorkflowJobs("secret", "owner", "repo", 9)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, jobs, 2)
	require.Equal(t, "failure", jobs[0].Conclusion)

	_, err = c.GetWorkflowRun("secret", "owner", "repo", 10)
	var api *APIError
	require.True(t, errors.As(err, &api))
	require.Equal(t, 404, api.Status)
}

func TestComparisonFilesCarryTheirPatch(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		return 200, `{"status":"ahead","files":[{"filename":"a.go","status":"modified","patch":"@@ -1 +1 @@\n-old\n+new"}]}`
	})
	comparison, err := c.CompareCommits("secret", "owner", "repo", "v1", "abc")
	require.NoError(t, err)
	require.Equal(t, "@@ -1 +1 @@\n-old\n+new", comparison.Files[0].Patch)
}
