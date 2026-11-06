package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// From the chat, one request and one approval merge the pull request, wait for
// its tests, publish the release with the notes as edited on the card and wait
// for the build; the card follows every step, and approving again or resuming
// after a restart repeats nothing.
func TestShipFromChatMergesAndReleasesAfterOneApproval(t *testing.T) {
	f := newToolFixture(t)
	var team, issue uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	require.NoError(t, f.db.Get(&issue, `INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1)) RETURNING id`, f.ws, team, f.user))

	work := prWork{merge: &repository.MergeTarget{TaskID: uuid.New(), IssueID: issue, CreatorID: f.user, Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "head1"}}
	pulls := &prGitHub{method: "squash", status: PullRequestStatus{State: "open", HeadSHA: "head1", BaseRef: "main"}}
	releases := &shipReleases{releaseFake: newReleaseFake()}
	releases.runs = []gh.WorkflowRun{{ID: 9, Name: "Build & Push", Event: "release", HeadBranch: "v1.3.0", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/app/actions/runs/9"}}
	merger := &shipMerger{github: pulls}
	members := repository.NewWorkspaceRepository(f.db)

	f.service.tools.SetAgentWork(work)
	f.service.tools.SetPullRequests(pulls, &prTargets{})
	f.service.tools.SetReleases(releases, releaseRepos{}, repository.NewAssistantSkillRepository(f.db))
	approvals := NewAssistantApprovals(f.repo, nil)
	approvals.SetMerging(work, members, merger)
	approvals.SetPullRequests(pulls, &prTargets{})
	approvals.SetReleases(releases)
	// The chain runs inline so the test sees its end; in the server it runs
	// after the approval request has returned.
	approvals.ships.spawn = func(run func()) { run() }
	approvals.ships.sleep = func(context.Context, time.Duration) error { return nil }
	f.service.SetApprovals(approvals)

	args := `{"identifier":"eng-1","tag":"v1.3.0","notes":"### Features\n- **Dark mode (ENG-1).** [#12](https://github.com/acme/app/pull/12)"}`
	ask := func() (domain.ConversationAttempt, []domain.ConversationApproval, string) {
		t.Helper()
		a := f.attempt(t, "Merge ENG-1 and release v1.3.0")
		provider, requests := scriptedProvider(t, toolCallStream(shipAction, args), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Ship it"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		history := (*requests)[1]["messages"].([]any)
		return a, h.Approvals, history[len(history)-1].(map[string]any)["content"].(string)
	}

	// A member cannot ask for the chain, which publishes a release.
	_, pending, answer := ask()
	require.Empty(t, pending)
	require.Contains(t, answer, "only workspace owners and admins can merge and release in one step")

	_, err := f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	a, pending, answer := ask()
	require.Contains(t, answer, "pending_user_approval")
	require.Len(t, pending, 1, "one approval for every step")
	approval := pending[0]
	require.Equal(t, shipAction, approval.Action)
	require.Equal(t, issue, approval.IssueID)
	var payload domain.ShipPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Len(t, payload.Steps, 4)
	require.Empty(t, merger.merged, "asking merges nothing")
	require.Empty(t, releases.created, "asking releases nothing")

	edited := "### Features\n- **Dark mode, edited on the card (ENG-1).**"
	result, err := f.service.ResolveApprovalDecision(f.ctx, f.ws, a.Conversation.ID, approval.ID, true, ModelChoice{}, &ApprovalEdit{Notes: &edited})
	require.NoError(t, err)
	require.Equal(t, "approving", result.Status, "approving answers before the chain's waits")

	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	done := h.Approvals[0]
	require.Equal(t, "approved", done.Status, "error: %v", done.Error)
	require.NoError(t, json.Unmarshal(done.Payload, &payload))
	for _, step := range payload.Steps {
		require.Equal(t, "done", step.Status, step.Step)
	}
	require.Equal(t, releaseSHA, payload.MergeCommit)
	require.Equal(t, []string{"acme/app#12@head1/squash"}, merger.merged)
	require.Len(t, releases.created, 1)
	require.Equal(t, edited, releases.created[0].Body, "the notes as edited on the card are published")
	require.Equal(t, releaseSHA, releases.created[0].TargetCommitish)

	var updates int
	require.NoError(t, f.db.Get(&updates, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='approval.updated'`, a.Conversation.ID))
	require.GreaterOrEqual(t, updates, 8, "the card is told as each step starts and ends")

	// Approving again repeats nothing.
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", again.Status)
	require.Len(t, merger.merged, 1)
	require.Len(t, releases.created, 1)

	// A chain a restart left approving resumes as its actor from its last step.
	_, err = f.db.Exec(`UPDATE conversation_approvals SET status='approving',resolved_at=NULL,payload=jsonb_set(payload,'{steps,3,status}','"running"') WHERE id=$1`, approval.ID)
	require.NoError(t, err)
	approvals.ResumeShips(f.ctx)
	h, err = f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "approved", h.Approvals[0].Status)
	require.Len(t, merger.merged, 1, "the merge is not repeated")
	require.Len(t, releases.created, 1, "the release is not published twice")

	// Another server process running the chain holds its lock, so this one
	// leaves the chain to it; once the lock is free, resuming finishes it.
	_, err = f.db.Exec(`UPDATE conversation_approvals SET status='approving',resolved_at=NULL,payload=jsonb_set(payload,'{steps,3,status}','"running"') WHERE id=$1`, approval.ID)
	require.NoError(t, err)
	unlock, held, err := f.repo.LockApprovalEffect(f.ctx, approval.ID)
	require.NoError(t, err)
	require.True(t, held)
	_, second, err := f.repo.LockApprovalEffect(f.ctx, approval.ID)
	require.NoError(t, err)
	require.False(t, second, "one process at a time holds a chain")
	approvals.ResumeShips(f.ctx)
	h, err = f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "approving", h.Approvals[0].Status, "the chain is left to the process holding it")
	unlock()
	approvals.ResumeShips(f.ctx)
	h, err = f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "approved", h.Approvals[0].Status)
	require.Len(t, merger.merged, 1)
	require.Len(t, releases.created, 1)
}
