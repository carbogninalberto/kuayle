package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type mergedPullRequestRecorder struct {
	review []repository.ReviewPullRequest
	calls  []struct {
		workspace  uuid.UUID
		repository string
		number     int
	}
}

func (r *mergedPullRequestRecorder) ReviewPullRequests(context.Context, int) ([]repository.ReviewPullRequest, error) {
	return r.review, nil
}

func (r *mergedPullRequestRecorder) CompleteMergedPullRequest(_ context.Context, workspace uuid.UUID, repository string, number int) (int, error) {
	r.calls = append(r.calls, struct {
		workspace  uuid.UUID
		repository string
		number     int
	}{workspace, repository, number})
	return 1, nil
}

// Only GitHub's merged state for a linked, active repository completes tasks;
// the repository name comes from Kuayle's link, not from the webhook body.
func TestMergedPullRequestWebhookCompletesAgentTasks(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Review test','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Review test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	repo := repository.NewGitHubRepository(db)
	inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: time.Now().UnixNano(), AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner}
	require.NoError(t, repo.CreateInstallation(ctx, inst))
	linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID, GitHubRepoID: time.Now().UnixNano(), FullName: "acme/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.CreateRepo(ctx, linked))
	recorder := &mergedPullRequestRecorder{}
	svc := NewGitHubService(repo, repository.NewIssueRepository(db), nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	svc.SetTaskReviews(recorder)
	event := func(action string, merged bool, number int) []byte {
		payload, err := json.Marshal(map[string]any{
			"action": action,
			"pull_request": map[string]any{"id": number, "number": number, "title": "No issue reference", "state": "closed", "merged": merged,
				"html_url": "https://github.com/acme/app/pull/1", "head": map[string]any{"ref": "kuayle/rec-1"}, "base": map[string]any{"ref": "main"},
				"user": map[string]any{"login": "kuayle[bot]"}, "body": "repository: attacker/other"},
			"repository":   map[string]any{"id": linked.GitHubRepoID, "full_name": "attacker/other"},
			"installation": map[string]any{"id": inst.InstallationID},
		})
		require.NoError(t, err)
		return payload
	}
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", event("closed", false, 7)))
	require.Empty(t, recorder.calls, "closing without merge is not acceptance")
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", event("closed", true, 7)))
	require.Len(t, recorder.calls, 1)
	require.Equal(t, ws, recorder.calls[0].workspace)
	require.Equal(t, "acme/app", recorder.calls[0].repository)
	require.Equal(t, 7, recorder.calls[0].number)
	_, err = db.Exec(`UPDATE github_repos SET is_active=false WHERE id=$1`, linked.ID)
	require.NoError(t, err)
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", event("closed", true, 8)))
	require.Len(t, recorder.calls, 1, "inactive repositories cannot complete tasks")
}

type signalRecorder struct {
	inputs         []domain.AgentTaskSignalInput
	repos          []string
	numbers        []int
	candidate      bool
	reviewable     bool
	candidateCalls int
}

func (r *signalRecorder) Record(_ context.Context, _ uuid.UUID, repository string, number int, in domain.AgentTaskSignalInput) error {
	r.inputs, r.repos, r.numbers = append(r.inputs, in), append(r.repos, repository), append(r.numbers, number)
	return nil
}

func (r *signalRecorder) HasSignalCandidate(context.Context, uuid.UUID, string, int, string) (bool, bool, error) {
	r.candidateCalls++
	return r.candidate, r.reviewable, nil
}

func TestCheckSuiteAndReviewWebhooksRecordTaskSignals(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Signals test','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Signals test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	repo := repository.NewGitHubRepository(db)
	inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: time.Now().UnixNano(), AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner}
	require.NoError(t, repo.CreateInstallation(ctx, inst))
	linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID, GitHubRepoID: time.Now().UnixNano(), FullName: "acme/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.CreateRepo(ctx, linked))
	recorder := &signalRecorder{candidate: true, reviewable: true}
	svc := NewGitHubService(repo, repository.NewIssueRepository(db), nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	svc.SetTaskSignals(recorder)
	svc.repositoryPermission = func(context.Context, *domain.GitHubInstallation, *domain.GitHubRepoModel, string) (string, error) {
		return "write", nil
	}
	ciReads := 0
	svc.ciChecks = func(context.Context, uuid.UUID, string, string) ([]domain.CIFailedCheck, bool) {
		ciReads++
		// The real shape of a runner failure: empty check output, one
		// annotation naming the build step, and the reason only in the job log.
		return []domain.CIFailedCheck{{
			Name: "Docker Runtime Integration", Conclusion: "failure",
			FailedStep:  "Build, smoke test and scan native architecture image",
			Annotations: []string{`buildx failed with: ERROR: failed to build: failed to solve: process "/bin/sh -c apt-get update && apt-get install -y ..." did not complete successfully`},
			LogTail:     "#7 [build 2/3] RUN apt-get update\nCould not resolve 'archive.ubuntu.com'\nTemporary failure resolving 'azure.archive.ubuntu.com'",
		}}, false
	}
	send := func(kind string, body map[string]any) {
		body["repository"] = map[string]any{"id": linked.GitHubRepoID, "full_name": "attacker/other"}
		body["installation"] = map[string]any{"id": inst.InstallationID}
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		require.NoError(t, svc.HandleWebhookEvent(ctx, kind, payload))
	}
	suite := func(action, conclusion string) map[string]any {
		return map[string]any{"action": action, "check_suite": map[string]any{"id": 77, "head_sha": "abc123", "conclusion": conclusion, "app": map[string]any{"name": "GitHub Actions"}, "pull_requests": []any{map[string]any{"number": 12}}}}
	}
	send("check_suite", suite("requested", ""))
	require.Empty(t, recorder.inputs, "only completed suites carry an outcome")
	send("check_suite", suite("completed", "FAILURE"))
	require.Len(t, recorder.inputs, 1)
	first := recorder.inputs[0]
	require.Equal(t, domain.AgentTaskSignalInput{Kind: "ci", ExternalID: "77", Outcome: "failure", Name: "GitHub Actions", HeadSHA: "abc123", URL: "https://github.com/acme/app/pull/12/checks", Failure: first.Failure}, first)
	require.NotNil(t, first.Failure, "a failed suite is classified when it is recorded")
	require.Equal(t, domain.CIFailureBuild, first.Failure.Class, "the test-step veto keeps the failed step the change's")
	require.Equal(t, "build_step", first.Failure.Rule)
	require.Equal(t, 1, ciReads)
	require.Equal(t, "acme/app", recorder.repos[0], "the repository comes from Kuayle's link, not the payload")
	require.Equal(t, 12, recorder.numbers[0])
	// A suite of a pull request without a task is matched first: GitHub's
	// checks are never read for it.
	recorder.candidate = false
	stray := suite("completed", "failure")
	stray["check_suite"].(map[string]any)["id"] = 88
	send("check_suite", stray)
	require.Equal(t, 1, ciReads, "no GitHub read for a PR no task published")
	require.Len(t, recorder.inputs, 1)
	// A task that no longer awaits review still records the suite; only the
	// classification read is skipped.
	recorder.candidate, recorder.reviewable = true, false
	late := suite("completed", "failure")
	late["check_suite"].(map[string]any)["id"] = 99
	send("check_suite", late)
	require.Len(t, recorder.inputs, 2, "recorded for a task that is publishing or completed")
	require.Nil(t, recorder.inputs[1].Failure, "no GitHub read without a reviewable candidate")
	require.Equal(t, 1, ciReads)
	recorder.reviewable = true

	review := func(association string) map[string]any {
		return map[string]any{"action": "submitted", "pull_request": map[string]any{"number": 12}, "review": map[string]any{"id": 5, "state": "changes_requested", "body": "Rename it", "author_association": association, "user": map[string]any{"login": "reviewer"}}}
	}
	send("pull_request_review", review("CONTRIBUTOR"))
	send("pull_request_review", review("MEMBER"))
	require.Len(t, recorder.inputs, 4)
	require.False(t, recorder.inputs[2].Trusted)
	require.True(t, recorder.inputs[3].Trusted)
	require.Equal(t, "changes_requested", recorder.inputs[3].Outcome)
	require.Equal(t, "Rename it", recorder.inputs[3].Summary)
	_, err = db.Exec(`UPDATE github_repos SET is_active=false WHERE id=$1`, linked.ID)
	require.NoError(t, err)
	send("check_suite", suite("completed", "failure"))
	require.Len(t, recorder.inputs, 4, "inactive repositories are ignored")
}

// A merge whose webhook never arrived is still completed by asking GitHub.
func TestMergeReconciliationCompletesTasksWhoseWebhookWasLost(t *testing.T) {
	ws := uuid.New()
	recorder := &mergedPullRequestRecorder{review: []repository.ReviewPullRequest{
		{WorkspaceID: ws, Repository: "acme/app", Number: 4},
		{WorkspaceID: ws, Repository: "acme/app", Number: 5},
		{WorkspaceID: ws, Repository: "acme/app", Number: 6},
	}}
	svc := NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	svc.SetTaskReviews(recorder)
	svc.pullRequestMerged = func(_ context.Context, _ uuid.UUID, _ string, number int) (bool, error) {
		switch number {
		case 4:
			return true, nil
		case 5:
			return false, nil
		}
		return false, errors.New("GitHub unavailable")
	}
	completed, err := svc.ReconcileMergedTasks(context.Background())
	require.NoError(t, err, "one unreachable pull request does not stop the others")
	require.Equal(t, 1, completed)
	require.Len(t, recorder.calls, 1)
	require.Equal(t, 4, recorder.calls[0].number)
}
