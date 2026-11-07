package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A trusted mention of the bot asking for a review on the pull request a task
// published starts one review for the task's creator, without an approval,
// only while the workspace turned the ability on. The review uses chat's
// defaults, posts its findings and a redelivered webhook converges on it.
func TestMentionOnPullRequestStartsOneReviewWhenEnabled(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, tasks := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	taskID := *started.TaskID
	publishTask(t, f, taskID)

	ctx := context.Background()
	githubRepo := repository.NewGitHubRepository(f.db)
	inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: f.ws, InstallationID: time.Now().UnixNano(), AccountLogin: "acme", AccountType: "Organization", InstalledBy: f.user}
	require.NoError(t, githubRepo.CreateInstallation(ctx, inst))
	linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: f.ws, InstallationID: inst.ID, GitHubRepoID: time.Now().UnixNano(), FullName: "acme/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, githubRepo.CreateRepo(ctx, linked))

	bot := repository.NewGitHubBotRepository(f.db)
	work := repository.NewAgentTaskRepository(f.db)
	reviews := &reviewTaskFake{approvalTaskFake: tasks}
	svc := NewGitHubService(githubRepo, repository.NewIssueRepository(f.db), nil, nil, nil, nil, nil, "https://kuayle.test", "", &GlobalGitHubAppConfig{Slug: "kuayle-acme"})
	svc.SetBotSettings(bot)
	svc.SetMentionReviews(NewGitHubMentionReviews(work, f.repo, reviews))

	comment := func(id int64, number int, body, association, userType string) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"action":       "created",
			"issue":        map[string]any{"number": number, "pull_request": map[string]any{"url": "https://api.github.com/repos/acme/app/pulls/12"}},
			"comment":      map[string]any{"id": id, "body": body, "author_association": association, "user": map[string]any{"login": "octo", "type": userType}},
			"repository":   map[string]any{"id": linked.GitHubRepoID, "full_name": "attacker/other"},
			"installation": map[string]any{"id": inst.InstallationID},
		})
		require.NoError(t, err)
		require.NoError(t, svc.HandleWebhookEvent(ctx, "issue_comment", payload))
	}
	countReviews := func() int {
		var n int
		require.NoError(t, f.db.Get(&n, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1 AND kind='review'`, f.ws))
		return n
	}

	comment(1, 12, "@kuayle-acme please review the error handling", "MEMBER", "User")
	require.Zero(t, countReviews(), "the mention trigger is off by default")

	_, _, err = bot.Update(ctx, f.ws, f.user, func(s *domain.GitHubBotSettings) error {
		s.Abilities[string(domain.GitHubBotMentionReviews)] = true
		return nil
	})
	require.NoError(t, err)

	comment(2, 12, "@kuayle-acme please review", "CONTRIBUTOR", "User")
	comment(3, 12, "@kuayle-acme[bot] review please", "MEMBER", "Bot")
	comment(4, 12, "@kuayle-acme thanks!", "MEMBER", "User")
	comment(5, 12, "@kuayle-acmex review this", "MEMBER", "User")
	comment(6, 99, "@kuayle-acme review", "MEMBER", "User")
	require.Zero(t, countReviews(), "untrusted, bot, non-review, other-handle and unknown pull request comments start nothing")

	comment(7, 12, "Hey @Kuayle-Acme, review the error handling please", "MEMBER", "User")
	require.Equal(t, 1, countReviews())
	var review domain.AgentReview
	require.NoError(t, f.db.Get(&review, `SELECT r.* FROM agent_reviews r JOIN agent_tasks t ON t.id=r.task_id WHERE t.workspace_id=$1`, f.ws))
	require.Equal(t, taskID, review.ReviewedTaskID)
	require.Equal(t, reviewedCommit, review.CommitSHA)
	require.Equal(t, "codex", review.Provider, "the default harness is the implementer's, as in chat")
	require.Equal(t, f.user, review.RequestedBy, "the review acts for the task's creator")
	require.True(t, review.PostToPullRequest, "findings of a mention are posted back")
	require.True(t, strings.HasPrefix(review.Focus, mentionReviewFocusPrefix+"octo"))
	require.Contains(t, review.Focus, "review the error handling please")
	require.NotContains(t, review.Focus, "@Kuayle-Acme")
	var model string
	require.NoError(t, f.db.Get(&model, `SELECT model FROM agent_tasks WHERE id=$1`, review.TaskID))
	require.Equal(t, "deepseek-flash", model, "the model chat would choose")

	comment(7, 12, "Hey @Kuayle-Acme, review the error handling please", "MEMBER", "User")
	require.Equal(t, 1, countReviews(), "a redelivered webhook converges on the same review")

	// The creator lost access: the mention is dropped instead of failing the delivery.
	_, err = f.db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='failed' WHERE id=$1`, review.TaskID)
	require.NoError(t, err)
	comment(8, 12, "@kuayle-acme review again", "MEMBER", "User")
	require.Equal(t, 1, countReviews())
}
