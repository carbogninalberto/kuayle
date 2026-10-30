package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

const publishedCommit = "0123456789abcdef0123456789abcdef01234567"

// reviewableTask publishes a pull request and moves the task to review.
func reviewableTask(t *testing.T) (*sqlx.DB, *AgentTaskRepository, context.Context, domain.AgentTask) {
	t.Helper()
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	require.NoError(t, store.StartPublication(ctx, op))
	require.NoError(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: publishedCommit, PullRequestNumber: 12, PullRequestURL: "https://github.com/fixture/repo/pull/12"}))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	require.NoError(t, repo.Transition(ctx, *claimed, "awaiting_review", "Published"))
	return db, repo, ctx, task
}

func ci(id, outcome, sha string) domain.AgentTaskSignalInput {
	return domain.AgentTaskSignalInput{Kind: "ci", ExternalID: id, Outcome: outcome, Name: "Tests", HeadSHA: sha, URL: "https://github.com/fixture/repo/pull/12/checks"}
}

func TestCISignalsFollowUpOnlyForThePublishedCommitWithinBudget(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	background := context.Background()
	followUps, err := repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("1", "success", publishedCommit))
	require.NoError(t, err)
	require.Empty(t, followUps)
	followUps, err = repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("2", "failure", "ffffffffffffffffffffffffffffffffffffffff"))
	require.NoError(t, err)
	require.Empty(t, followUps, "CI for another commit is stale")
	for _, other := range []struct {
		ws     uuid.UUID
		repo   string
		number int
	}{{uuid.New(), "fixture/repo", 12}, {task.WorkspaceID, "other/repo", 12}, {task.WorkspaceID, "fixture/repo", 13}} {
		followUps, err = repo.RecordSignal(background, other.ws, other.repo, other.number, ci("3", "failure", publishedCommit))
		require.NoError(t, err)
		require.Empty(t, followUps)
	}
	followUps, err = repo.RecordSignal(background, task.WorkspaceID, "Fixture/Repo", 12, ci("4", "failure", publishedCommit))
	require.NoError(t, err)
	require.Len(t, followUps, 1)
	first := followUps[0]
	require.Equal(t, "follow_up", first.Signal.Action)
	require.Equal(t, task.ID, first.Task.ID)
	// A redelivery before the follow-up was queued resumes the same request.
	again, err := repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("4", "failure", publishedCommit))
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, first.Signal.RequestID, again[0].Signal.RequestID)
	generation := int64(2)
	require.NoError(t, repo.FinishFollowUp(background, first.Signal.ID, &generation))
	again, err = repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("4", "failure", publishedCommit))
	require.NoError(t, err)
	require.Empty(t, again, "a queued follow-up is never requested twice")
	followUps, err = repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("5", "timed_out", publishedCommit))
	require.NoError(t, err)
	require.Len(t, followUps, 1)
	followUps, err = repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("6", "failure", publishedCommit))
	require.NoError(t, err)
	require.Empty(t, followUps)
	signals, err := repo.Signals(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	actions := map[string]string{}
	for _, s := range signals {
		actions[s.ExternalID] = s.Action
	}
	require.Equal(t, map[string]string{"1": "recorded", "4": "follow_up", "5": "follow_up", "6": "budget_exhausted"}, actions)
	var events int
	require.NoError(t, db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.signal_recorded'`, task.ConversationID))
	require.Equal(t, 5, events)
	stranger := domain.WithAccess(background, domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Signals(stranger, task.WorkspaceID, task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestReviewSignalsRequireTrustedChangeRequestsWhileReviewable(t *testing.T) {
	_, repo, ctx, task := reviewableTask(t)
	background := context.Background()
	review := func(id, outcome string, trusted bool) []domain.AgentTaskFollowUp {
		followUps, err := repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, domain.AgentTaskSignalInput{Kind: "review", ExternalID: id, Outcome: outcome, Name: "reviewer", Summary: "Please rename", Trusted: trusted})
		require.NoError(t, err)
		return followUps
	}
	require.Empty(t, review("1", "approved", true))
	require.Empty(t, review("2", "changes_requested", false), "outside contributors cannot steer the agent")
	require.Len(t, review("3", "changes_requested", true), 1)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	_, err = repo.CompleteMergedPullRequest(background, task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	require.Empty(t, review("4", "changes_requested", true), "a completed task is not reopened")
	signals, err := repo.Signals(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	actions := map[string]string{}
	for _, s := range signals {
		actions[s.ExternalID] = s.Action
	}
	require.Equal(t, map[string]string{"1": "recorded", "2": "untrusted", "3": "follow_up", "4": "not_reviewable"}, actions, fmt.Sprint(current.State))
}

func TestPendingFollowUpsResumeOnlyStaleUnqueuedSignals(t *testing.T) {
	db, repo, _, task := reviewableTask(t)
	background := context.Background()
	followUps, err := repo.RecordSignal(background, task.WorkspaceID, "fixture/repo", 12, ci("9", "failure", publishedCommit))
	require.NoError(t, err)
	require.Len(t, followUps, 1)
	signal := followUps[0].Signal
	pending, err := repo.PendingFollowUps(background, 10)
	require.NoError(t, err)
	require.Empty(t, pending, "a follow-up still being requested is left alone")
	_, err = db.Exec(`UPDATE agent_task_signals SET updated_at=now()-interval '1 minute' WHERE id=$1`, signal.ID)
	require.NoError(t, err)
	pending, err = repo.PendingFollowUps(background, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, signal.RequestID, pending[0].Signal.RequestID)
	_, err = db.Exec(`UPDATE agent_tasks SET run_id=gen_random_uuid() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	pending, err = repo.PendingFollowUps(background, 10)
	require.NoError(t, err)
	require.Empty(t, pending, "a replaced attempt makes the follow-up obsolete")
	var action string
	require.NoError(t, db.Get(&action, `SELECT action FROM agent_task_signals WHERE id=$1`, signal.ID))
	require.Equal(t, "follow_up_failed", action)
}

func TestRetryFindsThePreviousAttemptsRecordedOutcome(t *testing.T) {
	_, repo, ctx, task := reviewableTask(t)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	none, err := repo.PreviousAttempt(ctx, *current)
	require.NoError(t, err)
	require.Nil(t, none, "a first attempt has no predecessor")
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: current.Generation, Prompt: "Address review feedback"})
	require.NoError(t, err)
	require.NotEqual(t, current.RunID, retried.RunID)
	previous, err := repo.PreviousAttempt(ctx, *retried)
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.Equal(t, current.RunID, previous.AttemptID)
	require.Equal(t, "succeeded", previous.Status)
	require.Equal(t, "succeeded", previous.Validation)
	require.Equal(t, 0, *previous.ValidationExitCode)
	require.Equal(t, "https://github.com/fixture/repo/pull/12", previous.PullRequestURL)
}

func TestDeferredOperationsWaitWithoutConsumingAttempts(t *testing.T) {
	db, repo, ctx, _, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	require.Equal(t, 1, op.Attempts)
	require.NoError(t, store.DeferOperation(ctx, op.ID, *op.LeaseOwner, "host_capacity_wait", "waiting for host memory", time.Minute))
	var row struct {
		Status   string `db:"status"`
		Attempts int    `db:"attempts"`
		Message  string `db:"error_message"`
		Future   bool   `db:"future"`
	}
	require.NoError(t, db.Get(&row, `SELECT status,attempts,error_message,available_at>now() AS future FROM dev_machine_operations WHERE id=$1`, op.ID))
	require.Equal(t, "pending", row.Status)
	require.Zero(t, row.Attempts)
	require.Equal(t, "waiting for host memory", row.Message)
	require.True(t, row.Future)
	again, err := store.LeaseOperations(ctx, "other", 5, time.Minute)
	require.NoError(t, err)
	require.Empty(t, again, "a waiting operation is not leased before its retry time")
	require.NoError(t, store.DeferOperation(ctx, op.ID, "stale-owner", "x", "y", time.Minute), "a non-owner cannot change it")
	allocations, err := store.ListResourceAllocations(ctx)
	require.NoError(t, err)
	found := false
	for _, a := range allocations {
		found = found || a.MachineID == *claimed.MachineID
	}
	require.True(t, found, "a running machine holds its allocation")
}

func TestNativeSessionsAreRecordedOnceAndHandedToTheSuccessor(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	store := NewDevMachineRepository(db)
	require.NoError(t, store.RecordNativeSession(ctx, task.RunID, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"))
	require.NoError(t, store.RecordNativeSession(ctx, task.RunID, "ffffffff-c3d4-7e5f-8a9b-0c1d2e3f4a5b"), "a later report never replaces it")
	_, err := db.Exec(`UPDATE dev_machine_agent_runs SET native_session_id='--last' WHERE id=$1`, task.RunID)
	require.Error(t, err, "the schema rejects unsafe session IDs")
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: current.Generation, Prompt: "Follow up"})
	require.NoError(t, err)
	previous, err := repo.PreviousAttempt(ctx, *retried)
	require.NoError(t, err)
	require.Equal(t, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", previous.NativeSessionID)
	require.Equal(t, *current.MachineID, previous.MachineID)
	var purges int
	require.NoError(t, db.Get(&purges, `SELECT count(*) FROM dev_machine_operations WHERE action='purge_task_session'`))
	require.Zero(t, purges, "a task still in progress keeps its sessions")
}

func TestCompletedTasksQueueOnePurgeOfTheirTranscripts(t *testing.T) {
	db, repo, _, task := reviewableTask(t)
	n, err := repo.CompleteMergedPullRequest(context.Background(), task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	var purges int
	require.NoError(t, db.Get(&purges, `SELECT count(*) FROM dev_machine_operations WHERE action='purge_task_session' AND checkout_id=$1 AND status='pending'`, task.CheckoutID))
	require.Equal(t, 1, purges)
}

func TestQueueListsOnlyTheActorsVisibleTasks(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	items, err := repo.Queue(ctx, task.WorkspaceID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, task.ID, items[0].ID)
	require.Equal(t, "REC-1", items[0].IssueIdentifier)
	require.Equal(t, "awaiting_review", items[0].State)
	var machine uuid.UUID
	require.NoError(t, db.Get(&machine, `SELECT machine_id FROM agent_tasks WHERE id=$1`, task.ID))
	require.Equal(t, machine, *items[0].MachineID, "the dock can open a terminal on the task's machine")
	require.NotNil(t, items[0].MachineName)
	require.Equal(t, task.CheckoutID, *items[0].CheckoutID)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Queue(stranger, task.WorkspaceID)
	require.Error(t, err, "non-members cannot list tasks")
	// Moving the issue to another team closes the old chat's scope, and its task.
	other := uuid.New()
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Other','OTH')`, other, task.WorkspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET team_id=$2 WHERE id=$1`, task.IssueID, other)
	require.NoError(t, err)
	items, err = repo.Queue(ctx, task.WorkspaceID)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestAgentDefaultsInheritAndAreSnapshotted(t *testing.T) {
	db, repo, ctx, task, _ := publishingTask(t)
	store := NewDevMachineRepository(db)
	provider, err := repo.DefaultProvider(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Empty(t, provider)
	claude, opencode, teamRules, issueRules := "claude-code", "opencode", "Use pnpm.", "Only touch the API."
	require.NoError(t, store.UpsertScopeSetting(ctx, &domain.DevMachineScopeSetting{ID: uuid.New(), WorkspaceID: task.WorkspaceID, TeamID: &task.TeamID, AgentProvider: &claude, AgentInstructions: &teamRules}))
	provider, err = repo.DefaultProvider(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Equal(t, "claude-code", provider)
	require.NoError(t, store.UpsertScopeSetting(ctx, &domain.DevMachineScopeSetting{ID: uuid.New(), WorkspaceID: task.WorkspaceID, IssueID: &task.IssueID, AgentProvider: &opencode, AgentInstructions: &issueRules}))
	provider, err = repo.DefaultProvider(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Equal(t, "opencode", provider, "the issue setting overrides the team")
	var created string
	require.NoError(t, db.Get(&created, `SELECT agent_instructions FROM agent_tasks WHERE id=$1`, task.ID))
	require.Empty(t, created, "a task accepted before the setting keeps its snapshot")
	second, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Second task", Provider: "codex"})
	require.NoError(t, err)
	require.Equal(t, "Only touch the API.", second.AgentInstructions, "new tasks snapshot the nearest instructions")
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET agent_instructions='x' WHERE issue_id=$1`, task.IssueID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET agent_instructions='' WHERE issue_id=$1`, task.IssueID)
	require.Error(t, err, "empty instructions are stored as NULL, never ''")
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET agent_provider='Bad Provider' WHERE issue_id=$1`, task.IssueID)
	require.Error(t, err)
}

func TestReviewPullRequestsListOnlyTasksAwaitingReview(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	pulls, err := repo.ReviewPullRequests(ctx, 50)
	require.NoError(t, err)
	found := false
	for _, pull := range pulls {
		if pull.WorkspaceID == task.WorkspaceID {
			found = true
			require.NotZero(t, pull.Number)
			require.NotEmpty(t, pull.Repository)
		}
	}
	require.True(t, found, "a task in review is reconciled")
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	pulls, err = repo.ReviewPullRequests(ctx, 50)
	require.NoError(t, err)
	for _, pull := range pulls {
		require.NotEqual(t, task.WorkspaceID, pull.WorkspaceID, "finished tasks are not checked again")
	}
}

func TestAttemptDiffsAreReadableOnlyWithTheTask(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	_, err := db.Exec(`UPDATE dev_machine_agent_runs SET diff='diff --git a/x b/x',diff_truncated=true WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	diff, err := repo.Diff(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Equal(t, "diff --git a/x b/x", diff.Diff)
	require.True(t, diff.Truncated)
	_, err = repo.Diff(ctx, task.WorkspaceID, task.ID, uuid.New())
	require.ErrorIs(t, err, ErrConversationNotFound, "only the task's own attempts")
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Diff(stranger, task.WorkspaceID, task.ID, task.RunID)
	require.Error(t, err, "non-members cannot read the diff")
}

func TestCommentSignalsFollowUpOnlyForVisibleMembersWhileReviewable(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	background := context.Background()
	member, guest, outsider := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{member, guest, outsider} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Reviewer','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member'),($1,$3,'guest')`, task.WorkspaceID, member, guest)
	require.NoError(t, err)
	comment := func(ws uuid.UUID, id, author uuid.UUID) *domain.AgentTaskFollowUp {
		t.Helper()
		followUp, err := repo.RecordCommentSignal(background, ws, task.IssueID, id, author, "Please rename the flag")
		require.NoError(t, err)
		return followUp
	}
	require.Nil(t, comment(task.WorkspaceID, uuid.New(), guest), "guests cannot steer the agent")
	require.Nil(t, comment(task.WorkspaceID, uuid.New(), outsider), "non-members cannot steer the agent")
	require.Nil(t, comment(uuid.New(), uuid.New(), member), "another workspace never matches")

	first := uuid.New()
	followUp := comment(task.WorkspaceID, first, member)
	require.NotNil(t, followUp)
	require.Equal(t, "comment", followUp.Signal.Kind)
	require.Equal(t, "follow_up", followUp.Signal.Action)
	require.Equal(t, "Reviewer", followUp.Signal.Name)
	require.Equal(t, "Please rename the flag", followUp.Signal.Summary)
	require.Equal(t, task.ID, followUp.Task.ID)
	again := comment(task.WorkspaceID, first, member)
	require.NotNil(t, again, "a follow-up recorded but not queued is resumed")
	require.Equal(t, followUp.Signal.RequestID, again.Signal.RequestID, "the same comment replays the same request")
	generation := int64(2)
	require.NoError(t, repo.FinishFollowUp(background, followUp.Signal.ID, &generation))
	require.Nil(t, comment(task.WorkspaceID, first, member), "a queued follow-up is never requested twice")
	for range CommentFollowUpBudget - 1 {
		require.NotNil(t, comment(task.WorkspaceID, uuid.New(), member))
	}
	exhausted := uuid.New()
	require.Nil(t, comment(task.WorkspaceID, exhausted, member))

	// Workspaces with Dev Machines cannot have private teams, so issue
	// visibility reduces to membership here; demotion is re-checked each time.
	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, member)
	require.NoError(t, err)
	demoted := uuid.New()
	require.Nil(t, comment(task.WorkspaceID, demoted, member))
	_, err = db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, member)
	require.NoError(t, err)

	signals, err := repo.Signals(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	actions := map[string]string{}
	for _, s := range signals {
		require.Equal(t, "comment", s.Kind)
		actions[s.ExternalID] = s.Action
	}
	require.Len(t, actions, CommentFollowUpBudget+1, "ignored comments are not recorded")
	require.Equal(t, "budget_exhausted", actions[exhausted.String()])
	require.NotContains(t, actions, demoted.String())

	_, err = repo.CompleteMergedPullRequest(background, task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	require.Nil(t, comment(task.WorkspaceID, uuid.New(), task.CreatorID), "a completed task is not reopened")
}

func TestMachineTasksListOnlyTheOwnersVisibleTasksOnThatMachine(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	var machine uuid.UUID
	require.NoError(t, db.Get(&machine, `SELECT machine_id FROM agent_tasks WHERE id=$1`, task.ID))
	items, err := repo.MachineTasks(ctx, task.WorkspaceID, machine)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, task.ID, items[0].ID)
	require.Equal(t, *task.ConversationID, items[0].ConversationID, "each task links to its chat")
	require.Equal(t, "REC-1", items[0].IssueIdentifier)
	require.Equal(t, "awaiting_review", items[0].State)
	require.NotEmpty(t, items[0].Provider)
	// Another machine of the same owner lists none of this machine's tasks.
	var owner uuid.UUID
	require.NoError(t, db.Get(&owner, `SELECT created_by_user_id FROM dev_machines WHERE id=$1`, machine))
	otherMachine := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,$3,$4,'Other machine','running','running',1,'','github','','','','','small',1000,2048,20)`, otherMachine, task.WorkspaceID, owner, strings.ReplaceAll(otherMachine.String(), "-", "")[:16])
	require.NoError(t, err)
	items, err = repo.MachineTasks(ctx, task.WorkspaceID, otherMachine)
	require.NoError(t, err)
	require.Empty(t, items)
	// A workspace member who does not own the machine cannot list its tasks.
	member := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','x')`, member, member.String()+"@example.test")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, task.WorkspaceID, member)
	require.NoError(t, err)
	memberCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: member})
	_, err = repo.MachineTasks(memberCtx, task.WorkspaceID, machine)
	require.ErrorIs(t, err, ErrAgentTaskUnauthorized)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.MachineTasks(stranger, task.WorkspaceID, machine)
	require.Error(t, err, "non-members cannot list tasks")
	_, err = repo.MachineTasks(ctx, uuid.New(), machine)
	require.Error(t, err, "the machine is scoped to its workspace")
	// Moving the issue to a team the owner cannot see hides its task.
	other := uuid.New()
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Other','OTH')`, other, task.WorkspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET team_id=$2 WHERE id=$1`, task.IssueID, other)
	require.NoError(t, err)
	items, err = repo.MachineTasks(ctx, task.WorkspaceID, machine)
	require.NoError(t, err)
	require.Empty(t, items)
}
