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

// KUA-188: a NUL in agent output reached the jsonb insert as the escape
// \u0000, PostgreSQL rejected it (22P05) and the run stopped.
func TestAgentOutputWithNULIsPersistedWithoutIt(t *testing.T) {
	key := cryptoutil.DeriveKey("nul-output-test")
	encrypted, err := cryptoutil.Encrypt("fixture-model-key", key)
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New(), ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30, ModelServerID: serverRef()}
	store := &managerStoreFake{machine: &domain.DevMachine{ID: run.MachineID, WorkspaceID: run.WorkspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}, agentRun: run, provider: &domain.DevMachineAgentProvider{ProviderID: "codex", ImageRef: "test"}, modelServer: &domain.AgentModelServer{EncryptedAPIKey: encrypted, DefaultModel: "deepseek-flash", Thinking: "high"}}
	runtime := &runtimeFake{agentExecution: &AgentExecution{
		ContainerID: "native",
		Stdout: `{"type":"item.completed","item":{"text":"a\u0000b","k\u0000":"v"}}` + "\n" +
			`{"type":"tool\u0000.output","n":12345678901234567890}` + "\n" +
			"hello\x00world\n",
		Stderr: "warn\x00ing\n",
	}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, key, nil, "test")

	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{AgentRunID: &run.ID, Generation: 1}))

	// Content: raw NUL bytes become U+FFFD before persistence, so replaying
	// an already stored line compares equal.
	var contents []string
	for _, chunk := range store.outputChunks {
		require.NotContains(t, chunk.Content, "\x00")
		contents = append(contents, chunk.Content)
	}
	require.Contains(t, contents, "hello\uFFFDworld\n", "stdout content")
	require.Contains(t, contents, "warn\uFFFDing\n", "stderr content")

	// Event payloads: no NUL in any form reaches jsonb.
	byType := map[string]json.RawMessage{}
	for _, event := range store.events {
		require.NotContains(t, event.EventType, "\x00")
		require.NotContains(t, string(event.Payload), `\u0000`)
		require.NotContains(t, string(event.Payload), "\x00")
		byType[event.EventType] = event.Payload
	}
	completed, ok := byType["item.completed"]
	require.True(t, ok, "events: %v", byType)
	require.True(t, json.Valid(completed))
	var decoded struct {
		Item map[string]string `json:"item"`
	}
	require.NoError(t, json.Unmarshal(completed, &decoded))
	require.Equal(t, map[string]string{"text": "ab", "k": "v"}, decoded.Item)

	output, ok := byType["tool.output"]
	require.True(t, ok, "a NUL in the event type is stripped: %v", byType)
	require.Contains(t, string(output), `"n":12345678901234567890`)
}

func TestAgentEventRecordStripsNULFromTypeAndMessageFallback(t *testing.T) {
	for name, parsed := range map[string]agent.Event{
		"empty payload":   {Type: "note", Message: "a\x00b"},
		"invalid payload": {Type: "note", Message: "a\x00b", Payload: json.RawMessage(`{"x":"\u0000"`)},
	} {
		eventType, payload := agentEventRecord(parsed)
		require.Equal(t, "note", eventType, name)
		require.JSONEq(t, `{"message":"ab"}`, string(payload), name)
	}

	eventType, _ := agentEventRecord(agent.Event{Type: "\x00"})
	require.Equal(t, "provider.event", eventType, "a type made only of NUL falls back")
	eventType, _ = agentEventRecord(agent.Event{Type: strings.Repeat("t", 129)})
	require.Equal(t, "provider.event", eventType)

	clean := json.RawMessage(`{ "type": "x",  "a": 1 }`)
	_, payload := agentEventRecord(agent.Event{Type: "x", Payload: clean})
	require.Equal(t, clean, payload, "payloads without NUL are stored byte for byte")
}
