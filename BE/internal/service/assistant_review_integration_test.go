package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// reviewTaskFake uses the real task repository for reviews and follow-ups and
// keeps Dev Machine policy out of the test, like approvalTaskFake.
type reviewTaskFake struct {
	*approvalTaskFake
	reviews int
}

func (f *reviewTaskFake) RequestReview(ctx context.Context, ws, conversation, reviewed uuid.UUID, input domain.AgentReviewRequest) (*domain.AgentTask, *domain.AgentReview, error) {
	f.reviews++
	return f.repo.CreateReview(ctx, ws, conversation, reviewed, input)
}
func (f *reviewTaskFake) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	return f.repo.Get(ctx, ws, id)
}
func (f *reviewTaskFake) Retry(ctx context.Context, ws, id uuid.UUID, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	return f.repo.Retry(ctx, ws, id, input)
}

const reviewedCommit = "0123456789abcdef0123456789abcdef01234567"

// publishTask records what publication would: the task awaits review with a
// pull request and commit Kuayle published.
func publishTask(t *testing.T, f toolFixture, task uuid.UUID) {
	t.Helper()
	machine := uuid.New()
	_, err := f.db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,$3,$4,'Review','running','running',1,'','github','','','','','small',1000,2048,20)`, machine, f.ws, f.user, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	var run uuid.UUID
	require.NoError(t, f.db.Get(&run, `UPDATE agent_tasks SET state='awaiting_review',machine_id=$2 WHERE id=$1 RETURNING run_id`, task, machine))
	_, err = f.db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,$4,'codex','autonomous','Implement','["codex"]',60,'succeeded')`, run, f.ws, machine, task)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source) VALUES
 ($1,$2,$3,'pull_request','acme/app','kuayle/eng-1',NULL,12,'https://github.com/acme/app/pull/12','published'),
 ($1,$2,$3,'commit','acme/app','kuayle/eng-1',$4,NULL,'https://github.com/acme/app/commit/x','published')`, f.ws, machine, run, reviewedCommit)
	require.NoError(t, err)
}

// Reviews start only after the user approves the request in chat, are pinned
// to the published commit and idempotent, and a follow-up with the findings
// continues the reviewed task through its retry path.
func TestRequestedReviewsNeedApprovalAndFollowUpsContinueTheTask(t *testing.T) {
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,2,'ENG-2','No work yet',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
	require.NoError(t, err)

	a, delegation, tasks := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	taskID := *started.TaskID
	publishTask(t, f, taskID)
	work := repository.NewAgentTaskRepository(f.db)
	reviews := &reviewTaskFake{approvalTaskFake: tasks}
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetMerging(work, repository.NewWorkspaceRepository(f.db), &mergerFake{})
	f.service.approvals.SetReviews(reviews)

	ask := func(tool, arguments string) (domain.ConversationAttempt, []domain.ConversationApproval, []map[string]any) {
		t.Helper()
		a := f.attempt(t, "Review please")
		provider, requests := scriptedProvider(t, toolCallStream(tool, arguments), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Review"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		return a, h.Approvals, *requests
	}
	countReviews := func() int {
		var n int
		require.NoError(t, f.db.Get(&n, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1 AND kind='review'`, f.ws))
		return n
	}

	a, approvals, requests := ask("request_agent_review", `{"identifier":"eng-1","harness":"codex","focus":"Look at error paths","post_to_pull_request":true}`)
	require.Len(t, approvals, 1)
	history := requests[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
	require.Zero(t, countReviews(), "requesting a review starts nothing")
	var payload domain.ReviewPayload
	require.NoError(t, json.Unmarshal(approvals[0].Payload, &payload))
	require.Equal(t, domain.ReviewPayload{Identifier: "ENG-1", Title: "Dark mode", TaskID: taskID, Harness: "codex", Focus: "Look at error paths", PostToPullRequest: true, PullRequest: 12, PullRequestURL: "https://github.com/acme/app/pull/12", Commit: reviewedCommit}, payload)
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", result.Status)
	require.NotNil(t, result.TaskID)
	require.Equal(t, 1, countReviews())
	var review domain.AgentReview
	require.NoError(t, f.db.Get(&review, `SELECT * FROM agent_reviews WHERE task_id=$1`, *result.TaskID))
	require.Equal(t, taskID, review.ReviewedTaskID)
	require.Equal(t, reviewedCommit, review.CommitSHA)
	require.True(t, review.PostToPullRequest)
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, *result.TaskID, *again.TaskID)

	// Asking again for the same commit and harness converges on that review.
	a, approvals, _ = ask("request_agent_review", `{"task_id":"`+taskID.String()+`"}`)
	require.Len(t, approvals, 1)
	require.NoError(t, json.Unmarshal(approvals[0].Payload, &payload))
	require.Equal(t, "codex", payload.Harness, "the default harness is the implementer's")
	require.False(t, payload.PostToPullRequest, "nothing is posted unless asked")
	result2, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, *result.TaskID, *result2.TaskID)
	require.Equal(t, 1, countReviews())

	// A harness the workspace does not allow fails the approval permanently.
	a, approvals, _ = ask("request_agent_review", `{"identifier":"ENG-1","harness":"claude-code"}`)
	failed, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Contains(t, *failed.Error, "not available")
	require.Equal(t, 1, countReviews())

	for arguments, message := range map[string]string{
		`{"identifier":"ENG-2"}`:                 "no pull request awaiting review",
		`{"task_id":"` + uuid.NewString() + `"}`: "no pull request awaiting review",
		`{}`:                                     "issue identifier or the task id",
		`{"identifier":"ENG-1","harness":"other-agent"}`: "invalid harness",
	} {
		_, _, err = f.service.tools.prepareReview(f.ctx, f.ws, f.user, arguments)
		require.ErrorContains(t, err, message, arguments)
	}

	// A follow-up with the findings continues the reviewed task.
	a, approvals, _ = ask("follow_up_task", `{"identifier":"ENG-1","instructions":"Fix the review findings: handle the error in app.go"}`)
	require.Len(t, approvals, 1)
	var followUp domain.FollowUpPayload
	require.NoError(t, json.Unmarshal(approvals[0].Payload, &followUp))
	require.Equal(t, taskID, followUp.TaskID)
	require.Equal(t, int64(1), followUp.Generation)
	current, err := work.Get(f.ctx, f.ws, taskID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", current.State, "requesting a follow-up starts nothing")
	result, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, taskID, *result.TaskID)
	current, err = work.Get(f.ctx, f.ws, taskID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Equal(t, int64(2), current.Generation)
	require.True(t, strings.HasPrefix(current.Prompt, "Implement dark mode and add tests"))
	require.Contains(t, current.Prompt, "Follow-up from Kuayle chat")
	require.Contains(t, current.Prompt, "handle the error in app.go")
	_, _, err = f.service.tools.prepareFollowUp(f.ctx, f.ws, f.user, `{"identifier":"ENG-1","instructions":"Again"}`)
	require.ErrorContains(t, err, "the task is queued")

	// Guests can request neither.
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, _, err = f.service.tools.prepareReview(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "no longer have access")
	_, _, err = f.service.tools.prepareFollowUp(f.ctx, f.ws, f.user, `{"identifier":"ENG-1","instructions":"x"}`)
	require.ErrorContains(t, err, "no longer have access")
}

// Another team's private issue is invisible to review and follow-up requests.
// (A workspace with private teams has no Dev Machines, so it has no tasks.)
func TestReviewRequestsDoNotRevealPrivateTeamIssues(t *testing.T) {
	f := newToolFixture(t)
	private := uuid.New()
	_, err := f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','SEC',true)`, private, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Backlog','backlog','backlog')`, private)
	require.NoError(t, err)
	var owner uuid.UUID
	require.NoError(t, f.db.Get(&owner, `SELECT owner_id FROM workspaces WHERE id=$1`, f.ws))
	_, err = f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'SEC-1','Hidden',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, private, owner)
	require.NoError(t, err)
	f.service.tools.SetAgentWork(repository.NewAgentTaskRepository(f.db))
	_, _, err = f.service.tools.prepareReview(f.ctx, f.ws, f.user, `{"identifier":"SEC-1"}`)
	require.ErrorContains(t, err, "issue not found")
	_, _, err = f.service.tools.prepareFollowUp(f.ctx, f.ws, f.user, `{"identifier":"SEC-1","instructions":"Fix it"}`)
	require.ErrorContains(t, err, "issue not found")
}
