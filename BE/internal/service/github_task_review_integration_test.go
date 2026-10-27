package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type mergedPullRequestRecorder struct {
	calls []struct {
		workspace  uuid.UUID
		repository string
		number     int
	}
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
