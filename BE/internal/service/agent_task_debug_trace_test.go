package service

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The task carries the run's class and real cause, not only a generic
// "Execution failed".
func TestAgentTaskCarriesTheRunsFailureClassAndCause(t *testing.T) {
	class := string(domain.FailurePlatform)
	cause := "The agent container reached its limit of 1024 processes and threads (PID limit), so the harness could not start another and crashed (exit code 139, SIGSEGV)"
	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "opencode"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{
		Status:       domain.DevMachineAgentRunStatusFailed,
		ErrorMessage: &cause,
		FailureClass: &class,
		FailureCause: &cause,
	}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, domain.FailurePlatform, store.failClass)
	require.Equal(t, cause, store.failReason)
	require.NotContains(t, store.failReason, "Execution failed; runtime cleanup confirmed")
}

// The run's classes carry into the specific reasons the task already gives.
func TestAgentTaskSpecificReasonsKeepTheirClass(t *testing.T) {
	message := domain.NoFinalMessageReason("The harness reported an error: APIError: 429 overloaded")
	class := string(domain.FailureProvider)
	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "opencode"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{
		Status:       domain.DevMachineAgentRunStatusFailed,
		ErrorMessage: &message,
		FailureClass: &class,
		FailureCause: &message,
	}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, message, store.failReason)
	require.Equal(t, domain.FailureProvider, store.failClass)

	memory := domain.MemoryLimitMessage("agent", 1503238553, true)
	store = &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "claude-code"}}
	runtime = &taskRuntimeFake{run: &domain.DevMachineAgentRun{
		Status:       domain.DevMachineAgentRunStatusFailed,
		ErrorMessage: &memory,
	}}
	svc = &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, memory, store.failReason)
	require.Equal(t, domain.FailurePlatform, store.failClass)
}
