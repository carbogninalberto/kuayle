package repository

import (
	"context"
	"fmt"
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

func TestPreviewRoutesToTheRunningDeveloperServiceOnItsPort(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	_ = repo
	store := NewDevMachineRepository(db)
	var machine domain.DevMachine
	require.NoError(t, db.Get(&machine, `SELECT m.* FROM dev_machines m JOIN agent_tasks t ON t.machine_id=m.id WHERE t.id=$1`, task.ID))
	_, route, err := store.GetRoute(ctx, machine.RoutingKey, "preview")
	require.NoError(t, err)
	require.Nil(t, route, "no preview configured")
	ide := &domain.DevMachineService{ID: uuid.New(), WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, ServiceType: "ide", ServiceKey: "ide", ContainerName: "ide", ImageRef: "fixture", InternalHost: "ide", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}
	require.NoError(t, store.CreateRuntimeService(ctx, ide))
	require.NoError(t, store.UpdateServiceRuntime(ctx, ide.ID, "container", "running", "healthy", nil))
	preview := &domain.DevMachinePreview{MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Port: 5173, CreatedByUserID: task.CreatorID}
	require.NoError(t, store.SetPreview(ctx, preview))
	_, route, err = store.GetRoute(ctx, machine.RoutingKey, "preview")
	require.NoError(t, err)
	require.NotNil(t, route)
	require.Equal(t, ide.ID, route.ID)
	require.Equal(t, "preview", route.ServiceType)
	require.Equal(t, 5173, route.InternalPort)
	require.Equal(t, "ide", route.InternalHost)
	_, ideRoute, err := store.GetRoute(ctx, machine.RoutingKey, "ide")
	require.NoError(t, err)
	require.Equal(t, 8080, ideRoute.InternalPort, "the IDE route is unchanged")
	require.NoError(t, store.DeletePreview(ctx, machine.WorkspaceID, machine.ID, machine.RoutingKey+"-preview.example.test"))
	_, route, err = store.GetRoute(ctx, machine.RoutingKey, "preview")
	require.NoError(t, err)
	require.Nil(t, route)
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
