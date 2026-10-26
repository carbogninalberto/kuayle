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

func taskRecoveryFixture(t *testing.T) (*sqlx.DB, *AgentTaskRepository, context.Context, domain.AgentTask, environmentRaceFixture) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	team, issue := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Task','REC')`, team, fixture.workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'REC-1','Recovery',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, fixture.workspaceID, team, fixture.userID)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: fixture.workspaceID, UserID: fixture.userID})
	chat, err := NewConversationRepository(db).OpenIssue(ctx, fixture.workspaceID, issue)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	task, err := repo.Create(ctx, fixture.workspaceID, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement and test", Provider: "codex"})
	require.NoError(t, err)
	return db, repo, ctx, *task, fixture
}

func TestAgentTaskWorkerFailuresAreBoundedBeforeTerminalState(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	for range 4 {
		require.NoError(t, repo.RecordStepResult(ctx, *claimed, true))
	}
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "preparing", current.State)
	require.NoError(t, repo.RecordStepResult(ctx, *claimed, false))
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Zero(t, current.FailureCount)
	for range 5 {
		require.NoError(t, repo.RecordStepResult(ctx, *claimed, true))
	}
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", current.State)
	require.True(t, current.FailurePending)
	require.NoError(t, repo.Release(ctx, *claimed))
	var delayed bool
	require.NoError(t, db.Get(&delayed, `SELECT next_poll_at>now()+interval '20 seconds' FROM agent_tasks WHERE id=$1`, task.ID))
	require.True(t, delayed)
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	cleanup, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	require.ErrorIs(t, repo.RecordStepResult(ctx, *claimed, true), ErrAgentTaskConflict)
	require.NoError(t, repo.FinishCancellation(ctx, *cleanup, "Cleanup confirmed"))
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State)
}

func TestAgentTaskCancellationSettlesOnlyItsAllocatedMachine(t *testing.T) {
	for _, owned := range []bool{true, false} {
		t.Run(map[bool]string{true: "allocated", false: "reused"}[owned], func(t *testing.T) {
			db, repo, ctx, task, fixture := taskRecoveryFixture(t)
			machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
			_, err := db.Exec(`UPDATE agent_tasks SET machine_id=$2,reserved_machine_id=CASE WHEN $3 THEN $2 ELSE reserved_machine_id END WHERE id=$1`, task.ID, machine, owned)
			require.NoError(t, err)
			require.NoError(t, repo.RequestCancel(ctx, task.WorkspaceID, task.ID))
			cleanup, err := repo.Claim(ctx)
			require.NoError(t, err)
			require.NotNil(t, cleanup)
			err = repo.FinishCancellation(ctx, *cleanup, "Cleanup confirmed")
			var count int
			require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_operations WHERE machine_id=$1 AND action='pause'`, machine))
			if !owned {
				require.NoError(t, err)
				require.Zero(t, count)
				return
			}
			require.ErrorIs(t, err, ErrAgentTaskConflict)
			require.Equal(t, 1, count)
			_, err = db.Exec(`UPDATE dev_machine_operations SET status='failed',attempts=5 WHERE machine_id=$1 AND action='pause'`, machine)
			require.NoError(t, err)
			require.ErrorIs(t, repo.FinishCancellation(ctx, *cleanup, "Not stopped"), ErrAgentTaskCleanupFailed)
			require.NoError(t, repo.RequestCancel(ctx, task.WorkspaceID, task.ID))
			var reset bool
			require.NoError(t, db.Get(&reset, `SELECT status='pending' AND attempts=0 FROM dev_machine_operations WHERE machine_id=$1 AND action='pause'`, machine))
			require.True(t, reset)
			require.ErrorIs(t, repo.FinishCancellation(ctx, *cleanup, "Still pending"), ErrAgentTaskConflict)
			_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE machine_id=$1 AND action='pause'`, machine)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE dev_machines SET status='paused' WHERE id=$1`, machine)
			require.NoError(t, err)
			require.NoError(t, repo.FinishCancellation(ctx, *cleanup, "Paused with worktree preserved"))
			var desired string
			require.NoError(t, db.Get(&desired, `SELECT desired_status FROM dev_machines WHERE id=$1`, machine))
			require.Equal(t, "paused", desired)
		})
	}
}

func TestAgentTaskRetryIsExplicitFencedAndIdempotent(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "blocked", "Repository setup is missing"))
	req := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: task.Generation, Prompt: "Retry after configuring the repository"}
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, req)
	require.NoError(t, err)
	require.Equal(t, "queued", retried.State)
	require.Equal(t, task.Generation+1, retried.Generation)
	require.NotEqual(t, task.RunID, retried.RunID)
	require.Equal(t, task.CheckoutID, retried.CheckoutID)
	require.Equal(t, req.Prompt, retried.Prompt)
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "preparing", "Stale previous attempt"), ErrAgentTaskConflict)
	repeated, err := repo.Retry(ctx, task.WorkspaceID, task.ID, req)
	require.NoError(t, err)
	require.Equal(t, retried.RunID, repeated.RunID)
	changed := req
	changed.Prompt = "Different work"
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, changed)
	require.ErrorIs(t, err, ErrConversationConflict)
	other := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Retry(other, task.WorkspaceID, task.ID, req)
	require.ErrorIs(t, err, ErrConversationNotFound)
	req.RequestID = uuid.New()
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, req)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM agent_task_retries WHERE task_id=$1`, task.ID))
	require.Equal(t, 1, count)
}

func TestAgentTaskRequestReplayKeepsOriginalAttemptAfterLaterRetries(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	original := domain.AgentTaskRequest{RequestID: task.RequestID, Prompt: task.Prompt, Provider: task.Provider}
	_, err := db.Exec(`UPDATE agent_tasks SET state='blocked' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	requestA := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: 1, Prompt: "First retry"}
	first, err := repo.Retry(ctx, task.WorkspaceID, task.ID, requestA)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET state='blocked' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	second, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: 2, Prompt: "Second retry"})
	require.NoError(t, err)
	require.Equal(t, int64(3), second.Generation)
	replayed, err := repo.Retry(ctx, task.WorkspaceID, task.ID, requestA)
	require.NoError(t, err)
	require.Equal(t, first.RunID, replayed.RunID)
	require.Equal(t, first.Generation, replayed.Generation)
	require.Equal(t, first.Prompt, replayed.Prompt)
	created, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, original)
	require.NoError(t, err)
	require.Equal(t, task.RunID, created.RunID)
	require.Equal(t, int64(1), created.Generation)
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, second.RunID, current.RunID)
	// Missing audit data must fail closed, never substitute a later attempt or
	// authorize another execution under a previously accepted request UUID.
	_, err = db.Exec(`DELETE FROM conversation_events WHERE conversation_id=$1 AND payload->>'id'=$2 AND payload->>'generation'='2'`, task.ConversationID, task.ID.String())
	require.NoError(t, err)
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, requestA)
	require.ErrorIs(t, err, ErrConversationConflict)
}
