package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type usageLimitStoreFake struct {
	*taskStoreFake
	paused  *domain.UsageLimit
	reason  string
	waiting string
}

func (f *usageLimitStoreFake) PauseForUsageLimit(_ context.Context, _ domain.AgentTask, limit domain.UsageLimit, reason, waiting string) error {
	f.paused, f.reason, f.waiting = &limit, reason, waiting
	f.task.State = "cancelling"
	return nil
}

// A run the provider refused at its usage limit pauses the task instead of
// failing it, also for a task in a workflow.
func TestAgentTaskPausesAtTheProviderUsageLimit(t *testing.T) {
	reset := time.Date(2026, 11, 10, 4, 10, 0, 0, time.UTC)
	message := domain.UsageLimit{Message: "You've hit your 5-hour limit · resets 4:10am (UTC)", ResetsAt: &reset}.ErrorMessage()
	for _, task := range []domain.AgentTask{
		{State: "running", Provider: "claude-code"},
		{State: "running", Provider: "claude-code", WorkflowVersionID: ptr(uuid.New())},
	} {
		store := &usageLimitStoreFake{taskStoreFake: &taskStoreFake{task: task}}
		runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message}}
		svc := &AgentTaskService{store: store, runtime: runtime}
		require.NoError(t, svc.step(context.Background(), store.task))
		require.NotNil(t, store.paused, "the task must pause, not fail")
		require.Empty(t, store.failReason)
		require.Equal(t, "You've hit your 5-hour limit · resets 4:10am (UTC)", store.paused.Message)
		require.True(t, store.paused.ResetsAt.Equal(reset))
		require.Contains(t, store.reason, "Claude Code usage limit reached")
		require.Contains(t, store.reason, "resumes automatically")
		require.Equal(t, "Waiting for the Claude Code usage limit to reset", store.waiting)
	}

	// After the automatic resume cap the user resumes it.
	store := &usageLimitStoreFake{taskStoreFake: &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "codex", UsageLimitResumes: domain.MaxUsageLimitAutoResumes}}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message}}
	require.NoError(t, (&AgentTaskService{store: store, runtime: runtime}).step(context.Background(), store.task))
	require.Contains(t, store.reason, "Codex usage limit reached")
	require.Contains(t, store.reason, "Resume it once the limit resets")
}

// A resumed attempt continues the paused attempt's native session, even
// though that attempt did not succeed, and is told to keep its work.
func TestUsageLimitedAttemptResumesItsSession(t *testing.T) {
	machine := uuid.New()
	previous := &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed", MachineID: machine, NativeSessionID: "ae361499-0008-48b7-b8a7-d9df1193e4d3"}
	paused := domain.AgentTask{ID: uuid.New(), MachineID: &machine, Provider: "claude-code", ResumeNativeSession: true}
	require.Equal(t, previous.NativeSessionID, taskSession(paused, previous).ResumeID)

	failed := paused
	failed.ResumeNativeSession = false
	require.Empty(t, taskSession(failed, previous).ResumeID, "an ordinary failure still starts a fresh session")

	prompt := withHandoff("Implement KUA-98", previous, true, true)
	require.Contains(t, prompt, "this continues your previous session")
	require.Contains(t, prompt, "paused by the provider's usage limit")
	require.Contains(t, prompt, "do not start over")
	require.NotContains(t, withHandoff("Implement KUA-98", previous, false, false), "usage limit")
}
