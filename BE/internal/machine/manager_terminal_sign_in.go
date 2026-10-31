package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	log "github.com/sirupsen/logrus"
)

// Files of the owner's sign-ins in the developer container, under
// ~/.kuayle/agent-sign-ins in its in-memory home. kuayle-agent-sign-in.sh reads
// them when an interactive shell starts.
const (
	terminalSignInManaged     = "managed"
	terminalClaudeToken       = "claude-code.token"
	terminalClaudeCredentials = "claude-code.credentials.json"
	terminalCodexAuth         = "codex.auth.json"
)

// terminalSignIns are the sign-ins the machine owner's own terminal and IDE
// shells use: the owner's sign-in for each agent the workspace policy allows,
// as a run would get it (a saved Claude Code token first, then the captured
// credentials file). An agent the terminal already reaches through the
// workspace's model server or an API key secret is left out. "managed" lists
// the agents Kuayle signs in, so the shell can hint how to sign in to one that
// has no sign-in yet. Only the machine's creator in the machine's workspace is
// read: a machine never receives another user's sign-in.
func (m *Manager) terminalSignIns(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) (map[string]string, error) {
	files := map[string]string{}
	if machine.CreatedByUserID == nil {
		return files, nil
	}
	policy, err := m.store.GetPolicy(ctx, machine.WorkspaceID)
	if err != nil || policy == nil {
		return files, err
	}
	var allowed []string
	_ = json.Unmarshal(policy.AllowedProviders, &allowed)
	configured, err := m.terminalAgentConfiguration(ctx, machine, services)
	if err != nil {
		return nil, err
	}
	owner := *machine.CreatedByUserID
	managed := []string{}
	for _, provider := range []string{"claude-code", "codex"} {
		if !containsString(allowed, provider) || configured[provider] {
			continue
		}
		managed = append(managed, provider)
		if provider == "claude-code" {
			token, err := m.decryptSignIn(m.store.GetAgentSignInToken(ctx, machine.WorkspaceID, owner, provider))
			if err != nil {
				return nil, err
			}
			if token != "" {
				files[terminalClaudeToken] = token
				continue
			}
		}
		content, err := m.decryptSignIn(m.store.GetAgentSignIn(ctx, machine.WorkspaceID, owner, provider))
		if err != nil {
			return nil, err
		}
		if !validSignIn(content) {
			continue
		}
		if provider == "claude-code" {
			files[terminalClaudeCredentials] = strings.TrimSpace(content)
		} else {
			files[terminalCodexAuth] = strings.TrimSpace(content)
		}
	}
	if len(managed) > 0 {
		files[terminalSignInManaged] = strings.Join(managed, "\n") + "\n"
	}
	return files, nil
}

// terminalAgentConfiguration reports the agents the developer container
// already reaches without a sign-in: through the workspace's default model
// server (see serviceSecrets) or an API key secret of the ide or terminal.
func (m *Manager) terminalAgentConfiguration(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) (map[string]bool, error) {
	configured := map[string]bool{}
	server, err := m.store.GetModelServer(ctx, machine.WorkspaceID, nil)
	if err != nil {
		return nil, err
	}
	if server != nil {
		configured["claude-code"] = server.AnthropicBaseURL != nil
		configured["codex"] = server.OpenAIBaseURL != nil
	}
	seen := map[string]bool{}
	for _, service := range services {
		if !isDeveloperService(service.ServiceType) || seen[service.ServiceType] {
			continue
		}
		seen[service.ServiceType] = true
		envVars, err := m.store.ListEnvVarsInternal(ctx, machine.ID, nil, service.ServiceType)
		if err != nil {
			return nil, err
		}
		for _, envVar := range envVars {
			for provider, names := range apiKeySecrets {
				if containsString(names, envVar.Name) {
					configured[provider] = true
				}
			}
		}
	}
	return configured, nil
}

func (m *Manager) decryptSignIn(encrypted string, err error) (string, error) {
	if err != nil || encrypted == "" {
		return "", err
	}
	value, err := cryptoutil.Decrypt(encrypted, m.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("secret_decryption_failed: %w", err)
	}
	return value, nil
}

// syncTerminalSignIns rewrites the owner's sign-ins in the machine's
// developer container, replacing what an earlier sync wrote.
func (m *Manager) syncTerminalSignIns(ctx context.Context, machine *domain.DevMachine) error {
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	files, err := m.terminalSignIns(ctx, machine, services)
	if err != nil {
		return err
	}
	return m.runtime.SyncAgentSignIns(ctx, services, files)
}

// syncTerminalSignInsAfterStart gives a freshly started developer container
// the owner's sign-ins. A failure does not fail the start: the terminal then
// works without them until the next sign-in change or start.
func (m *Manager) syncTerminalSignInsAfterStart(ctx context.Context, machine *domain.DevMachine) {
	if err := m.syncTerminalSignIns(ctx, machine); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("terminal sign-in sync failed")
	}
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
