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
	thread := "01a20dfc-3afe-72f0-808d-40d2daae0827"
	threadTotal := func(input, output, read int64) domain.AgentRunUsage {
		var write int64
		return domain.AgentRunUsage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &read, CacheWriteTokens: &write}
	}
	// Codex 0.171.0 resuming one thread, as captured (agent/testdata/usage,
	// one request per run): each run's turn.completed is the thread's total
	// so far, 400, 800 and 1200 input tokens once cached input is taken off.
	complete(task.RunID, thread, threadTotal(400, 42, 800), true)
	resumed := later(task.ID, 10)
	complete(resumed, thread, threadTotal(800, 84, 1600), true)
	third := later(task.ID, 15)
	complete(third, thread, threadTotal(1200, 126, 2400), true)
	silent := later(task.ID, 20)
	complete(silent, "", domain.AgentRunUsage{}, false)
	priced := later(task.ID, 30)
	complete(priced, "", reportedUsage(40, 10, 0, 7, 0.25), false)
	// Recording a run's end again (a manager replaying it) keeps its share.
	complete(resumed, "", threadTotal(800, 84, 1600), true)
	other, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Independent request", Provider: "codex"})
	require.NoError(t, err)
	complete(later(other.ID, 40), "", reportedUsage(1_000_000, 1_000_000, 0, 0, 99), false)

	runs, err := repo.Runs(ctx, task.WorkspaceID, task.ID, nil)
	require.NoError(t, err)
	byID := map[uuid.UUID]domain.DevMachineAgentRun{}
	for _, run := range runs.Runs {
		byID[run.ID] = run
	}
	require.Len(t, byID, 5, "the other task's run is not this task's")
	for _, run := range []uuid.UUID{task.RunID, resumed, third} {
		require.Equal(t, threadTotal(400, 42, 800), byID[run].AgentRunUsage, "each run's own share of the thread")
	}
	require.Equal(t, domain.AgentRunUsage{}, byID[silent].AgentRunUsage, "a harness that reported nothing")
	require.Equal(t, reportedUsage(40, 10, 0, 7, 0.25), byID[priced].AgentRunUsage)
	encoded, err := json.Marshal(byID[resumed])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"input_tokens":400`)
	require.NotContains(t, string(encoded), "cost_usd", "an unreported cost is absent, not zero")

	total := reportedUsage(1240, 136, 2400, 7, 0.25)
	require.Equal(t, total, runs.Usage, "the thread's 1,200 input tokens once, not the 2,400 its totals add up to")
	older, err := repo.Runs(ctx, task.WorkspaceID, task.ID, &priced)
	require.NoError(t, err)
	require.Len(t, older.Runs, 4)
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
