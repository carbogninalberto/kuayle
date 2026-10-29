package service

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Production: a Claude Code attempt was OOM-killed and the task said only
// "Native coding attempt failed". The memory limit is now the reason.
func TestAgentTaskFailsWithTheRunsResourceLimit(t *testing.T) {
	message := domain.MemoryLimitMessage("agent", 1503238553, true)
	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "claude-code"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, message, store.failReason)
	require.False(t, store.signInRequired)

	other := "agent_failed: exit 1"
	store = &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "claude-code"}}
	runtime = &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &other}}
	svc = &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, "Native coding attempt failed; stopping execution before retry", store.failReason, "other run errors stay internal")
}
