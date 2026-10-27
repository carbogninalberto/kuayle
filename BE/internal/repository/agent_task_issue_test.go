package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestAgentTaskNativeStartMovesUnchangedIssueToConfiguredStartedStatus(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	started := uuid.New()
	_, err := db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Building','building','started',3)`, started, task.TeamID)
	require.NoError(t, err)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	var before uuid.UUID
	require.NoError(t, db.Get(&before, `SELECT status_id FROM issues WHERE id=$1`, task.IssueID))
	require.NotEqual(t, started, before, "delegation must not start the issue")
	changed, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	var current uuid.UUID
	require.NoError(t, db.Get(&current, `SELECT status_id FROM issues WHERE id=$1`, task.IssueID))
	require.Equal(t, started, current)
	require.NotNil(t, changed)
	require.Equal(t, domain.IssueStatus("building"), changed.Status)
	require.NotNil(t, changed.StartedAt)
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM issue_history WHERE issue_id=$1 AND user_id=$2 AND field='status' AND old_value='Todo' AND new_value='Building' AND agent=$3`, task.IssueID, task.CreatorID, task.Provider))
	require.Equal(t, 1, count)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Independent validation pending"))
	require.NoError(t, db.Get(&current, `SELECT status_id FROM issues WHERE id=$1`, task.IssueID))
	require.Equal(t, started, current, "native success is not issue completion")
}

func prepareTaskStart(t *testing.T, db *sqlx.DB, repo *AgentTaskRepository, ctx context.Context, task domain.AgentTask, fixture environmentRaceFixture) *domain.AgentTask {
	t.Helper()
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err := db.Exec(`UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, task.ID, machine)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at) VALUES($1,$2,$3,$4,$5,'codex','autonomous','Test','["codex"]',60,'running',now())`, task.RunID, task.WorkspaceID, machine, task.ID, task.CreatorID)
	require.NoError(t, err)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, task.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	return claimed
}

func TestAgentTaskNativeStartPreservesLaterIssueEdits(t *testing.T) {
	for _, scenario := range []string{"status", "title", "status_back", "completed_at_delegation", "missing_started_status"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, ctx, task, fixture := taskRecoveryFixture(t)
			started, done := uuid.New(), uuid.New()
			_, err := db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$3,'Building','building','started'),($2,$3,'Accepted','accepted','completed')`, started, done, task.TeamID)
			require.NoError(t, err)
			var original uuid.UUID
			require.NoError(t, db.Get(&original, `SELECT status_id FROM issues WHERE id=$1`, task.IssueID))
			expected := original
			switch scenario {
			case "status", "completed_at_delegation":
				_, err = db.Exec(`UPDATE issues SET status_id=$2,status='accepted',updated_at=now() WHERE id=$1`, task.IssueID, done)
				expected = done
			case "title":
				_, err = db.Exec(`UPDATE issues SET title='Human edited the request',updated_at=now() WHERE id=$1`, task.IssueID)
			case "status_back":
				_, err = db.Exec(`UPDATE issues SET status_id=$2,status='accepted',updated_at=now() WHERE id=$1`, task.IssueID, done)
				require.NoError(t, err)
				_, err = db.Exec(`UPDATE issues SET status_id=$2,status='todo',updated_at=now() WHERE id=$1`, task.IssueID, original)
			case "missing_started_status":
				_, err = db.Exec(`DELETE FROM team_statuses WHERE id=$1`, started)
			}
			require.NoError(t, err)
			if scenario == "completed_at_delegation" {
				_, err = db.Exec(`DELETE FROM agent_tasks WHERE id=$1`, task.ID)
				require.NoError(t, err)
				fresh, e := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Inspect accepted work", Provider: "codex"})
				require.NoError(t, e)
				task = *fresh
			}
			claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
			changed, err := repo.Start(ctx, *claimed)
			require.NoError(t, err)
			require.Nil(t, changed)
			var actual uuid.UUID
			require.NoError(t, db.Get(&actual, `SELECT status_id FROM issues WHERE id=$1`, task.IssueID))
			require.Equal(t, expected, actual)
			var state string
			require.NoError(t, db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, task.ID))
			require.Equal(t, "running", state)
		})
	}
}

func TestAgentTaskNativeStartRequiresCurrentAuthorityAndExecution(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	stranger := domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err := repo.Start(stranger, *claimed)
	require.ErrorIs(t, err, ErrConversationNotFound)
	stale := *claimed
	stale.Generation++
	_, err = repo.Start(ctx, stale)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET started_at=NULL,status='starting' WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	_, err = repo.Start(ctx, *claimed)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET started_at=now(),status='failed' WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err, "a short-lived failed process still really started")
	_, err = repo.Start(ctx, *claimed)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
}

func TestAgentTaskNativeStartRollsBackWhenHistoryCannotPersist(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	_, err := db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Building','building','started')`, task.TeamID)
	require.NoError(t, err)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err = db.Exec(`ALTER TABLE issue_history ADD CONSTRAINT simulated_history_failure CHECK(field<>'status')`)
	require.NoError(t, err)
	_, err = repo.Start(ctx, *claimed)
	require.Error(t, err)
	var state string
	require.NoError(t, db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, task.ID))
	require.Equal(t, "preparing", state)
	var unchanged bool
	require.NoError(t, db.Get(&unchanged, `SELECT i.status_id=t.issue_status_id AND i.updated_at=t.issue_updated_at FROM issues i JOIN agent_tasks t ON i.id=t.issue_id WHERE t.id=$1`, task.ID))
	require.True(t, unchanged)
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM agent_task_transitions WHERE task_id=$1 AND to_state='running'`, task.ID))
	require.Zero(t, count)
}

func TestConcurrentAgentTaskStartsChangeOneIssueOnce(t *testing.T) {
	db, repo, ctx, first, fixture := taskRecoveryFixture(t)
	_, err := db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Building','building','started')`, first.TeamID)
	require.NoError(t, err)
	second, err := repo.Create(ctx, first.WorkspaceID, *first.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Separate worktree", Provider: "codex"})
	require.NoError(t, err)
	a := prepareTaskStart(t, db, repo, ctx, first, fixture)
	b := prepareTaskStart(t, db, repo, ctx, *second, fixture)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, claimed := range []*domain.AgentTask{a, b} {
		wg.Go(func() { _, e := repo.Start(ctx, *claimed); results <- e })
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM issue_history WHERE issue_id=$1 AND field='status'`, first.IssueID))
	require.Equal(t, 1, count)
}

func TestAgentTaskRetryRefreshesIssueSnapshotOnlyForNewIntent(t *testing.T) {
	for _, laterEdit := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh_retry", true: "duplicate_after_edit"}[laterEdit], func(t *testing.T) {
			db, repo, ctx, task, fixture := taskRecoveryFixture(t)
			_, err := db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Building','building','started')`, task.TeamID)
			require.NoError(t, err)
			claimed, err := repo.Claim(ctx)
			require.NoError(t, err)
			require.NoError(t, repo.Transition(ctx, *claimed, "blocked", "Repository configuration missing"))
			_, err = db.Exec(`UPDATE issues SET title='Clarified before retry',updated_at=now() WHERE id=$1`, task.IssueID)
			require.NoError(t, err)
			req := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: task.Generation, Prompt: "Implement clarified request"}
			fresh, err := repo.Retry(ctx, task.WorkspaceID, task.ID, req)
			require.NoError(t, err)
			if laterEdit {
				_, err = db.Exec(`UPDATE issues SET title='Changed again after retry',updated_at=now() WHERE id=$1`, task.IssueID)
				require.NoError(t, err)
				duplicate, e := repo.Retry(ctx, task.WorkspaceID, task.ID, req)
				require.NoError(t, e)
				require.Equal(t, fresh.RunID, duplicate.RunID)
			}
			started := prepareTaskStart(t, db, repo, ctx, *fresh, fixture)
			changed, err := repo.Start(ctx, *started)
			require.NoError(t, err)
			if laterEdit {
				require.Nil(t, changed)
			} else {
				require.NotNil(t, changed)
				require.Equal(t, "Clarified before retry", changed.Title)
			}
		})
	}
}
