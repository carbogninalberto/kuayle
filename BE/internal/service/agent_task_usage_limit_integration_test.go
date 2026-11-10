package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The task worker end to end on a real database (KUA-113): a run refused at
// the provider's usage limit pauses its task; cleanup ends in blocked, not
// failed; once the reset has come the worker's sweep queues the next attempt,
// which continues the same native session.
func TestWorkerPausesAndResumesATaskAtTheUsageLimit(t *testing.T) {
	f := newToolFixture(t)
	issue := uuid.New()
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
 SELECT $1,t.workspace_id,t.id,1,'ENG-1','Usage limit',$3,(SELECT id FROM team_statuses WHERE team_id=t.id LIMIT 1) FROM teams t WHERE t.workspace_id=$2`, issue, f.ws, f.user)
	require.NoError(t, err)
	chat, err := f.repo.OpenIssue(f.ctx, f.ws, issue)
	require.NoError(t, err)
	repo := repository.NewAgentTaskRepository(f.db)
	created, err := repo.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "claude-code"})
	require.NoError(t, err)
	// The worker holds the task's lease while its agent runs.
	lease := func() domain.AgentTask {
		t.Helper()
		_, err := f.db.Exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds' WHERE id=$1`, created.ID)
		require.NoError(t, err)
		task, err := repo.Get(f.ctx, f.ws, created.ID)
		require.NoError(t, err)
		return *task
	}
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='running' WHERE id=$1`, created.ID)
	require.NoError(t, err)

	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	runError := domain.UsageLimit{Message: "You've hit your 5-hour limit · resets 4:10am (UTC)", ResetsAt: &reset}.ErrorMessage()
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &runError}}
	svc := &AgentTaskService{Repo: repo, store: repo, runtime: runtime}

	// The agent's run comes back refused at the limit.
	require.NoError(t, svc.step(f.ctx, lease()))
	paused, err := repo.Get(f.ctx, f.ws, created.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", paused.State)
	require.Equal(t, "You've hit your 5-hour limit · resets 4:10am (UTC)", *paused.UsageLimitMessage)
	require.Contains(t, paused.Reason, "Claude Code usage limit reached")

	// Runtime cleanup confirmed: paused, never failed.
	require.NoError(t, svc.step(f.ctx, lease()))
	require.Equal(t, 1, runtime.cancels)
	blocked, err := repo.Get(f.ctx, f.ws, created.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", blocked.State)
	require.WithinDuration(t, reset.Add(domain.UsageLimitResumeMargin), *blocked.UsageLimitResumeAt, time.Second)
	var transitions []string
	require.NoError(t, f.db.Select(&transitions, `SELECT to_state FROM agent_task_transitions WHERE task_id=$1 ORDER BY sequence`, created.ID))
	require.NotContains(t, transitions, "failed")

	// Before the reset nothing resumes; after it the sweep queues the attempt.
	svc.resumeUsageLimited(f.ctx)
	still, err := repo.Get(f.ctx, f.ws, created.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", still.State)
	_, err = f.db.Exec(`UPDATE agent_tasks SET usage_limit_resume_at=now()-interval '1 second' WHERE id=$1`, created.ID)
	require.NoError(t, err)
	svc.resumeUsageLimited(f.ctx)
	resumed, err := repo.Get(f.ctx, f.ws, created.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", resumed.State)
	require.Equal(t, usageLimitResumedReason, resumed.Reason)
	require.Equal(t, blocked.Generation+1, resumed.Generation)
	require.True(t, resumed.ResumeNativeSession)
	require.Nil(t, resumed.UsageLimitMessage)

	// The resumed attempt continues the paused attempt's session.
	machine := uuid.New()
	resumed.MachineID = &machine
	previous := &domain.AgentHandoff{AttemptID: blocked.RunID, Status: "failed", MachineID: machine, NativeSessionID: "ae361499-0008-48b7-b8a7-d9df1193e4d3"}
	require.Equal(t, previous.NativeSessionID, taskSession(*resumed, previous).ResumeID)
}
