package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// KUA-193: a retry, or a message that starts a new iteration, on an issue
// whose work was already merged is refused until the person confirms it.

// linkGitHubPullRequest links pull request number in state to issue through
// the workspace's GitHub installation and repository acme/app, created on
// first use.
func linkGitHubPullRequest(t *testing.T, db *sqlx.DB, ws, installer, issue uuid.UUID, number int, state string) {
	t.Helper()
	var repo uuid.UUID
	err := db.Get(&repo, `SELECT id FROM github_repos WHERE workspace_id=$1`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		var installation uuid.UUID
		require.NoError(t, db.Get(&installation, `INSERT INTO github_installations(workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,'acme','Organization',$3) RETURNING id`, ws, int64(uuid.New().ID()), installer))
		err = db.Get(&repo, `INSERT INTO github_repos(installation_id,workspace_id,github_repo_id,full_name) VALUES($1,$2,42,'acme/app') RETURNING id`, installation, ws)
	}
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_pull_requests(workspace_id,issue_id,github_repo_id,github_pr_id,number,title,state,author_login,html_url,merged_at)
 VALUES($1,$2,$3,$4,$5,'Work',$6::text,'dev',$7,CASE WHEN $6::text='merged' THEN now() END)`, ws, issue, repo, int64(number), number, state, fmt.Sprintf("https://github.com/acme/app/pull/%d", number))
	require.NoError(t, err)
}

// recoveryIssue adds another issue to the task's team.
func recoveryIssue(t *testing.T, db *sqlx.DB, task domain.AgentTask, number int) uuid.UUID {
	t.Helper()
	var issue uuid.UUID
	require.NoError(t, db.Get(&issue, `INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 VALUES($1,$2,$3,$4,'Other',$5,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1)) RETURNING id`, task.WorkspaceID, task.TeamID, number, fmt.Sprintf("REC-%d", number), task.CreatorID))
	return issue
}

func TestRetryOfMergedWorkNeedsConfirmation(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	ws := task.WorkspaceID
	_, err := db.Exec(`UPDATE agent_tasks SET state='failed' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	linkGitHubPullRequest(t, db, ws, fixture.userID, task.IssueID, 7, "open")
	linkGitHubPullRequest(t, db, ws, fixture.userID, task.IssueID, 8, "closed")
	linkGitHubPullRequest(t, db, ws, fixture.userID, recoveryIssue(t, db, task, 2), 9, "merged")
	none, err := repo.MergedPullRequest(ctx, ws, task.IssueID)
	require.NoError(t, err)
	require.Nil(t, none, "open and closed pull requests, and another issue's merged one, are no merged work of this issue")

	linkGitHubPullRequest(t, db, ws, fixture.userID, task.IssueID, 12, "merged")
	request := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: task.Generation, Prompt: "Try again"}
	_, err = repo.Retry(ctx, ws, task.ID, request)
	var merged *domain.MergedWorkError
	require.ErrorAs(t, err, &merged)
	require.Equal(t, domain.MergedPullRequest{Number: 12, URL: "https://github.com/acme/app/pull/12", Repository: "acme/app"}, merged.PullRequest)
	current, err := repo.Get(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State, "nothing starts before the person confirms")
	require.Equal(t, task.Generation, current.Generation)
	var receipts int
	require.NoError(t, db.Get(&receipts, `SELECT count(*) FROM agent_task_retries WHERE task_id=$1`, task.ID))
	require.Zero(t, receipts, "the refused request can be sent again, confirmed, with the same request id")

	request.ConfirmMerged = true
	retried, err := repo.Retry(ctx, ws, task.ID, request)
	require.NoError(t, err)
	require.Equal(t, "queued", retried.State)
	require.Equal(t, task.Generation+1, retried.Generation)
	// An accepted request replays without asking again.
	request.ConfirmMerged = false
	replayed, err := repo.Retry(ctx, ws, task.ID, request)
	require.NoError(t, err)
	require.Equal(t, retried.RunID, replayed.RunID)
	// A task that cannot be retried says so: there is nothing to confirm.
	_, err = repo.Retry(ctx, ws, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: retried.Generation, Prompt: "Again"})
	require.ErrorIs(t, err, ErrAgentTaskConflict)
}

// A merge request of another forge (GitLab, Gitea, Forgejo) linked to the
// issue is merged work too.
func TestMergedWorkIncludesForgeMergeRequests(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	var connection, forgeRepo uuid.UUID
	require.NoError(t, db.Get(&connection, `INSERT INTO forge_connections(workspace_id,provider,base_url,webhook_secret,created_by) VALUES($1,'gitlab','https://gitlab.example.test','secret',$2) RETURNING id`, task.WorkspaceID, fixture.userID))
	require.NoError(t, db.Get(&forgeRepo, `INSERT INTO forge_repos(connection_id,workspace_id,external_repo_id,full_name) VALUES($1,$2,7,'acme/api') RETURNING id`, connection, task.WorkspaceID))
	_, err := db.Exec(`INSERT INTO forge_merge_requests(workspace_id,issue_id,forge_repo_id,external_id,number,title,state,author_login,web_url,merged_at)
 VALUES($1,$2,$3,1,3,'Work','merged','dev','https://gitlab.example.test/acme/api/-/merge_requests/3',now())`, task.WorkspaceID, task.IssueID, forgeRepo)
	require.NoError(t, err)
	merged, err := repo.MergedPullRequest(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Equal(t, &domain.MergedPullRequest{Number: 3, URL: "https://gitlab.example.test/acme/api/-/merge_requests/3", Repository: "acme/api"}, merged)
	// The newest merge is named when there are several.
	linkGitHubPullRequest(t, db, task.WorkspaceID, fixture.userID, task.IssueID, 12, "merged")
	_, err = db.Exec(`UPDATE github_pull_requests SET merged_at=now()+interval '1 minute' WHERE number=12`)
	require.NoError(t, err)
	merged, err = repo.MergedPullRequest(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Equal(t, 12, merged.Number)
}

// Only members of the issue's workspace learn about its pull requests, and
// only through that workspace. The database keeps GitHub activity on public
// issues of the issue's own workspace, so there is no private-team row.
func TestMergedWorkRespectsWorkspaceVisibility(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	other := uuid.New()
	_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, other, "other-"+other.String()[:8], fixture.userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, other, fixture.userID)
	require.NoError(t, err)
	linkGitHubPullRequest(t, db, task.WorkspaceID, fixture.userID, task.IssueID, 12, "merged")
	merged, err := repo.MergedPullRequest(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.NotNil(t, merged)

	elsewhere := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: other, UserID: fixture.userID})
	merged, err = repo.MergedPullRequest(elsewhere, other, task.IssueID)
	require.NoError(t, err)
	require.Nil(t, merged, "another workspace, even for a member of both")
	merged, err = repo.MergedPullRequest(elsewhere, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Nil(t, merged, "a request scoped to another workspace")
	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	merged, err = repo.MergedPullRequest(outsider, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Nil(t, merged, "a non-member")
	_, err = db.Exec(`UPDATE teams SET is_private=true WHERE id=$1`, task.TeamID)
	require.Error(t, err, "a team with GitHub activity or dev machines stays public")
}

// "Send and start now" on a failed task starts a new iteration, so merged
// work needs the same confirmation; a plain message starts nothing.
func TestStartingAnIterationForAMessageOnMergedWorkNeedsConfirmation(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	ws := task.WorkspaceID
	_, err := db.Exec(`UPDATE agent_tasks SET state='failed' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	linkGitHubPullRequest(t, db, ws, fixture.userID, task.IssueID, 12, "merged")
	_, err = repo.SendLiveMessage(ctx, ws, task.ID, "Try the other fixture", domain.AgentLiveSendStart, false)
	var merged *domain.MergedWorkError
	require.ErrorAs(t, err, &merged)
	require.Equal(t, 12, merged.PullRequest.Number)
	messages, err := repo.LiveMessages(ctx, ws, task.ID, 10)
	require.NoError(t, err)
	require.Empty(t, messages, "nothing is queued before the person confirms")
	current, err := repo.Get(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State)

	_, err = repo.SendLiveMessage(ctx, ws, task.ID, "A note for the next iteration", "", false)
	require.NoError(t, err)
	current, err = repo.Get(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State, "a plain message waits")

	_, err = repo.SendLiveMessage(ctx, ws, task.ID, "Try the other fixture", domain.AgentLiveSendStart, true)
	require.NoError(t, err)
	current, err = repo.Get(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Equal(t, task.Generation+1, current.Generation)
}
