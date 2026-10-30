package dto_test

import (
	"encoding/json"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestTerminalSessionLaunchResponseOmitsMissingSession(t *testing.T) {
	encoded, err := json.Marshal(dto.TerminalSessionLaunchResponse{Status: "pending", RetryAfterSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["session"]; exists {
		t.Fatalf("pending terminal response contains a fabricated session: %s", encoded)
	}
}

func TestDevMachineDTOsAcceptBuiltInProviders(t *testing.T) {
	providers := []string{"claude-code", "opencode", "codex", "qwen-code", "crush", "kimi", "goose", "custom"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			require.NoError(t, validate.Struct(dto.CreateDevMachineRequest{
				Size: "small",
				Agents: []dto.DevMachineAgentInput{{
					Provider: provider,
					Mode:     "autonomous",
				}},
			}))
			require.NoError(t, validate.Struct(dto.CreateAgentRunRequest{
				Provider: provider,
				Mode:     "autonomous",
				Prompt:   "fix the issue",
			}))
		})
	}
}

func TestDevMachineDTOsAcceptProfiles(t *testing.T) {
	for _, profile := range []string{"", "standard", "headless"} {
		require.NoError(t, validate.Struct(dto.CreateDevMachineRequest{Size: "small", Profile: profile}))
	}
	require.Error(t, validate.Struct(dto.CreateDevMachineRequest{Size: "small", Profile: "desktop"}))
}

func TestDevMachinePolicyRejectsUnknownProviders(t *testing.T) {
	for _, provider := range []string{"claude-code", "opencode", "codex", "qwen-code", "crush", "kimi", "goose"} {
		require.NoError(t, validate.Struct(dto.DevMachinePolicyRequest{
			MaxDiskGB:        20,
			AllowedProviders: []string{provider},
		}))
	}
	require.Error(t, validate.Struct(dto.DevMachinePolicyRequest{
		MaxDiskGB:        20,
		AllowedProviders: []string{"unknown"},
	}))
}
