package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

const creditReason = "Paused: OpenCode's provider account has no credit left (APIError: Insufficient balance). Top up the provider account, then resume the task; the work done so far is kept."

// Production (KUA-211): each "Insufficient balance" took the workflow's
// agent_failed self-loop and used a visit. A credit pause (KUA-218) keeps the
// task on its step: cleanup ends in blocked, nothing resumes on its own, the
// creator hears about it through the task's own notices, and resuming after
// the top-up continues the same step, visit and iteration, with the paused
// time left out of the time budget.
func TestCreditPauseKeepsTheWorkflowStepAndResumeContinuesIt(t *testing.T) {
	f := newPauseFixture(t)
	f.exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,(random()*1e12)::bigint+1,'x','x',true,'{12345}')`, f.ws)
	f.exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,12345)`, f.ws, f.task.CreatorID)
	before := f.current()
	require.NotNil(t, before.WorkflowNode)
	f.exec(`UPDATE dev_machine_agent_runs SET status='failed',failure_class='provider',failure_rule='provider_credit' WHERE id=$1`, before.RunID)
	var stepsBefore int
	require.NoError(t, f.db.Get(&stepsBefore, `SELECT count(*) FROM agent_task_workflow_steps WHERE task_id=$1`, f.task.ID))

	claimed := f.claim()
	require.NotNil(t, claimed)
	require.NoError(t, f.repo.PauseForProviderCredit(f.ctx, *claimed, "APIError: Insufficient balance.", creditReason))
	pausing := f.current()
	require.Equal(t, "cancelling", pausing.State, "the runtime is cleaned up first, as for a usage limit")
	require.True(t, pausing.ProviderCreditExhausted)
	require.Nil(t, pausing.FailureClass, "a pause is not a failure")
	require.NotNil(t, pausing.WorkflowHeldSince, "the time budget stops while the task waits for a person")

	f.cleanup()
	paused := f.current()
	require.Equal(t, "blocked", paused.State)
	require.Equal(t, creditReason, paused.Reason)
	require.Equal(t, "APIError: Insufficient balance.", *paused.UsageLimitMessage)
	require.Nil(t, paused.UsageLimitResetsAt)
	require.Nil(t, paused.UsageLimitResumeAt, "nothing resumes on its own")
	require.Nil(t, paused.FailureClass)
	require.Equal(t, before.WorkflowSequence, paused.WorkflowSequence)
	require.JSONEq(t, string(before.WorkflowVisits), string(paused.WorkflowVisits), "no visit is used")
	require.Equal(t, before.WorkflowIterations, paused.WorkflowIterations, "no iteration is used")
	var steps int
	require.NoError(t, f.db.Get(&steps, `SELECT count(*) FROM agent_task_workflow_steps WHERE task_id=$1`, f.task.ID))
	require.Equal(t, stepsBefore, steps, "no edge is taken: the workflow enters no step")
	require.Nil(t, f.step(paused.WorkflowSequence).FinishedAt, "the step stays open")
	var holds int
	require.NoError(t, f.db.Get(&holds, `SELECT count(*) FROM agent_usage_limit_holds WHERE user_id=$1`, f.task.CreatorID))
	require.Zero(t, holds, "an account without credit holds none of the creator's other tasks")
	var transitions []string
	require.NoError(t, f.db.Select(&transitions, `SELECT to_state FROM agent_task_transitions WHERE task_id=$1 ORDER BY sequence`, f.task.ID))
	require.NotContains(t, transitions, "failed")
	// The creator is told, as for a usage limit: the blocked transition's
	// notice carries the reason, never the provider's billing link.
	var texts []string
	require.NoError(t, f.db.Select(&texts, `SELECT text FROM telegram_deliveries WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.task.CreatorID))
	require.Len(t, texts, 1)
	require.Contains(t, texts[0], "coding task blocked.")
	require.Contains(t, texts[0], "Top up the provider account, then resume the task")
	require.NotContains(t, texts[0], "http")

	// The usage-limit sweep never resumes it.
	resumedBySweep, err := f.repo.ResumeDueUsageLimits(f.ctx, "Resumed after the usage limit reset")
	require.NoError(t, err)
	require.Zero(t, resumedBySweep)
	require.Equal(t, "blocked", f.current().State)

	// The account was topped up an hour later; its creator resumes the task.
	f.exec(`UPDATE agent_tasks SET workflow_held_since=now()-interval '1 hour' WHERE id=$1`, f.task.ID)
	held := f.current()
	start := held.WorkflowStartedAt
	if held.WorkflowBudgetFrom != nil {
		start = held.WorkflowBudgetFrom
	}
	require.NotNil(t, start)
	resumed, err := f.repo.ResumePaused(f.ctx, f.ws, f.task.ID, "Resumed by request after the usage limit")
	require.NoError(t, err)
	require.Equal(t, "queued", resumed.State)
	require.Equal(t, domain.ProviderCreditResumedReason, resumed.Reason)
	require.False(t, resumed.ProviderCreditExhausted)
	require.Nil(t, resumed.UsageLimitMessage)
	require.True(t, resumed.ResumeNativeSession, "the attempt continues the paused session")
	require.Equal(t, paused.Generation+1, resumed.Generation)
	require.NotEqual(t, paused.RunID, resumed.RunID)
	require.Equal(t, before.WorkflowSequence, resumed.WorkflowSequence, "the same step")
	require.Equal(t, *before.WorkflowNode, *resumed.WorkflowNode)
	require.JSONEq(t, string(before.WorkflowVisits), string(resumed.WorkflowVisits), "the same visit")
	require.Equal(t, before.WorkflowIterations, resumed.WorkflowIterations, "the same iteration")
	require.NoError(t, f.db.Get(&steps, `SELECT count(*) FROM agent_task_workflow_steps WHERE task_id=$1`, f.task.ID))
	require.Equal(t, stepsBefore, steps)
	step := f.step(resumed.WorkflowSequence)
	require.EqualValues(t, resumed.Generation, *step.Generation, "the open step follows the new attempt")
	require.Nil(t, step.FinishedAt)
	require.GreaterOrEqual(t, step.PausedSeconds, 3590, "the paused hour is not the step's working time")
	require.Nil(t, resumed.WorkflowHeldSince)
	require.NotNil(t, resumed.WorkflowBudgetFrom)
	require.WithinDuration(t, start.Add(time.Hour), *resumed.WorkflowBudgetFrom, time.Minute, "the paused hour is left out of the time budget")

	handoff, err := f.repo.PreviousAttempt(f.ctx, *resumed)
	require.NoError(t, err)
	require.NotNil(t, handoff)
	require.Equal(t, paused.RunID, handoff.AttemptID)
	require.Equal(t, domain.FailureRuleProviderCredit, handoff.FailureRule, "the next attempt is told why it was paused")
	require.NotNil(t, f.claim(), "the worker starts the step again")
}

// Outside a workflow the task pauses and resumes the same way, and a retry
// instead of a resume forgets the pause.
func TestCreditPauseOfATaskWithoutAWorkflow(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	running := func() domain.AgentTask {
		t.Helper()
		_, err := db.Exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds' WHERE id=$1`, task.ID)
		require.NoError(t, err)
		current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
		require.NoError(t, err)
		return *current
	}
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, task.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Transition(ctx, *claimed, "running", "Running"))

	pause := func() domain.AgentTask {
		t.Helper()
		require.NoError(t, repo.PauseForProviderCredit(ctx, running(), "Credit balance is too low", creditReason))
		current := running()
		require.Equal(t, "cancelling", current.State)
		require.Nil(t, current.WorkflowHeldSince, "no workflow, no clock")
		require.NoError(t, repo.FinishCancellation(ctx, current, "Runtime cancellation confirmed"))
		paused, err := repo.Get(ctx, task.WorkspaceID, task.ID)
		require.NoError(t, err)
		require.Equal(t, "blocked", paused.State)
		require.True(t, paused.ProviderCreditExhausted)
		require.Nil(t, paused.UsageLimitResumeAt)
		return *paused
	}
	paused := pause()
	resumed, err := repo.ResumeUsageLimitNow(ctx, task.WorkspaceID, task.ID, "Resumed by request after the usage limit")
	require.NoError(t, err)
	require.Equal(t, "queued", resumed.State)
	require.Equal(t, domain.ProviderCreditResumedReason, resumed.Reason)
	require.Equal(t, paused.Generation+1, resumed.Generation)
	require.False(t, resumed.ProviderCreditExhausted)
	require.True(t, resumed.ResumeNativeSession)

	// Paused again; this time the creator retries it with new instructions.
	claimed, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, task.ID, claimed.ID)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Transition(ctx, *claimed, "running", "Running"))
	paused = pause()
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: paused.Generation, Prompt: "Continue after the top-up"})
	require.NoError(t, err)
	require.Equal(t, "queued", retried.State)
	require.False(t, retried.ProviderCreditExhausted, "a retry forgets the pause")
	require.Nil(t, retried.UsageLimitMessage)
}
