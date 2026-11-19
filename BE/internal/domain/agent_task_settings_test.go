package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentSettingsOmitRetiredTestCommands(t *testing.T) {
	for _, value := range []any{AgentTask{}, AgentTaskRequest{}, AgentTaskRetryRequest{}, DevMachineScopeSetting{}, DevMachineAgentRun{}} {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fields))
		require.NotContains(t, fields, "test_command")
		require.NotContains(t, fields, "validation_command")
	}
}
