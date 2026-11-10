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

// A cancelled task leaves its machine running, whether Kuayle created it for
// the task or the task reused it, so the next task can reuse it.
func TestAgentTaskCancellationKeepsTheMachineRunning(t *testing.T) {
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
			require.NoError(t, repo.FinishCancellation(ctx, *cleanup, "Cleanup confirmed"))
			var operations int
			require.NoError(t, db.Get(&operations, `SELECT count(*) FROM dev_machine_operations WHERE machine_id=$1 AND action IN ('pause','stop','teardown')`, machine))
			require.Zero(t, operations)
			var status, desired string
			require.NoError(t, db.QueryRow(`SELECT status,desired_status FROM dev_machines WHERE id=$1`, machine).Scan(&status, &desired))
			require.Equal(t, "running", status)
			require.Equal(t, "running", desired)
			current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
			require.NoError(t, err)
			require.Equal(t, "cancelled", current.State)
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

func TestAgentTaskRetrySwitchesModelAndReplaysOnlyTheSameModel(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	var server uuid.UUID
	require.NoError(t, db.Get(&server, `INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,encrypted_api_key,models,default_model,thinking) VALUES($1,'Other','https://models.example.test','x','["model-b"]','model-b','low') RETURNING id`, task.WorkspaceID))
	_, err := db.Exec(`UPDATE agent_tasks SET state='blocked',model='model-a',thinking='high',model_server_id=NULL WHERE id=$1`, task.ID)
	require.NoError(t, err)

	switched := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: task.Generation, Prompt: "Use another model", Model: "model-b", Thinking: "low", ModelServerID: &server}
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, switched)
	require.NoError(t, err)
	require.Equal(t, "model-b", retried.Model)
	require.Equal(t, "low", retried.Thinking)
	require.Equal(t, &server, retried.ModelServerID)
	var model, thinking string
	var stored *uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT model,thinking,model_server_id FROM agent_tasks WHERE id=$1`, task.ID).Scan(&model, &thinking, &stored))
	require.Equal(t, "model-b", model)
	require.Equal(t, "low", thinking)
	require.Equal(t, &server, stored)

	// Replaying the same receipt returns the same attempt; a changed model is a
	// different intent and conflicts, as a changed prompt does.
	repeated, err := repo.Retry(ctx, task.WorkspaceID, task.ID, switched)
	require.NoError(t, err)
	require.Equal(t, retried.RunID, repeated.RunID)
	changed := switched
	changed.Model, changed.Thinking, changed.ModelServerID = "model-c", "", nil
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, changed)
	require.ErrorIs(t, err, ErrConversationConflict)
	kept := switched
	kept.Model, kept.Thinking, kept.ModelServerID = "", "", nil
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, kept)
	require.ErrorIs(t, err, ErrConversationConflict)
	otherServer := switched
	otherServer.RequestedModelServerID = &server
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, otherServer)
	require.ErrorIs(t, err, ErrConversationConflict, "the server the caller picked is part of the intent")

	// Without a model the next attempt keeps the task's model, thinking and server,
	// and replaying it with a model conflicts too.
	_, err = db.Exec(`UPDATE agent_tasks SET state='blocked' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	plain := domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: retried.Generation, Prompt: "Keep the model"}
	next, err := repo.Retry(ctx, task.WorkspaceID, task.ID, plain)
	require.NoError(t, err)
	require.Equal(t, "model-b", next.Model)
	require.Equal(t, "low", next.Thinking)
	require.Equal(t, &server, next.ModelServerID)
	plainWithModel := plain
	plainWithModel.Model = "model-b"
	_, err = repo.Retry(ctx, task.WorkspaceID, task.ID, plainWithModel)
	require.ErrorIs(t, err, ErrConversationConflict)
	again, err := repo.Retry(ctx, task.WorkspaceID, task.ID, plain)
	require.NoError(t, err)
	require.Equal(t, next.RunID, again.RunID)
}

// A workflow that stops a task says why (a step's visit limit, the budget, an
// outcome nothing follows). That stays the failed task's reason instead of
// the generic text; other failures keep the generic text (KUA-120).
func TestWorkflowFailureKeepsItsReason(t *testing.T) {
	for name, tc := range map[string]struct{ reason, want string }{
		"workflow limit": {
			"Workflow · Automated review reached its limit of 3 visits after pull request published",
			"Workflow · Automated review reached its limit of 3 visits after pull request published",
		},
		"native failure": {"Native coding iteration failed; stopping execution before retry", "Execution failed; runtime cleanup confirmed"},
	} {
		t.Run(name, func(t *testing.T) {
			_, repo, ctx, task, _ := taskRecoveryFixture(t)
			claimed, err := repo.Claim(ctx)
			require.NoError(t, err)
			require.NotNil(t, claimed)
			require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
			require.NoError(t, repo.Fail(ctx, *claimed, tc.reason))
			require.NoError(t, repo.FinishCancellation(ctx, *claimed, "Runtime cancellation confirmed"))
			failed, err := repo.Get(ctx, task.WorkspaceID, task.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", failed.State)
			require.Equal(t, tc.want, failed.Reason)
		})
	}
}
