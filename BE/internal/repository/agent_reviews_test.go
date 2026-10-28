package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func sessionOf(t *testing.T, db *sqlx.DB, task uuid.UUID) domain.AgentSession {
	t.Helper()
	var session domain.AgentSession
	require.NoError(t, db.Get(&session, `SELECT * FROM agent_sessions WHERE task_id=$1`, task))
	return session
}

// The session follows the task's own transitions, in their transactions, and
// a follow-up attempt continues the same implement session.
func TestAgentSessionsFollowTaskTransitions(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	session := sessionOf(t, db, task.ID)
	require.Equal(t, "implement", session.Purpose)
	require.Equal(t, "created", session.State)
	require.Equal(t, "codex", session.Provider)
	require.Nil(t, session.StartedAt)

	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	require.Equal(t, "starting", sessionOf(t, db, task.ID).State)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	session = sessionOf(t, db, task.ID)
	require.Equal(t, "active", session.State)
	require.NotNil(t, session.StartedAt)
	require.Equal(t, claimed.MachineID, session.MachineID)
	require.NoError(t, NewDevMachineRepository(db).RecordNativeSession(ctx, task.RunID, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"))
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	session = sessionOf(t, db, task.ID)
	require.Equal(t, "suspended", session.State, "the harness exited; a follow-up can resume it")
	require.NotNil(t, session.NativeSessionID)
	require.Equal(t, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", *session.NativeSessionID)

	// Another member, another workspace and an anonymous caller cannot read it.
	member := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, member, member.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, task.WorkspaceID, member)
	require.NoError(t, err)
	views, err := repo.TaskSessions(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Len(t, views.Sessions, 1)
	require.Equal(t, session.ID, views.Sessions[0].ID)
	require.Equal(t, "validating", views.Sessions[0].TaskState)
	_, err = repo.TaskSessions(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: member}), task.WorkspaceID, task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound, "tasks stay private to their creator")
	other := uuid.New()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, other, other.String(), member)
	require.NoError(t, err)
	_, err = repo.TaskSessions(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: other, UserID: task.CreatorID}), other, task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound, "a task is never read through another workspace")
	_, err = repo.TaskSessions(context.Background(), task.WorkspaceID, task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestFollowUpsReuseTheImplementSessionUntilCompletion(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	before := sessionOf(t, db, task.ID)
	require.Equal(t, "suspended", before.State)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: current.Generation, Prompt: "Follow up"})
	require.NoError(t, err)
	require.Equal(t, int64(2), retried.Generation)
	after := sessionOf(t, db, task.ID)
	require.Equal(t, before.ID, after.ID, "a follow-up continues the implement session")
	require.Equal(t, "starting", after.State)
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM agent_sessions WHERE task_id=$1`, task.ID))
	require.Equal(t, 1, count)
	// Simulate the follow-up being published again on the same pull request.
	_, err = db.Exec(`UPDATE agent_tasks SET state='awaiting_review',run_id=$2 WHERE id=$1`, task.ID, current.RunID)
	require.NoError(t, err)
	_, err = repo.CompleteMergedPullRequest(context.Background(), task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	done := sessionOf(t, db, task.ID)
	require.Equal(t, "completed", done.State)
	require.NotNil(t, done.EndedAt)
}

func reviewRequest(provider string) domain.AgentReviewRequest {
	return domain.AgentReviewRequest{RequestID: uuid.New(), Provider: provider, Focus: "Check error handling"}
}

// A review is requested from the requester's own issue chat for a task with a
// pull request Kuayle published, is idempotent per published commit and
// harness, runs as a review session and never validates or publishes.
func TestReviewsAreIdempotentRunWithoutPushAndRecordFindings(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	ws := task.WorkspaceID
	review, record, err := repo.CreateReview(ctx, ws, *task.ConversationID, task.ID, reviewRequest("claude-code"))
	require.NoError(t, err)
	require.Equal(t, domain.AgentTaskKindReview, review.Kind)
	require.Equal(t, task.ID, *review.ReviewOfTaskID)
	require.Equal(t, "claude-code", review.Provider)
	require.Equal(t, "queued", review.State)
	require.Contains(t, review.Prompt, publishedCommit)
	require.Contains(t, review.Prompt, "Check error handling")
	require.Equal(t, publishedCommit, record.CommitSHA)
	require.Equal(t, 12, record.PullRequestNumber)
	require.Equal(t, "fixture/repo", record.Repository)
	require.Equal(t, "main", record.BaseBranch)
	require.Equal(t, "not_requested", record.CommentState)
	session := sessionOf(t, db, review.ID)
	require.Equal(t, "review", session.Purpose)
	require.Equal(t, "created", session.State)

	again, _, err := repo.CreateReview(ctx, ws, *task.ConversationID, task.ID, reviewRequest("claude-code"))
	require.NoError(t, err)
	require.Equal(t, review.ID, again.ID, "one review per task, published commit and harness")
	codex, _, err := repo.CreateReview(ctx, ws, *task.ConversationID, task.ID, reviewRequest("codex"))
	require.NoError(t, err)
	require.NotEqual(t, review.ID, codex.ID, "another harness reviews independently")
	changed := reviewRequest("opencode")
	changed.ExpectedCommit = "ffffffffffffffffffffffffffffffffffffffff"
	_, _, err = repo.CreateReview(ctx, ws, *task.ConversationID, task.ID, changed)
	require.ErrorIs(t, err, ErrReviewTargetChanged)

	// Reviews never appear as tasks of their own in lists or agent work.
	tasks, err := repo.List(ctx, ws, *task.ConversationID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	page, err := repo.Page(ctx, ws, *task.ConversationID, nil)
	require.NoError(t, err)
	require.Len(t, page.Tasks, 1)
	work, err := repo.IssueAgentWork(ctx, ws, task.IssueID)
	require.NoError(t, err)
	require.Len(t, work, 1)
	require.NotNil(t, work[0].AgentReview)
	require.Contains(t, string(*work[0].AgentReview), `"state" : "queued"`)
	target, err := repo.MergeTarget(ctx, ws, &task.IssueID, nil)
	require.NoError(t, err)
	require.Equal(t, task.ID, target.TaskID)

	// Cancel the codex review so only the claude-code review is claimable.
	_, err = db.Exec(`UPDATE agent_tasks SET state='cancelled' WHERE id=$1`, codex.ID)
	require.NoError(t, err)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, review.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing review"))
	var machine uuid.UUID
	require.NoError(t, db.Get(&machine, `SELECT machine_id FROM agent_tasks WHERE id=$1`, task.ID))
	_, err = db.Exec(`UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, review.ID, machine)
	require.NoError(t, err)
	claimed.MachineID = &machine
	store := NewDevMachineRepository(db)
	dispatch := domain.WithTaskExecution(ctx, *claimed)
	run := func(push bool) *domain.DevMachineAgentRun {
		return &domain.DevMachineAgentRun{ID: review.RunID, WorkspaceID: ws, MachineID: machine, IssueID: &task.IssueID, RequestedByUserID: &task.CreatorID, ProviderID: "claude-code", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, Prompt: review.Prompt, AcceptanceCriteria: []byte(`[]`), AllowedCommands: []byte(`[]`), ForbiddenPaths: []byte(`[]`), AllowedSecrets: []byte(`[]`), CommandArgv: []byte(`["claude"]`), MaxRuntimeSeconds: 60, PushBranch: push}
	}
	op := func() *domain.DevMachineOperation {
		return &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: ws, MachineID: machine, Action: domain.DevMachineOpRunAgent, Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "run-agent:" + review.RunID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 3}
	}
	require.ErrorIs(t, store.CreateAgentRun(dispatch, run(true), op()), ErrAgentTaskUnauthorized, "a review run never receives a push credential")
	created := run(false)
	require.NoError(t, store.CreateAgentRun(dispatch, created, op()))
	require.NotNil(t, created.SessionID)
	require.Equal(t, session.ID, *created.SessionID, "the attempt runs inside the review session")

	var issueStatus uuid.UUID
	require.NoError(t, db.Get(&issueStatus, `SELECT status_id FROM issues WHERE id=$1`, task.IssueID))
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='running',started_at=now() WHERE id=$1`, review.RunID)
	require.NoError(t, err)
	changedIssue, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	require.Nil(t, changedIssue, "a review never moves the issue")
	require.Equal(t, "active", sessionOf(t, db, review.ID).State)
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "validating", "Reviewer finished"), ErrAgentTaskConflict, "a review is never validated")
	require.ErrorIs(t, repo.CompleteReview(ctx, *claimed), ErrAgentTaskConflict, "findings need a succeeded attempt")

	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),summary=$2 WHERE id=$1`, review.RunID, "**Verdict:** request_changes\n\n- `app.go:12` major: handle the error")
	require.NoError(t, err)
	require.NoError(t, repo.CompleteReview(ctx, *claimed))
	finished, err := repo.Get(ctx, ws, review.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", finished.State)
	views, err := repo.TaskSessions(ctx, ws, task.ID)
	require.NoError(t, err)
	var found *domain.AgentSessionView
	for i := range views.Sessions {
		if views.Sessions[i].TaskID == review.ID {
			found = &views.Sessions[i]
		}
	}
	require.NotNil(t, found)
	require.Equal(t, "completed", found.State)
	require.NotNil(t, found.Review)
	require.Equal(t, "request_changes", *found.Review.Verdict)
	require.Equal(t, "- `app.go:12` major: handle the error", *found.Review.Findings)
	var events int
	require.NoError(t, db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.review_updated'`, *task.ConversationID))
	require.Positive(t, events)
	var publications int
	require.NoError(t, db.Get(&publications, `SELECT count(*) FROM dev_machine_operations WHERE agent_run_id=$1 AND action IN ('publish_agent','validate_agent')`, review.RunID))
	require.Zero(t, publications)
	reviewed, err := repo.Get(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", reviewed.State, "reviewing leaves the reviewed task as it was")
	_, err = repo.Retry(ctx, ws, review.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: 1, Prompt: "Again"})
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a review is requested again, never retried")

	// No comment was requested, so nothing is ever due.
	comment, err := repo.ClaimReviewComment(context.Background(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, comment)
}

func TestReviewRequestsRequireAPublishedPullRequestAndIssueAccess(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	ws := task.WorkspaceID
	_, _, err := repo.CreateReview(ctx, ws, *task.ConversationID, task.ID, reviewRequest("codex"))
	require.ErrorIs(t, err, ErrNoReviewTarget, "a task without a published pull request cannot be reviewed")

	db, repo, ctx, task = reviewableTask(t)
	ws = task.WorkspaceID
	member := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, member, member.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, member)
	require.NoError(t, err)
	memberCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: member})
	chat, err := NewConversationRepository(db).OpenIssue(memberCtx, ws, task.IssueID)
	require.NoError(t, err)
	// Another member who can see the issue may review it from their own chat.
	review, record, err := repo.CreateReview(memberCtx, ws, chat.ID, task.ID, reviewRequest("codex"))
	require.NoError(t, err)
	require.Equal(t, member, review.CreatorID)
	require.Equal(t, member, record.RequestedBy)
	_, _, err = repo.CreateReview(memberCtx, ws, *task.ConversationID, task.ID, reviewRequest("opencode"))
	require.ErrorIs(t, err, ErrConversationNotFound, "the creator's chat is not the member's")

	// A chat about another issue cannot target this issue's task. (Private
	// team visibility is enforced by the same conversation scope check.)
	otherIssue := uuid.New()
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,2,'REC-2','Other',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, otherIssue, ws, task.TeamID, member)
	require.NoError(t, err)
	otherChat, err := NewConversationRepository(db).OpenIssue(memberCtx, ws, otherIssue)
	require.NoError(t, err)
	_, _, err = repo.CreateReview(memberCtx, ws, otherChat.ID, task.ID, reviewRequest("opencode"))
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, ws, member)
	require.NoError(t, err)
	_, _, err = repo.CreateReview(memberCtx, ws, chat.ID, task.ID, reviewRequest("opencode"))
	require.ErrorIs(t, err, ErrConversationNotFound, "guests cannot request reviews")
}

// A requested pull request comment is due only after the review completed, is
// claimed by one poster at a time and is recorded once.
func TestReviewCommentsAreDueOnlyWhenRequestedAndFinished(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	request := reviewRequest("codex")
	request.PostToPullRequest = true
	review, record, err := repo.CreateReview(ctx, task.WorkspaceID, *task.ConversationID, task.ID, request)
	require.NoError(t, err)
	require.Equal(t, "pending", record.CommentState)
	claim, err := repo.ClaimReviewComment(context.Background(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, claim, "nothing is posted before the review has findings")
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, review.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_reviews SET findings='No findings.',verdict='approve' WHERE task_id=$1`, review.ID)
	require.NoError(t, err)
	claim, err = repo.ClaimReviewComment(context.Background(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, "REC-1", claim.IssueIdentifier)
	again, err := repo.ClaimReviewComment(context.Background(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, again, "a leased comment is not claimed twice")
	require.NoError(t, repo.FinishReviewComment(context.Background(), claim.Review, true))
	require.ErrorIs(t, repo.FinishReviewComment(context.Background(), claim.Review, true), ErrAgentTaskConflict)
	var state string
	require.NoError(t, db.Get(&state, `SELECT comment_state FROM agent_reviews WHERE task_id=$1`, review.ID))
	require.Equal(t, "posted", state)

	// A requester who lost access never gets a comment posted.
	second := reviewRequest("claude-code")
	second.PostToPullRequest = true
	other, _, err := repo.CreateReview(ctx, task.WorkspaceID, *task.ConversationID, task.ID, second)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, other.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_reviews SET findings='x' WHERE task_id=$1`, other.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	claim, err = repo.ClaimReviewComment(context.Background(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, claim)
	require.NoError(t, db.Get(&state, `SELECT comment_state FROM agent_reviews WHERE task_id=$1`, other.ID))
	require.Equal(t, "failed", state)
}
