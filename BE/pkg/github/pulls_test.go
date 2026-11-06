package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func pullsClient(handle func(r *http.Request, body map[string]any) (int, string)) *Client {
	return &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		code, answer := handle(r, body)
		return commentResponse(code, answer), nil
	})}}
}

func TestMergePullRequestWithMethodSendsTheChosenMethod(t *testing.T) {
	for _, method := range []string{"merge", "squash", "rebase"} {
		var sent map[string]any
		c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
			require.Equal(t, "/repos/owner/repo/pulls/7/merge", r.URL.Path)
			sent = body
			return 200, `{"merged":true}`
		})
		require.NoError(t, c.MergePullRequestWithMethod("secret", "owner", "repo", 7, "abc", method))
		require.Equal(t, map[string]any{"sha": "abc", "merge_method": method}, sent)
	}
}

func TestReplyAndUpdateBranchPinTheirTargets(t *testing.T) {
	c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/7/comments/55/replies":
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, map[string]any{"body": "Fixed"}, body)
			return 201, `{"id":56,"body":"Fixed","user":{"login":"kuayle[bot]","type":"Bot"}}`
		case "/repos/owner/repo/pulls/7/update-branch":
			require.Equal(t, http.MethodPut, r.Method)
			require.Equal(t, map[string]any{"expected_head_sha": "abc"}, body)
			return 202, `{"message":"Updating pull request branch."}`
		}
		return 404, `{}`
	})
	reply, err := c.ReplyToReviewComment("secret", "owner", "repo", 7, 55, "Fixed")
	require.NoError(t, err)
	require.EqualValues(t, 56, reply.ID)
	require.NoError(t, c.UpdatePullRequestBranch("secret", "owner", "repo", 7, "abc"))
}

func TestAPIErrorsCarryTheStatusAndGitHubsMessage(t *testing.T) {
	c := pullsClient(func(*http.Request, map[string]any) (int, string) {
		return 403, `{"message":"Resource not accessible by integration"}`
	})
	err := c.UpdatePullRequestBranch("secret", "owner", "repo", 7, "abc")
	var api *APIError
	require.True(t, errors.As(err, &api))
	require.Equal(t, 403, api.Status)
	require.Equal(t, "Resource not accessible by integration", api.Message)
}

func TestListCheckRunsAndStatusesAndMergeSettings(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc/check-runs":
			return 200, `{"check_runs":[{"name":"unit","status":"completed","conclusion":"failure","html_url":"https://github.com/owner/repo/runs/1","output":{"title":"3 failed","summary":"see logs"}}]}`
		case "/repos/owner/repo/commits/abc/status":
			return 200, `{"statuses":[{"context":"ci/legacy","state":"pending","target_url":"https://ci.example"}]}`
		case "/repos/owner/repo":
			return 200, `{"allow_squash_merge":false,"allow_merge_commit":true,"allow_rebase_merge":false}`
		}
		return 404, `{}`
	})
	runs, _, err := c.ListCheckRuns("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.Equal(t, "unit", runs[0].Name)
	require.Equal(t, "3 failed", runs[0].Output.Title)
	statuses, _, err := c.ListCommitStatuses("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.Equal(t, "pending", statuses[0].State)
	settings, err := c.GetRepositoryMergeSettings("secret", "owner", "repo")
	require.NoError(t, err)
	require.True(t, settings.Allows("merge"))
	require.False(t, settings.Allows("squash"))
	require.False(t, settings.Allows("fast-forward"))
}

func TestReviewThreadsMapCommentIDsAndGraphQLErrorsAreFailures(t *testing.T) {
	c := pullsClient(func(r *http.Request, body map[string]any) (int, string) {
		require.Equal(t, "/graphql", r.URL.Path)
		if body["query"].(string)[:5] == "query" {
			return 200, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"T1","isResolved":false,"isOutdated":true,"path":"a.go","comments":{"nodes":[{"databaseId":55},{"databaseId":56}]}}]}}}}}`
		}
		return 200, `{"errors":[{"message":"Resource not accessible by integration"}]}`
	})
	threads, truncated, err := c.ListReviewThreads("secret", "owner", "repo", 7)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, threads, 1)
	require.Equal(t, "T1", threads[0].ID)
	require.True(t, threads[0].IsOutdated)
	require.Equal(t, []int64{55, 56}, threads[0].CommentIDs)
	err = c.ResolveReviewThread("secret", "T1")
	var api *APIError
	require.True(t, errors.As(err, &api))
	require.Contains(t, api.Message, "not accessible")
	require.Error(t, c.ConvertPullRequestToDraft("secret", "PR_node"))
}

func TestReviewCommentsAreReadPageByPage(t *testing.T) {
	var pages []string
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		pages = append(pages, r.URL.Query().Get("page"))
		if r.URL.Query().Get("page") == "1" {
			full := make([]string, 100)
			for i := range full {
				full[i] = fmt.Sprintf(`{"id":%d}`, i+1)
			}
			return 200, "[" + strings.Join(full, ",") + "]"
		}
		return 200, `[{"id":101}]`
	})
	comments, truncated, err := c.ListReviewComments("secret", "owner", "repo", 7)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, comments, 101)
	require.Equal(t, []string{"1", "2"}, pages)

	c = pullsClient(func(*http.Request, map[string]any) (int, string) {
		full := make([]string, 100)
		for i := range full {
			full[i] = `{"id":1}`
		}
		return 200, "[" + strings.Join(full, ",") + "]"
	})
	_, truncated, err = c.ListReviewComments("secret", "owner", "repo", 7)
	require.NoError(t, err)
	require.True(t, truncated, "a list cut by the page limit says so")
}

func TestReviewThreadsFollowCursorsThroughThreadsAndTheirComments(t *testing.T) {
	c := pullsClient(func(_ *http.Request, body map[string]any) (int, string) {
		query := body["query"].(string)
		vars := body["variables"].(map[string]any)
		switch {
		case strings.Contains(query, "node(id"):
			require.Equal(t, "T1", vars["id"])
			return 200, `{"data":{"node":{"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"databaseId":102}]}}}}`
		case vars["after"] == nil:
			return 200, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[{"id":"T1","isResolved":false,"comments":{"pageInfo":{"hasNextPage":true,"endCursor":"k1"},"nodes":[{"databaseId":101}]}}]}}}}}`
		}
		require.Equal(t, "c1", vars["after"])
		return 200, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"T2","isResolved":true,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"databaseId":201}]}}]}}}}}`
	})
	threads, truncated, err := c.ListReviewThreads("secret", "owner", "repo", 7)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, threads, 2)
	require.Equal(t, []int64{101, 102}, threads[0].CommentIDs)
	require.Equal(t, "T2", threads[1].ID)
	require.True(t, threads[1].IsResolved)
}

func TestCheckRunsAndStatusesFollowPagesUntilTotalCount(t *testing.T) {
	items := func(key string, from, to int) string {
		var parts []string
		for i := from; i <= to; i++ {
			parts = append(parts, fmt.Sprintf(`{"name":"c%d","context":"s%d"}`, i, i))
		}
		return fmt.Sprintf(`{"total_count":150,"%s":[%s]}`, key, strings.Join(parts, ","))
	}
	var pages []string
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		key := "check_runs"
		if strings.HasSuffix(r.URL.Path, "/status") {
			key = "statuses"
		}
		pages = append(pages, key+r.URL.Query().Get("page"))
		if r.URL.Query().Get("page") == "1" {
			return 200, items(key, 1, 100)
		}
		return 200, items(key, 101, 150)
	})
	runs, truncated, err := c.ListCheckRuns("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, runs, 150)
	statuses, truncated, err := c.ListCommitStatuses("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, statuses, 150)
	require.Equal(t, []string{"check_runs1", "check_runs2", "statuses1", "statuses2"}, pages)
}

func TestCheckRunsAndStatusesSayWhenThePageLimitCutThemShort(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		full := make([]string, 100)
		for i := range full {
			full[i] = `{"name":"c"}`
		}
		body := strings.Join(full, ",")
		if strings.HasSuffix(r.URL.Path, "/status") {
			return 200, `{"total_count":999999,"statuses":[` + body + `]}`
		}
		return 200, `{"total_count":999999,"check_runs":[` + body + `]}`
	})
	_, truncated, err := c.ListCheckRuns("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.True(t, truncated)
	_, truncated, err = c.ListCommitStatuses("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.True(t, truncated)
}

func TestCheckRunsAndStatusesFailOnALaterPage(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		if r.URL.Query().Get("page") == "2" {
			return 403, `{"message":"rate limited"}`
		}
		return 200, `{"total_count":150,"check_runs":[` + strings.Repeat(`{"name":"c"},`, 99) + `{"name":"c"}],"statuses":[` + strings.Repeat(`{"context":"s"},`, 99) + `{"context":"s"}]}`
	})
	_, _, err := c.ListCheckRuns("secret", "owner", "repo", "abc")
	var api *APIError
	require.True(t, errors.As(err, &api), "a failed page is an error, not a shorter list")
	_, _, err = c.ListCommitStatuses("secret", "owner", "repo", "abc")
	require.True(t, errors.As(err, &api))
}
