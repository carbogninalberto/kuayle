package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

const piModelFormat = "Pi takes its model as provider/model, for example opencode-go/deepseek-v4.1-flash"

// Pi, like OpenCode, takes a provider/model and an effort and brings its own
// providers, so it never runs on a workspace model server.
func TestValidateAgentModelGivesPiAProviderModelAndNoModelServer(t *testing.T) {
	ws := uuid.New()
	server := deepSeekModelServer(ws)
	store := &devMachineStoreFake{modelServers: []domain.AgentModelServer{server}}
	svc := NewDevMachineService(store, agent.NewRegistry(agent.NewPiProvider("")),
		true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})

	for _, tc := range []struct {
		name            string
		server          *uuid.UUID
		model, thinking string
		reason          string
	}{
		{name: "model and effort", model: openCodeModel, thinking: "xhigh"},
		{name: "nothing chosen"},
		{name: "effort only", thinking: "off"},
		{name: "model with a nested name", model: "openrouter/anthropic/claude-sonnet-5"},
		{name: "model without a provider", model: "claude-sonnet-5", reason: piModelFormat},
		{name: "model read as a flag", model: "--extension/x", reason: "the model name is not valid"},
		{name: "unknown effort", model: openCodeModel, thinking: "minimal", reason: "the effort is one of off, low, medium, high, xhigh, max"},
		{name: "on a model server", server: &server.ID, model: openCodeModel, reason: "Pi does not run on a model server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateAgentModel(context.Background(), ws, "pi", tc.server, tc.model, tc.thinking)
			if tc.reason == "" {
				require.NoError(t, err)
				return
			}
			var refused *AgentModelError
			require.ErrorAs(t, err, &refused)
			require.Equal(t, tc.reason, refused.Reason)
		})
	}
}

// A Pi run started on a machine carries its model and effort to the command
// and never inherits the workspace's model server or another agent's default.
func TestPiRunCarriesItsModelAndEffortAndNoModelServer(t *testing.T) {
	ws, machine, user := uuid.New(), uuid.New(), uuid.New()
	store := ownSignInClaudeStore(ws, machine, user)
	store.policy.AllowedProviders = json.RawMessage(`["pi","claude-code"]`)
	store.agentProvider.ProviderID = "pi"
	store.modelServers = []domain.AgentModelServer{deepSeekModelServer(ws)}
	svc := NewDevMachineService(store, agent.NewRegistry(agent.NewClaudeCodeProvider(""), agent.NewPiProvider("")), true,
		"machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})
	request := func(model, thinking string) dto.CreateAgentRunRequest {
		return dto.CreateAgentRunRequest{UseRootWorkspace: true, PushBranch: dmBoolPtr(false), Provider: "pi",
			Mode: "autonomous", Prompt: "Implement it", MaxRuntimeSeconds: 30, Model: model, Thinking: thinking}
	}

	run, err := svc.createAgentRun(context.Background(), ws, machine, user, request(openCodeModel, "high"), uuid.New())
	require.NoError(t, err)
	argv := runArgv(t, run)
	require.Equal(t, []string{"pi", "--print", "--mode", "json", "--no-session", "--no-approve", "--model", openCodeModel, "--thinking", "high", "--"}, argv[:len(argv)-1])
	require.Contains(t, argv[len(argv)-1], "Implement it")
	require.Nil(t, run.ModelServerID)
	require.Equal(t, openCodeModel, run.Model)
	require.Equal(t, "high", run.Thinking)

	run, err = svc.createAgentRun(context.Background(), ws, machine, user, request("", ""), uuid.New())
	require.NoError(t, err)
	argv = runArgv(t, run)
	require.Equal(t, []string{"pi", "--print", "--mode", "json", "--no-session", "--no-approve", "--"}, argv[:len(argv)-1])
	require.Empty(t, run.Model, "no default of the model server or of another agent is inherited")
	require.Empty(t, run.Thinking)
}

// The profile sheet offers a model for Pi and shows its provider/model form.
func TestAgentProfilesOfferPiAModelInItsForm(t *testing.T) {
	workflows := NewWorkflowService(nil, agent.NewRegistry(agent.NewPiProvider(""), agent.NewGooseProvider("")), nil)

	require.Equal(t, []string{"pi"}, workflows.modelHarnesses())
	require.Equal(t, map[string]HarnessModelFormat{"pi": {Format: "provider/model", Example: openCodeModel}}, workflows.modelFormats())
}
