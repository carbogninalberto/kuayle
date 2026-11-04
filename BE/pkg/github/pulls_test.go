package github

import (
	"encoding/json"
	"errors"
	"net/http"
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
	runs, err := c.ListCheckRuns("secret", "owner", "repo", "abc")
	require.NoError(t, err)
	require.Equal(t, "unit", runs[0].Name)
	require.Equal(t, "3 failed", runs[0].Output.Title)
	statuses, err := c.ListCommitStatuses("secret", "owner", "repo", "abc")
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
	threads, err := c.ListReviewThreads("secret", "owner", "repo", 7)
	require.NoError(t, err)
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
