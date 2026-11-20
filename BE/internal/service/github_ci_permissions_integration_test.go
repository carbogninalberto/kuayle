package service

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestGitHubCIReadTokenPermissions(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	repo := repository.NewGitHubRepository(db)
	installationID := time.Now().UnixNano()
	ws := setupTestWorkspace(t, db, repo, installationID)
	app := &GlobalGitHubAppConfig{AppID: 9, PrivateKey: testAppPrivateKey(t), Slug: "kuayle"}

	const (
		fullName    = "acme/app"
		repoPath    = "/repos/" + fullName
		pullPath    = repoPath + "/pulls/1"
		checksPath  = repoPath + "/commits/abc/check-runs"
		statusPath  = repoPath + "/commits/abc/status"
		runsPath    = repoPath + "/actions/runs"
		runPath     = runsPath + "/9"
		jobsPath    = runPath + "/jobs"
		jobPath     = repoPath + "/actions/jobs/42"
		logPath     = jobPath + "/logs"
		annotations = repoPath + "/check-runs/3/annotations"
	)
	// The fixture sets the annotation count where the current reader expects
	// it so the test reaches the annotation request.
	responses := map[string]string{
		pullPath:    `{"head":{"sha":"abc"}}`,
		checksPath:  `{"total_count":1,"check_runs":[{"id":3,"name":"Test","status":"completed","conclusion":"failure","details_url":"https://github.com/acme/app/actions/runs/9/job/42","annotations_count":1}]}`,
		statusPath:  `{"total_count":1,"statuses":[{"context":"external-ci","state":"success"}]}`,
		runsPath:    `{"workflow_runs":[{"id":9}]}`,
		runPath:     `{"id":9}`,
		jobsPath:    `{"total_count":1,"jobs":[{"id":42}]}`,
		jobPath:     `{"id":42,"steps":[{"name":"Test","conclusion":"failure"}]}`,
		logPath:     "test failed",
		annotations: `[{"message":"test annotation"}]`,
	}

	for _, tt := range []struct {
		name        string
		permissions map[string]string
		paths       []string
		call        func(*testing.T, *GitHubService)
	}{
		{
			name:        "PullRequestChecks",
			permissions: map[string]string{"checks": "read", "statuses": "read"},
			paths:       []string{pullPath, checksPath, statusPath},
			call: func(t *testing.T, svc *GitHubService) {
				_, _, _, err := svc.PullRequestChecks(t.Context(), ws, fullName, 1)
				require.NoError(t, err)
			},
		},
		{
			name:        "CommitChecks",
			permissions: map[string]string{"checks": "read", "statuses": "read"},
			paths:       []string{checksPath, statusPath},
			call: func(t *testing.T, svc *GitHubService) {
				_, _, err := svc.CommitChecks(t.Context(), ws, fullName, "abc")
				require.NoError(t, err)
			},
		},
		{
			name:        "WorkflowRuns",
			permissions: map[string]string{"actions": "read"},
			paths:       []string{runsPath},
			call: func(t *testing.T, svc *GitHubService) {
				_, err := svc.WorkflowRuns(t.Context(), ws, fullName, "abc")
				require.NoError(t, err)
			},
		},
		{
			name:        "WorkflowRun",
			permissions: map[string]string{"actions": "read"},
			paths:       []string{runPath, jobsPath},
			call: func(t *testing.T, svc *GitHubService) {
				_, _, _, err := svc.WorkflowRun(t.Context(), ws, fullName, 9)
				require.NoError(t, err)
			},
		},
		{
			name:        "readCIChecksFromGitHub",
			permissions: map[string]string{"checks": "read", "actions": "read"},
			paths:       []string{checksPath, jobPath, annotations, logPath},
			call: func(t *testing.T, svc *GitHubService) {
				svc.readCIChecksFromGitHub(t.Context(), ws, fullName, "abc", 1)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}
			maps.Copy(want, tt.permissions)
			var paths []string
			tokenRequests := 0
			svc := NewGitHubService(repo, nil, nil, nil, nil, nil, nil, "", "", app)
			svc.githubHTTPClient = &http.Client{Transport: setupTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost {
					tokenRequests++
					require.Equal(t, fmt.Sprintf("/app/installations/%d/access_tokens", installationID), r.URL.Path)
					var payload struct {
						Repositories []string          `json:"repositories"`
						Permissions  map[string]string `json:"permissions"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
					require.Equal(t, []string{"app"}, payload.Repositories)
					require.Equal(t, want, payload.Permissions)
					return setupResponse(http.StatusCreated, `{"token":"ci-token","expires_at":"2099-01-01T00:00:00Z"}`), nil
				}
				require.Equal(t, http.MethodGet, r.Method)
				paths = append(paths, r.URL.Path)
				body, ok := responses[r.URL.Path]
				require.True(t, ok, "unexpected GitHub API path %s", r.URL.Path)
				return setupResponse(http.StatusOK, body), nil
			})}
			tt.call(t, svc)
			require.Equal(t, 1, tokenRequests)
			require.Equal(t, tt.paths, paths)
		})
	}
}
