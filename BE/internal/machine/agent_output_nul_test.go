package machine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestAgentRunContinuesAfterOutputWithNUL(t *testing.T) {
	key := cryptoutil.DeriveKey("nul-output-test")
	encrypted, err := cryptoutil.Encrypt("fixture-model-key", key)
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New(), ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30, ModelServerID: serverRef()}
	store := &managerStoreFake{machine: &domain.DevMachine{ID: run.MachineID, WorkspaceID: run.WorkspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}, agentRun: run, provider: &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"}, modelServer: &domain.AgentModelServer{EncryptedAPIKey: encrypted, DefaultModel: "deepseek-flash", Thinking: "high"}}
	runtime := &runtimeFake{agentExecution: &AgentExecution{
		ContainerID: "native",
		Stdout: `{"type":"tool\u0000.output","text":"a\u0000b\ud800"}` + "\n" +
			"hello\x00world\n" +
			`{"type":"item.completed","item":{"type":"agent_message","text":"Finished the issue."}}` + "\n",
		Stderr: "warn\x00ing\n",
	}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")

	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{AgentRunID: &run.ID, Generation: 1}))
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, run.Status)
	require.NotNil(t, run.Summary)
	require.Equal(t, "Finished the issue.", *run.Summary)

	// Content: raw NUL bytes become U+FFFD before persistence, so replaying
	// an already stored line compares equal.
	var contents []string
	for _, chunk := range store.outputChunks {
		require.NotContains(t, chunk.Content, "\x00")
		contents = append(contents, chunk.Content)
	}
	require.Contains(t, contents, "hello\uFFFDworld\n", "stdout content")
	require.Contains(t, contents, "warn\uFFFDing\n", "stderr content")

	var types []string
	for _, event := range store.events {
		types = append(types, event.EventType)
	}
	require.Contains(t, types, "tool.output")
	require.Contains(t, types, "item.completed")
}

func TestAgentEventRecordPreservesPayloadAndFallsBackToMessage(t *testing.T) {
	for name, parsed := range map[string]agent.Event{
		"empty payload":   {Type: "note", Message: "a\x00b"},
		"invalid payload": {Type: "note", Message: "a\x00b", Payload: json.RawMessage(`{"x":"\u0000"`)},
	} {
		eventType, payload := agentEventRecord(parsed)
		require.Equal(t, "note", eventType, name)
		require.JSONEq(t, `{"message":"a\u0000b"}`, string(payload), name)
	}

	eventType, _ := agentEventRecord(agent.Event{Type: "\x00"})
	require.Equal(t, "provider.event", eventType, "a type made only of NUL falls back")
	eventType, _ = agentEventRecord(agent.Event{Type: strings.Repeat("t", 129)})
	require.Equal(t, "provider.event", eventType)

	raw := json.RawMessage(`{ "type": "x",  "a": "\u0000\ud800" }`)
	_, payload := agentEventRecord(agent.Event{Type: "x", Payload: raw})
	require.Equal(t, raw, payload)
}
