package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

// signInSecretNames carries a stored sign-in into an agent run; the runner
// writes it to the agent's file in the run's in-memory home.
var signInSecretNames = map[string]string{
	"claude-code": "KUAYLE_CLAUDE_CREDENTIALS",
	"codex":       "KUAYLE_CODEX_AUTH",
}

// apiKeySecrets let an agent authenticate without a sign-in.
var apiKeySecrets = map[string][]string{
	"claude-code": {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"},
	"codex":       {"OPENAI_API_KEY", "CODEX_API_KEY"},
}

var agentNames = map[string]string{"claude-code": "Claude Code", "codex": "Codex"}

const maxSignInBytes = 64 * 1024

// validSignIn accepts only a JSON object of bounded size: the agents' own
// credential files.
func validSignIn(content string) bool {
	content = strings.TrimSpace(content)
	var object map[string]any
	return content != "" && len(content) <= maxSignInBytes && json.Unmarshal([]byte(content), &object) == nil
}

// signInTokens are the credential strings inside a sign-in file, redacted
// from run output like any other secret.
func signInTokens(content string) []string {
	var value any
	if json.Unmarshal([]byte(content), &value) != nil {
		return nil
	}
	tokens := []string{}
	var walk func(key string, value any)
	walk = func(key string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for name, item := range typed {
				walk(name, item)
			}
		case []any:
			for _, item := range typed {
				walk(key, item)
			}
		case string:
			lower := strings.ToLower(key)
			if len(typed) >= 16 && (strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "secret")) {
				tokens = append(tokens, typed)
			}
		}
	}
	walk("", value)
	return tokens
}

// agentSignInSecrets adds the user's stored sign-in to a Codex or Claude Code
// run without a model server. Without one, and without an API key secret,
// the run stops so the task can ask the user to sign in from their terminal.
func (m *Manager) agentSignInSecrets(ctx context.Context, run *domain.DevMachineAgentRun, secrets, redactions map[string]string) error {
	name, ok := signInSecretNames[run.ProviderID]
	if !ok || run.ModelServerID != nil {
		return nil
	}
	for _, key := range apiKeySecrets[run.ProviderID] {
		if secrets[key] != "" {
			return nil
		}
	}
	var encrypted string
	var err error
	if run.RequestedByUserID != nil {
		if encrypted, err = m.store.GetAgentSignIn(ctx, run.WorkspaceID, *run.RequestedByUserID, run.ProviderID); err != nil {
			return err
		}
	}
	if encrypted == "" {
		return &terminalOperationError{code: "sign_in_required", message: agentNames[run.ProviderID] + " needs you to sign in"}
	}
	content, err := cryptoutil.Decrypt(encrypted, m.encryptionKey)
	if err != nil {
		return fmt.Errorf("secret_decryption_failed: %w", err)
	}
	secrets[name], redactions[name] = content, content
	for index, token := range signInTokens(content) {
		key := fmt.Sprintf("KUAYLE_SIGN_IN_SECRET_%d", index)
		secrets[key], redactions[key] = token, token
	}
	return nil
}

// captureSignIn stores the sign-in the task's creator completed in their
// terminal, encrypted, for their later runs of that agent.
func (m *Manager) captureSignIn(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	if op.AgentRunID == nil || op.RequestedByUserID == nil {
		return &terminalOperationError{code: "invalid_operation", message: "sign-in capture has no run or user"}
	}
	run, err := m.store.GetAgentRunInternal(ctx, *op.AgentRunID)
	if err != nil {
		return err
	}
	if run == nil || run.MachineID != machine.ID || signInSecretNames[run.ProviderID] == "" {
		return &terminalOperationError{code: "invalid_operation", message: "the run's agent has no sign-in"}
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	content, err := m.runtime.ReadSignIn(ctx, services, run.ProviderID)
	if err != nil {
		return err
	}
	if !validSignIn(content) {
		return &terminalOperationError{code: "sign_in_not_found", message: "No " + agentNames[run.ProviderID] + " sign-in was found in the machine's terminal"}
	}
	encrypted, err := cryptoutil.Encrypt(strings.TrimSpace(content), m.encryptionKey)
	if err != nil {
		return err
	}
	return m.store.SaveAgentSignIn(ctx, machine.WorkspaceID, *op.RequestedByUserID, run.ProviderID, encrypted)
}

// returnSignIn keeps the sign-in an agent refreshed during its run, so a
// rotated refresh token is not lost with the run's in-memory home.
func (m *Manager) returnSignIn(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, used string) error {
	if run.RequestedByUserID == nil {
		return nil
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	content, err := m.runtime.TakeSignInReturn(ctx, services, run.ID)
	if err != nil || !validSignIn(content) || strings.TrimSpace(content) == strings.TrimSpace(used) {
		return err
	}
	encrypted, err := cryptoutil.Encrypt(strings.TrimSpace(content), m.encryptionKey)
	if err != nil {
		return err
	}
	return m.store.SaveAgentSignIn(ctx, machine.WorkspaceID, *run.RequestedByUserID, run.ProviderID, encrypted)
}
