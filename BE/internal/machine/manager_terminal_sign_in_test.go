package machine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

const (
	terminalClaudeFile = `{"claudeAiOauth":{"accessToken":"fixture-terminal-access-0001"}}`
	terminalCodexFile  = `{"tokens":{"access_token":"fixture-terminal-codex-0001"}}`
)

// terminalSignInFixture is a running standard machine owned by a user whose
// workspace allows Claude Code and Codex.
func terminalSignInFixture(t *testing.T) (*managerStoreFake, *runtimeFake, *Manager, uuid.UUID, []byte) {
	t.Helper()
	owner := uuid.New()
	workspaceID := uuid.New()
	ide := "container-ide"
	store := &managerStoreFake{
		machine: &domain.DevMachine{
			ID: uuid.New(), WorkspaceID: workspaceID, CreatedByUserID: &owner, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		services: []domain.DevMachineService{{ServiceKey: "ide", ServiceType: "ide", ContainerID: &ide, Status: "running"}},
		policy:   &domain.DevMachineWorkspacePolicy{WorkspaceID: workspaceID, AllowedProviders: json.RawMessage(`["claude-code","codex","opencode"]`)},
	}
	key := cryptoutil.DeriveKey("terminal-sign-in-test")
	runtime := &runtimeFake{}
	manager := NewManager(store, runtime, agent.NewRegistry(), nil, key, nil, "test")
	return store, runtime, manager, owner, key
}

func sealSignIn(t *testing.T, key []byte, value string) string {
	t.Helper()
	sealed, err := cryptoutil.Encrypt(value, key)
	require.NoError(t, err)
	return sealed
}

func syncTerminalSignIns(t *testing.T, manager *Manager, store *managerStoreFake) error {
	t.Helper()
	return manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpSyncAgentSignIns,
		Generation: store.machine.Generation,
	})
}

// The owner's terminal gets the owner's own sign-ins, read only for the
// machine's workspace and owner: another user's sign-ins never reach it.
func TestManagerSyncsTheOwnersSignInsIntoTheirTerminal(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	ctx := context.Background()
	other := uuid.New()
	require.NoError(t, store.SaveAgentSignInToken(ctx, store.machine.WorkspaceID, owner, "claude-code", sealSignIn(t, key, claudeTokenFixture), time.Now().Add(time.Hour)))
	require.NoError(t, store.SaveAgentSignIn(ctx, store.machine.WorkspaceID, owner, "codex", sealSignIn(t, key, terminalCodexFile)))
	require.NoError(t, store.SaveAgentSignIn(ctx, store.machine.WorkspaceID, other, "claude-code", sealSignIn(t, key, terminalClaudeFile)))

	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Equal(t, map[string]string{
		"managed":           "claude-code\ncodex\n",
		"claude-code.token": claudeTokenFixture,
		"codex.auth.json":   terminalCodexFile,
	}, runtime.terminalSignIns)
	require.NotEmpty(t, store.signInReads)
	for _, read := range store.signInReads {
		require.Equal(t, store.machine.WorkspaceID.String()+"/"+owner.String(), read, "only the owner's sign-ins in the machine's workspace are read")
	}
}

// Like a run, the terminal uses a saved Claude Code token before the
// captured credentials file, and the file when there is no token.
func TestManagerSyncsTheCapturedClaudeFileWithoutAToken(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	require.NoError(t, store.SaveAgentSignIn(context.Background(), store.machine.WorkspaceID, owner, "claude-code", sealSignIn(t, key, terminalClaudeFile)))

	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Equal(t, map[string]string{
		"managed":                      "claude-code\ncodex\n",
		"claude-code.credentials.json": terminalClaudeFile,
	}, runtime.terminalSignIns)
}

// A sign-in the user removed in Kuayle is removed from the terminal by the
// next sync: the terminal gets an empty set for that agent.
func TestManagerSyncRemovesARevokedSignIn(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), store.machine.WorkspaceID, owner, "claude-code", sealSignIn(t, key, claudeTokenFixture), time.Now().Add(time.Hour)))
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Contains(t, runtime.terminalSignIns, "claude-code.token")

	store.signIns = nil
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Equal(t, map[string]string{"managed": "claude-code\ncodex\n"}, runtime.terminalSignIns)
}

// An agent the workspace policy does not allow gets no sign-in, and no
// sign-in hint, in the terminal.
func TestManagerSyncsOnlyAgentsThePolicyAllows(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	store.policy.AllowedProviders = json.RawMessage(`["claude-code"]`)
	require.NoError(t, store.SaveAgentSignIn(context.Background(), store.machine.WorkspaceID, owner, "codex", sealSignIn(t, key, terminalCodexFile)))

	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Equal(t, map[string]string{"managed": "claude-code\n"}, runtime.terminalSignIns)

	store.policy = nil
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Empty(t, runtime.terminalSignIns, "a workspace without a policy allows no agent")
}

// An agent the terminal already reaches through the workspace's model server
// or an API key secret keeps that configuration: no sign-in is added.
func TestManagerLeavesAgentsConfiguredByAModelServerOrAPIKey(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), store.machine.WorkspaceID, owner, "claude-code", sealSignIn(t, key, claudeTokenFixture), time.Now().Add(time.Hour)))
	require.NoError(t, store.SaveAgentSignIn(context.Background(), store.machine.WorkspaceID, owner, "codex", sealSignIn(t, key, terminalCodexFile)))

	store.modelServer = deepSeekServer(store.machine.WorkspaceID, sealSignIn(t, key, "sk-model"), "deepseek-v4-pro", "high")
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Empty(t, runtime.terminalSignIns, "the model server serves both agents")

	store.modelServer.AnthropicBaseURL = nil
	store.envVars = []domain.DevMachineEnvVar{{Name: "ANTHROPIC_API_KEY", EncryptedValue: sealSignIn(t, key, "fixture-anthropic-key")}}
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Empty(t, runtime.terminalSignIns, "an API key secret configures Claude Code; the server still serves Codex")

	store.modelServer = nil
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Equal(t, map[string]string{"managed": "codex\n", "codex.auth.json": terminalCodexFile}, runtime.terminalSignIns)
}

// A machine without an owner gets no sign-in, and a sync for a machine that
// is not running leaves its containers alone: its next start syncs.
func TestManagerSyncsNothingForUnownedOrStoppedMachines(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), store.machine.WorkspaceID, owner, "claude-code", sealSignIn(t, key, claudeTokenFixture), time.Now().Add(time.Hour)))

	store.machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Zero(t, runtime.terminalSignInSyncs)

	store.machine.Status = domain.DevMachineStatusRunning
	store.machine.CreatedByUserID = nil
	require.NoError(t, syncTerminalSignIns(t, manager, store))
	require.Equal(t, 1, runtime.terminalSignInSyncs)
	require.Empty(t, runtime.terminalSignIns)
	require.Empty(t, store.signInReads)
}

// Starting a machine writes the owner's sign-ins into its new terminal home.
func TestManagerSyncsTerminalSignInsWhenTheMachineStarts(t *testing.T) {
	store, runtime, manager, owner, key := terminalSignInFixture(t)
	store.machine.Status = domain.DevMachineStatusQueued
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), store.machine.WorkspaceID, owner, "claude-code", sealSignIn(t, key, claudeTokenFixture), time.Now().Add(time.Hour)))

	require.NoError(t, manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpSpawn, Generation: 1,
	}))
	require.Equal(t, 1, runtime.spawned)
	require.Equal(t, 1, runtime.terminalSignInSyncs)
	require.Equal(t, claudeTokenFixture, runtime.terminalSignIns["claude-code.token"])
	for _, secrets := range runtime.spawnSecrets {
		require.NotContains(t, secrets, "CLAUDE_CODE_OAUTH_TOKEN", "the sign-in is not a container-wide secret")
		for _, value := range secrets {
			require.NotEqual(t, claudeTokenFixture, value)
		}
	}
}
