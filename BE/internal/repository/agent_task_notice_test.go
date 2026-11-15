package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTaskTransitionNotifiesOnlyFinishedAndReviewStates(t *testing.T) {
	creator, other := uuid.New(), uuid.New()
	for state, want := range map[string]bool{
		"completed": true, "failed": true, "awaiting_review": true,
		"queued": false, "running": false, "validating": false, "publishing": false, "blocked": false, "cancelling": false, "cancelled": false,
	} {
		require.Equal(t, want, taskTransitionNotifies(domain.AgentTask{State: state, CreatorID: creator}, nil), state)
	}
	// Someone else's change is news to the creator; their own is not.
	require.True(t, taskTransitionNotifies(domain.AgentTask{State: "completed", CreatorID: creator}, &other))
	require.False(t, taskTransitionNotifies(domain.AgentTask{State: "completed", CreatorID: creator}, &creator))
	require.False(t, taskTransitionNotifies(domain.AgentTask{State: "completed"}, nil), "no creator to notify")
}

// A transition into review is handed out once, with what the notification
// shows; the transitions before it are not announced.
func TestTaskReviewTransitionIsClaimedOnce(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	require.NoError(t, store.StartPublication(ctx, op))
	require.NoError(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: publishedCommit, PullRequestNumber: 12, PullRequestURL: "https://github.com/fixture/repo/pull/12"}))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	var pending int
	require.NoError(t, db.Get(&pending, `SELECT count(*) FROM agent_task_transitions WHERE task_id=$1 AND notification_pending`, task.ID))
	require.Zero(t, pending, "intermediate states such as publishing do not notify")

	require.NoError(t, repo.Transition(ctx, *claimed, "awaiting_review", "Pull request opened; waiting for human review"))

	// Other tests share the database; look only at this task's notice.
	mine := func(notices []domain.AgentTaskNotice) []domain.AgentTaskNotice {
		var own []domain.AgentTaskNotice
		for _, notice := range notices {
			if notice.TaskID == task.ID {
				own = append(own, notice)
			}
		}
		return own
	}
	notices, err := repo.ClaimTaskNotices(ctx, 1000)
	require.NoError(t, err)
	require.Equal(t, []domain.AgentTaskNotice{{TaskID: task.ID, WorkspaceID: task.WorkspaceID, CreatorID: task.CreatorID, IssueID: task.IssueID,
		Kind: domain.AgentTaskKindImplement, State: "awaiting_review", IssueIdentifier: "REC-1", IssueTitle: "Recovery"}}, mine(notices))
	again, err := repo.ClaimTaskNotices(ctx, 1000)
	require.NoError(t, err)
	require.Empty(t, mine(again))
}

func TestNotificationPreferencesRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	user := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	repo, ctx := NewUserPreferencesRepository(db), context.Background()

	// A row written without the new columns reads the column defaults.
	_, err = db.Exec(`INSERT INTO user_preferences(user_id) VALUES($1)`, user)
	require.NoError(t, err)
	prefs, err := repo.Get(ctx, user)
	require.NoError(t, err)
	require.True(t, prefs.BrowserNotifications)
	require.True(t, prefs.WorkflowEditorPanelOpen)
	require.Equal(t, "chime", prefs.NotificationSound)
	require.Empty(t, prefs.NotificationEvents)

	prefs.BrowserNotifications, prefs.NotificationSound = false, "bell"
	prefs.WorkflowEditorPanelOpen = false
	prefs.NotificationEvents = domain.NotificationEvents{"comment": false, "assigned": true}
	require.NoError(t, repo.Upsert(ctx, prefs))
	stored, err := repo.Get(ctx, user)
	require.NoError(t, err)
	require.False(t, stored.BrowserNotifications)
	require.False(t, stored.WorkflowEditorPanelOpen)
	require.Equal(t, "bell", stored.NotificationSound)
	require.Equal(t, domain.NotificationEvents{"comment": false, "assigned": true}, stored.NotificationEvents)

	prefs.NotificationSound = "gong"
	require.Error(t, repo.Upsert(ctx, prefs), "the column refuses an unknown sound")
}
