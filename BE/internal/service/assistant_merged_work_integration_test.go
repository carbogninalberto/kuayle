package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// mergeIssueWork records that pull request number of acme/app, linked to the
// issue, was merged on GitHub.
func mergeIssueWork(t *testing.T, f toolFixture, issue uuid.UUID, number int) {
	t.Helper()
	var repo uuid.UUID
	err := f.db.Get(&repo, `SELECT id FROM github_repos WHERE workspace_id=$1`, f.ws)
	if err != nil {
		var installation uuid.UUID
		require.NoError(t, f.db.Get(&installation, `INSERT INTO github_installations(workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,'acme','Organization',$3) RETURNING id`, f.ws, int64(uuid.New().ID()), f.user))
		require.NoError(t, f.db.Get(&repo, `INSERT INTO github_repos(installation_id,workspace_id,github_repo_id,full_name) VALUES($1,$2,42,'acme/app') RETURNING id`, installation, f.ws))
	}
	_, err = f.db.Exec(`INSERT INTO github_pull_requests(workspace_id,issue_id,github_repo_id,github_pr_id,number,title,state,author_login,html_url,merged_at)
 VALUES($1,$2,$3,$4,$5,'Work','merged','dev',$6,now())`, f.ws, issue, repo, int64(number), number, fmt.Sprintf("https://github.com/acme/app/pull/%d", number))
	require.NoError(t, err)
}

// KUA-193 through the assistant: the tool asks the model to confirm with the
// user, the confirmed card carries the merged pull request, and approving it
// retries the task. A card proposed before the merge is not approved past it.
func TestFollowUpApprovalsOnMergedWork(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, tasks := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	taskID := *started.TaskID
	publishTask(t, f, taskID)
	work := repository.NewAgentTaskRepository(f.db)
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetReviews(&reviewTaskFake{approvalTaskFake: tasks})
	ask := func(arguments string) (domain.ConversationAttempt, []domain.ConversationApproval, []map[string]any) {
		t.Helper()
		a := f.attempt(t, "Follow up please")
		provider, requests := scriptedProvider(t, toolCallStream("follow_up_task", arguments), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Follow up"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		return a, h.Approvals, *requests
	}
	issue := delegation.IssueID

	// Proposed before the merge, the card is not approved past it.
	early, approvals, _ := ask(`{"identifier":"ENG-1","instructions":"Handle the error in app.go"}`)
	require.Len(t, approvals, 1)
	mergeIssueWork(t, f, issue, 12)
	failed, err := f.service.ResolveApproval(f.ctx, f.ws, early.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Contains(t, *failed.Error, "merged in #12")
	current, err := work.Get(f.ctx, f.ws, taskID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", current.State)

	// The tool tells the model to ask; nothing is proposed.
	_, approvals, requests := ask(`{"identifier":"ENG-1","instructions":"Handle the error in app.go"}`)
	require.Empty(t, approvals)
	history := requests[1]["messages"].([]any)
	result := history[len(history)-1].(map[string]any)["content"].(string)
	require.Contains(t, result, "This issue's work was merged in #12 (https://github.com/acme/app/pull/12). Retry anyway?")
	require.Contains(t, result, "confirm_merged")

	// Confirmed, the card names the merged pull request and approving it retries the task.
	confirmed, approvals, _ := ask(`{"identifier":"ENG-1","instructions":"Handle the error in app.go","confirm_merged":true}`)
	require.Len(t, approvals, 1)
	var payload domain.FollowUpPayload
	require.NoError(t, json.Unmarshal(approvals[0].Payload, &payload))
	require.Equal(t, &domain.MergedPullRequest{Number: 12, URL: "https://github.com/acme/app/pull/12", Repository: "acme/app"}, payload.MergedPullRequest)
	approved, err := f.service.ResolveApproval(f.ctx, f.ws, confirmed.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)
	current, err = work.Get(f.ctx, f.ws, taskID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Equal(t, int64(2), current.Generation)
}
