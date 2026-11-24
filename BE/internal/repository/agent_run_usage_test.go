package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func reportedUsage(input, output, read, write int64, cost float64) domain.AgentRunUsage {
	return domain.AgentRunUsage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &read, CacheWriteTokens: &write, CostUSD: &cost}
}

// A run stores what its harness reported, its own share when the harness
// reports its session's running total (Codex), and a task's usage is the
// sum over its own runs, whatever page of them is read, for whoever may read
// the task.
func TestTaskUsageSumsWhatItsRunsReported(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	prepareTaskStart(t, db, repo, ctx, task, fixture)
	store := NewDevMachineRepository(db)
	later := func(owner uuid.UUID, minutes int) uuid.UUID {
		id := uuid.New()
		_, err := db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,created_at)
 SELECT $2,workspace_id,machine_id,$4,provider_id,mode,prompt,command_argv,max_runtime_seconds,'running',created_at+make_interval(mins=>$3) FROM dev_machine_agent_runs WHERE id=$1`, task.RunID, id, minutes, owner)
		require.NoError(t, err)
		return id
	}
	complete := func(id uuid.UUID, session string, usage domain.AgentRunUsage, sessionTotal bool) {
		t.Helper()
		if session != "" {
			require.NoError(t, store.RecordNativeSession(ctx, id, session))
		}
		run, err := store.GetAgentRunInternal(ctx, id)
		require.NoError(t, err)
		run.Status = domain.DevMachineAgentRunStatusSucceeded
		run.AgentRunUsage, run.UsageSessionTotal = usage, sessionTotal
		require.NoError(t, store.CompleteAgentRun(ctx, run))
	}
	thread := "019a8f2e-1c4b-7d10-9e55-3f2a1b0c4d5e"
	threadTotal := func(input, output, read int64) domain.AgentRunUsage {
		return domain.AgentRunUsage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &read}
	}
	// The first Codex attempt opened the thread; the next resumed it, and
	// Codex reports the thread's total, the first attempt's tokens included.
	complete(task.RunID, thread, threadTotal(300, 100, 5000), true)
	resumed := later(task.ID, 10)
	complete(resumed, thread, threadTotal(900, 250, 12000), true)
	silent := later(task.ID, 20)
	complete(silent, "", domain.AgentRunUsage{}, false)
	priced := later(task.ID, 30)
	complete(priced, "", reportedUsage(40, 10, 0, 7, 0.25), false)
	// Recording a run's end again (a manager replaying it) keeps its share.
	complete(resumed, "", threadTotal(900, 250, 12000), true)
	other, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Independent request", Provider: "codex"})
	require.NoError(t, err)
	complete(later(other.ID, 40), "", reportedUsage(1_000_000, 1_000_000, 0, 0, 99), false)

	runs, err := repo.Runs(ctx, task.WorkspaceID, task.ID, nil)
	require.NoError(t, err)
	byID := map[uuid.UUID]domain.DevMachineAgentRun{}
	for _, run := range runs.Runs {
		byID[run.ID] = run
	}
	require.Len(t, byID, 4, "the other task's run is not this task's")
	require.Equal(t, threadTotal(300, 100, 5000), byID[task.RunID].AgentRunUsage)
	require.Equal(t, threadTotal(600, 150, 7000), byID[resumed].AgentRunUsage, "the resumed attempt's own share")
	require.Equal(t, domain.AgentRunUsage{}, byID[silent].AgentRunUsage, "a harness that reported nothing")
	require.Equal(t, reportedUsage(40, 10, 0, 7, 0.25), byID[priced].AgentRunUsage)
	encoded, err := json.Marshal(byID[resumed])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"input_tokens":600`)
	require.NotContains(t, string(encoded), "cost_usd", "an unreported cost is absent, not zero")

	total := reportedUsage(940, 260, 12000, 7, 0.25)
	require.Equal(t, total, runs.Usage, "the sum of what was reported; 900 input tokens of the thread, not 1,200")
	older, err := repo.Runs(ctx, task.WorkspaceID, task.ID, &priced)
	require.NoError(t, err)
	require.Len(t, older.Runs, 3)
	require.Equal(t, total, older.Usage, "every page carries the task's total")
	usage, err := repo.Usage(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, total, usage)

	otherUsage, err := repo.Usage(ctx, task.WorkspaceID, other.ID)
	require.NoError(t, err)
	require.Equal(t, reportedUsage(1_000_000, 1_000_000, 0, 0, 99), otherUsage)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Usage(stranger, task.WorkspaceID, task.ID)
	require.Error(t, err, "only those who may read the task read its usage")
	_, err = repo.Usage(ctx, uuid.New(), task.ID)
	require.Error(t, err, "another workspace's task")
}
