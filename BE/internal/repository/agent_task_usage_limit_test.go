package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A task whose agent hit the provider's usage limit pauses instead of failing:
// cleanup ends in blocked with the reset time, the creator's other queued
// tasks for the agent wait, and the task resumes on its own once the reset
// has come, continuing its native session.
func TestUsageLimitPausesHoldsAndResumesTheTask(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	other := uuid.New()
	_, err := db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 SELECT $1,workspace_id,team_id,2,'REC-2','Second',creator_id,status_id FROM issues WHERE id=$2`, other, task.IssueID)
	require.NoError(t, err)
	chat, err := NewConversationRepository(db).OpenIssue(ctx, fixture.workspaceID, other)
	require.NoError(t, err)
	second, err := repo.Create(ctx, fixture.workspaceID, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Second", Provider: "codex"})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now()+interval '1 hour' WHERE id=$1`, second.ID)
	require.NoError(t, err)

	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, task.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Transition(ctx, *claimed, "running", "Running"))
	claimed, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	claimed, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)

	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	limit := domain.UsageLimit{Message: "You've hit your usage limit. Try again in 1 hour.", ResetsAt: &reset}
	require.NoError(t, repo.PauseForUsageLimit(ctx, *claimed, limit, "Paused: Codex usage limit reached.", "Waiting for the Codex usage limit to reset"))
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", current.State, "the runtime is cleaned up first, as for a failure")
	require.Equal(t, limit.Message, *current.UsageLimitMessage)
	require.WithinDuration(t, reset.Add(domain.UsageLimitResumeMargin), *current.UsageLimitResumeAt, time.Second)
	waiting, err := repo.Get(ctx, task.WorkspaceID, second.ID)
	require.NoError(t, err)
	require.Equal(t, "Waiting for the Codex usage limit to reset", waiting.Reason)

	require.NoError(t, repo.FinishCancellation(ctx, *current, "Runtime cancellation confirmed"))
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", current.State, "a usage limit pauses, never fails")
	require.Equal(t, "Paused: Codex usage limit reached.", current.Reason)
	require.Nil(t, current.FailureClass, "a pause is not a failure and has no class")

	// The other queued task waits for the creator's hold, then runs.
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, second.ID)
	require.NoError(t, err)
	held, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, held, "a queued task for the limited agent must not start")
	_, err = db.Exec(`UPDATE agent_usage_limit_holds SET resets_at=now()-interval '1 second'`)
	require.NoError(t, err)
	released, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, released)
	require.Equal(t, second.ID, released.ID)
	require.NoError(t, repo.Release(ctx, *released))
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now()+interval '1 hour' WHERE id=$1`, second.ID)
	require.NoError(t, err)

	// Not yet due: nothing resumes.
	resumed, err := repo.ResumeDueUsageLimits(ctx, "Resumed after the usage limit reset")
	require.NoError(t, err)
	require.Zero(t, resumed)
	_, err = db.Exec(`UPDATE agent_tasks SET usage_limit_resume_at=now()-interval '1 second' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	resumed, err = repo.ResumeDueUsageLimits(ctx, "Resumed after the usage limit reset")
	require.NoError(t, err)
	require.Equal(t, 1, resumed)
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Equal(t, claimed.Generation+1, current.Generation)
	require.NotEqual(t, claimed.RunID, current.RunID)
	require.True(t, current.ResumeNativeSession)
	require.Equal(t, 1, current.UsageLimitResumes)
	require.Nil(t, current.UsageLimitMessage)
	require.False(t, current.FailurePending)
	var holds int
	require.NoError(t, db.Get(&holds, `SELECT count(*) FROM agent_usage_limit_holds`))
	require.Zero(t, holds, "expired holds are removed")
}

// "Don't resume" keeps the task paused; "Resume now" starts it at once and
// lifts the creator's hold; a manual retry forgets the pause.
func TestUsageLimitUserControls(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	pause := func() {
		t.Helper()
		_, err := db.Exec(`UPDATE agent_tasks SET state='blocked',usage_limit_message='You''ve hit your usage limit',usage_limit_resume_at=now()-interval '1 second',usage_limit_resumes=2 WHERE id=$1`, task.ID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO agent_usage_limit_holds(user_id,provider,resets_at,message) VALUES($1,'codex',now()+interval '1 hour','limit') ON CONFLICT DO NOTHING`, fixture.userID)
		require.NoError(t, err)
	}
	pause()
	paused, err := repo.CancelUsageLimitResume(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Nil(t, paused.UsageLimitResumeAt)
	resumed, err := repo.ResumeDueUsageLimits(ctx, "Resumed")
	require.NoError(t, err)
	require.Zero(t, resumed, "a resume the user turned off never happens")

	now, err := repo.ResumeUsageLimitNow(ctx, task.WorkspaceID, task.ID, "Resumed by request")
	require.NoError(t, err)
	require.Equal(t, "queued", now.State)
	require.Zero(t, now.UsageLimitResumes, "a manual resume restarts the automatic count")
	require.True(t, now.ResumeNativeSession)
	var holds int
	require.NoError(t, db.Get(&holds, `SELECT count(*) FROM agent_usage_limit_holds WHERE user_id=$1`, fixture.userID))
	require.Zero(t, holds)
	_, err = repo.ResumeUsageLimitNow(ctx, task.WorkspaceID, task.ID, "Again")
	require.ErrorIs(t, err, ErrAgentTaskConflict, "only a paused task resumes")

	// Another member cannot resume someone else's task.
	pause()
	outsider := domain.WithAccess(ctx, domain.Access{WorkspaceID: fixture.workspaceID, UserID: uuid.New()})
	_, err = repo.ResumeUsageLimitNow(outsider, task.WorkspaceID, task.ID, "Resumed by request")
	require.Error(t, err)
	_, err = repo.CancelUsageLimitResume(outsider, task.WorkspaceID, task.ID)
	require.Error(t, err)

	// A manual retry starts a fresh attempt and forgets the pause.
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: task.Generation + 1, Prompt: "Retry"})
	require.NoError(t, err)
	require.Nil(t, retried.UsageLimitMessage)
	require.False(t, retried.ResumeNativeSession)
	require.Zero(t, retried.UsageLimitResumes)
}
