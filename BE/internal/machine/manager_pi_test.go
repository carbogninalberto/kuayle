package machine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Pi 1.6.0's own events (pi --print --mode json): a tool call, then the
// conclusion; and a model provider that keeps failing, after which Pi still
// exits 0.
const (
	piToolCallEvent   = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hello"}}],"api":"openai-completions","provider":"fake","model":"m1","stopReason":"toolUse","timestamp":1795535652795}}`
	piToolEndEvent    = `{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hello\n"}]},"isError":false,"durationMs":10}`
	piConclusionEvent = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Added the toggle; tests pass."}],"api":"openai-completions","provider":"fake","model":"m1","stopReason":"stop","timestamp":1795535652853}}`
	piErrorEvent      = `{"type":"message_end","message":{"role":"assistant","content":[],"api":"openai-completions","provider":"fake","model":"m1","stopReason":"error","timestamp":1795535681220,"errorMessage":"500: {\"message\":\"upstream exploded\"}"}}`
)

func runPiAgent(t *testing.T, stdout string) *domain.DevMachineAgentRun {
	t.Helper()
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		agentRun: &domain.DevMachineAgentRun{
			ID: runID, MachineID: machineID, WorkspaceID: workspaceID,
			ProviderID: "pi", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30,
		},
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "pi", ImageRef: "kuayle/headless:test", Enabled: true},
	}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: 0, Stdout: stdout}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewPiProvider("kuayle/headless:test")), nil, make([]byte, 32), make([]byte, 32), "test")

	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}))
	require.Equal(t, 1, runtime.agentRuns)
	require.NotNil(t, store.completedRun)
	return store.completedRun
}

func TestPiAutonomousRunSucceedsWithItsConclusion(t *testing.T) {
	run := runPiAgent(t, piToolCallEvent+"\n"+piToolEndEvent+"\n"+piConclusionEvent+"\n")

	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, run.Status)
	require.Equal(t, "Added the toggle; tests pass.", *run.Summary)
	require.Nil(t, run.ErrorMessage)
}

func TestPiAutonomousRunThatOnlyFailedReportsTheProviderError(t *testing.T) {
	run := runPiAgent(t, piToolCallEvent+"\n"+piToolEndEvent+"\n"+piErrorEvent+"\n")

	require.Equal(t, domain.DevMachineAgentRunStatusFailed, run.Status)
	require.NotNil(t, run.ErrorMessage)
	require.True(t, domain.IsNoFinalMessageReason(*run.ErrorMessage))
	require.Contains(t, *run.ErrorMessage, `The harness reported an error: 500: {"message":"upstream exploded"}`)
}
