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

const claudeTokenFixture = "sk-ant-oat01-fixture-token-value-0001"

// claudeSignInFixture is a Claude Code run without a model server whose
// output echoes the token, so redaction can be observed.
func claudeSignInFixture(t *testing.T) (*managerStoreFake, *runtimeFake, *Manager, *domain.DevMachineAgentRun, []byte) {
	t.Helper()
	store, runtime, _, run, key := signInRunFixture(t)
	run.ProviderID = "claude-code"
	run.CommandArgv = json.RawMessage(`["claude"]`)
	store.provider = &domain.DevMachineAgentProvider{MachineID: run.MachineID, ProviderID: "claude-code", ImageRef: "kuayle/claude:test", Enabled: true}
	runtime.agentExecution.Stdout = "authenticated with " + claudeTokenFixture + "\n"
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewClaudeCodeProvider("kuayle/claude:test")), nil, key, nil, "test")
	return store, runtime, manager, run, key
}

func runClaudeSignIn(t *testing.T, manager *Manager, store *managerStoreFake, run *domain.DevMachineAgentRun) error {
	t.Helper()
	return manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: run.MachineID, WorkspaceID: run.WorkspaceID, AgentRunID: &run.ID, Generation: 1,
	})
}

func TestManagerPrefersTheClaudeCodeTokenOverTheCapturedFile(t *testing.T) {
	store, runtime, manager, run, key := claudeSignInFixture(t)
	file, err := cryptoutil.Encrypt(`{"claudeAiOauth":{"accessToken":"fixture-access-token-0001"}}`, key)
	require.NoError(t, err)
	require.NoError(t, store.SaveAgentSignIn(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "claude-code", file))
	token, err := cryptoutil.Encrypt(claudeTokenFixture, key)
	require.NoError(t, err)
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "claude-code", token, time.Now().Add(time.Hour)))
	runtime.returnedSignIn = `{"claudeAiOauth":{"accessToken":"fixture-access-token-0002"}}`

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, claudeTokenFixture, runtime.agentSecrets["CLAUDE_CODE_OAUTH_TOKEN"])
	require.NotContains(t, runtime.agentSecrets, "KUAYLE_CLAUDE_CREDENTIALS", "the file is not installed when a token is saved")
	require.Len(t, store.outputChunks, 1)
	require.NotContains(t, store.outputChunks[0].Content, claudeTokenFixture)
	require.Contains(t, store.outputChunks[0].Content, "[REDACTED]")
	require.Equal(t, file, store.signIns[run.RequestedByUserID.String()+"/claude-code"], "a token run hands nothing back")
}

func TestManagerFallsBackToTheCapturedClaudeCodeFile(t *testing.T) {
	store, runtime, manager, run, key := claudeSignInFixture(t)
	content := `{"claudeAiOauth":{"accessToken":"fixture-access-token-0001"}}`
	file, err := cryptoutil.Encrypt(content, key)
	require.NoError(t, err)
	require.NoError(t, store.SaveAgentSignIn(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "claude-code", file))

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, content, runtime.agentSecrets["KUAYLE_CLAUDE_CREDENTIALS"])
	require.NotContains(t, runtime.agentSecrets, "CLAUDE_CODE_OAUTH_TOKEN")
}

func TestManagerAsksForAClaudeCodeSignInWithoutTokenOrFile(t *testing.T) {
	store, runtime, manager, run, _ := claudeSignInFixture(t)
	var terminal *terminalOperationError
	require.ErrorAs(t, runClaudeSignIn(t, manager, store, run), &terminal)
	require.Equal(t, "sign_in_required", terminal.code)
	require.Zero(t, runtime.agentRuns)
}

func TestManagerKeepsTheAPIKeyBypassBeforeTheToken(t *testing.T) {
	store, runtime, manager, run, key := claudeSignInFixture(t)
	apiKey, err := cryptoutil.Encrypt("fixture-anthropic-key", key)
	require.NoError(t, err)
	run.AllowedSecrets = json.RawMessage(`["ANTHROPIC_API_KEY"]`)
	store.envVars = []domain.DevMachineEnvVar{{Name: "ANTHROPIC_API_KEY", EncryptedValue: apiKey}}
	token, err := cryptoutil.Encrypt(claudeTokenFixture, key)
	require.NoError(t, err)
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "claude-code", token, time.Now().Add(time.Hour)))

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, "fixture-anthropic-key", runtime.agentSecrets["ANTHROPIC_API_KEY"])
	require.NotContains(t, runtime.agentSecrets, "CLAUDE_CODE_OAUTH_TOKEN")
}

const openCodeKeyFixture = "fixture-opencode-api-key-0001"

// openCodeKeyFixtureRun is an OpenCode run whose output echoes the key, so
// redaction can be observed.
func openCodeKeyFixtureRun(t *testing.T) (*managerStoreFake, *runtimeFake, *Manager, *domain.DevMachineAgentRun, []byte) {
	t.Helper()
	store, runtime, _, run, key := signInRunFixture(t)
	run.ProviderID = "opencode"
	run.CommandArgv = json.RawMessage(`["opencode"]`)
	store.provider = &domain.DevMachineAgentProvider{MachineID: run.MachineID, ProviderID: "opencode", ImageRef: "kuayle/opencode:test", Enabled: true}
	runtime.agentExecution.Stdout = "authenticated with " + openCodeKeyFixture + "\n"
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, key, nil, "test")
	return store, runtime, manager, run, key
}

func saveOpenCodeKey(t *testing.T, store *managerStoreFake, run *domain.DevMachineAgentRun, key []byte) {
	t.Helper()
	sealed, err := cryptoutil.Encrypt(openCodeKeyFixture, key)
	require.NoError(t, err)
	require.NoError(t, store.SaveAgentSignInToken(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "opencode", sealed, time.Time{}))
}

func TestManagerInjectsTheSavedOpenCodeKey(t *testing.T) {
	store, runtime, manager, run, key := openCodeKeyFixtureRun(t)
	saveOpenCodeKey(t, store, run, key)
	runtime.returnedSignIn = `{"unexpected":"fixture-returned-sign-in"}`

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, 1, runtime.agentRuns)
	require.Equal(t, openCodeKeyFixture, runtime.agentSecrets["OPENCODE_API_KEY"])
	for name := range runtime.agentSecrets {
		require.NotContains(t, name, "KUAYLE_SIGN_IN_SECRET_", "a key is not a credentials file")
	}
	require.Len(t, store.outputChunks, 1)
	require.NotContains(t, store.outputChunks[0].Content, openCodeKeyFixture)
	require.Contains(t, store.outputChunks[0].Content, "[REDACTED]")
	require.NotEmpty(t, runtime.returnedSignIn, "a key run hands nothing back")
}

// OpenCode can be configured in other ways, so a run without a saved key
// starts anyway instead of asking the user to sign in.
func TestManagerRunsOpenCodeWithoutASavedKey(t *testing.T) {
	store, runtime, manager, run, _ := openCodeKeyFixtureRun(t)

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, 1, runtime.agentRuns)
	require.NotContains(t, runtime.agentSecrets, "OPENCODE_API_KEY")
}

func TestManagerUsesOnlyTheRequestingUsersOpenCodeKey(t *testing.T) {
	t.Run("another user", func(t *testing.T) {
		store, runtime, manager, run, key := openCodeKeyFixtureRun(t)
		saveOpenCodeKey(t, store, run, key)
		other := uuid.New()
		run.RequestedByUserID = &other

		require.NoError(t, runClaudeSignIn(t, manager, store, run))
		require.Equal(t, 1, runtime.agentRuns)
		require.NotContains(t, runtime.agentSecrets, "OPENCODE_API_KEY", "another user's key is never used")
	})
	t.Run("no user", func(t *testing.T) {
		store, runtime, manager, run, key := openCodeKeyFixtureRun(t)
		saveOpenCodeKey(t, store, run, key)
		run.RequestedByUserID = nil

		require.NoError(t, runClaudeSignIn(t, manager, store, run))
		require.Equal(t, 1, runtime.agentRuns)
		require.NotContains(t, runtime.agentSecrets, "OPENCODE_API_KEY")
	})
}

// A saved OpenCode key belongs to OpenCode runs only: Claude Code and Codex
// runs of the same user never receive it.
func TestManagerKeepsTheOpenCodeKeyOutOfOtherHarnesses(t *testing.T) {
	t.Run("claude-code", func(t *testing.T) {
		store, runtime, manager, run, key := claudeSignInFixture(t)
		saveOpenCodeKey(t, store, run, key)
		token, err := cryptoutil.Encrypt(claudeTokenFixture, key)
		require.NoError(t, err)
		require.NoError(t, store.SaveAgentSignInToken(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "claude-code", token, time.Now().Add(time.Hour)))

		require.NoError(t, runClaudeSignIn(t, manager, store, run))
		require.Equal(t, 1, runtime.agentRuns)
		require.NotContains(t, runtime.agentSecrets, "OPENCODE_API_KEY")
		for _, value := range runtime.agentSecrets {
			require.NotEqual(t, openCodeKeyFixture, value)
		}
	})
	t.Run("codex", func(t *testing.T) {
		store, runtime, manager, run, key := signInRunFixture(t)
		saveOpenCodeKey(t, store, run, key)
		file, err := cryptoutil.Encrypt(`{"tokens":{"access_token":"fixture-access-token-0001"}}`, key)
		require.NoError(t, err)
		require.NoError(t, store.SaveAgentSignIn(context.Background(), run.WorkspaceID, *run.RequestedByUserID, "codex", file))

		require.NoError(t, runClaudeSignIn(t, manager, store, run))
		require.Equal(t, 1, runtime.agentRuns)
		require.NotContains(t, runtime.agentSecrets, "OPENCODE_API_KEY")
		for _, value := range runtime.agentSecrets {
			require.NotEqual(t, openCodeKeyFixture, value)
		}
	})
}

// As with ANTHROPIC_API_KEY for Claude Code, an OPENCODE_API_KEY secret the
// run was allowed to use comes before the saved key.
func TestManagerKeepsAnOpenCodeKeySecretBeforeTheSavedKey(t *testing.T) {
	store, runtime, manager, run, key := openCodeKeyFixtureRun(t)
	saveOpenCodeKey(t, store, run, key)
	secret, err := cryptoutil.Encrypt("fixture-opencode-machine-key", key)
	require.NoError(t, err)
	run.AllowedSecrets = json.RawMessage(`["OPENCODE_API_KEY"]`)
	store.envVars = []domain.DevMachineEnvVar{{Name: "OPENCODE_API_KEY", EncryptedValue: secret}}

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, "fixture-opencode-machine-key", runtime.agentSecrets["OPENCODE_API_KEY"])
}

// A machine secret the run was not allowed to use does not hide the saved key.
func TestManagerUsesTheSavedOpenCodeKeyWhenTheMachineSecretIsNotAllowed(t *testing.T) {
	store, runtime, manager, run, key := openCodeKeyFixtureRun(t)
	saveOpenCodeKey(t, store, run, key)
	secret, err := cryptoutil.Encrypt("fixture-opencode-machine-key", key)
	require.NoError(t, err)
	store.envVars = []domain.DevMachineEnvVar{{Name: "OPENCODE_API_KEY", EncryptedValue: secret}}

	require.NoError(t, runClaudeSignIn(t, manager, store, run))
	require.Equal(t, openCodeKeyFixture, runtime.agentSecrets["OPENCODE_API_KEY"])
}
