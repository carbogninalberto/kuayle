package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/dto"
)

func modelChoiceOptions() *AgentTaskOptions {
	return &AgentTaskOptions{
		Providers:       []dto.AgentProviderResponse{{ID: "claude-code"}, {ID: "codex"}},
		DefaultProvider: "codex",
		ModelServers: []dto.ModelServerResponse{
			{IsDefault: true, DefaultModel: "gpt-5", Thinking: "high", Harnesses: []string{"codex"}},
		},
	}
}

func TestChatModelFallsBackToTheDefaultServerWhenNothingIsChosen(t *testing.T) {
	model, thinking := chatModel(modelChoiceOptions(), "codex", "")
	require.Equal(t, "gpt-5", model)
	require.Equal(t, "high", thinking)
}

func TestChatModelKeepsTheServerDefaultOnlyForHarnessesItSpeaks(t *testing.T) {
	model, thinking := chatModel(modelChoiceOptions(), "claude-code", "")
	require.Empty(t, model)
	require.Empty(t, thinking)
}

func TestChatModelPrefersTheExplicitModel(t *testing.T) {
	model, thinking := chatModel(modelChoiceOptions(), "codex", "gpt-5-mini")
	require.Equal(t, "gpt-5-mini", model)
	require.Equal(t, "high", thinking)
	model, _ = chatModel(modelChoiceOptions(), "claude-code", "opus")
	require.Equal(t, "opus", model)
}

func TestModelChoiceApprovalOverridesTheAssistantsProposal(t *testing.T) {
	choice := ModelChoice{Harness: "claude-code", Model: " opus "}
	harness, model := choice.over("codex", "gpt-5")
	require.Equal(t, "claude-code", harness)
	require.Equal(t, "opus", model)
	// A new harness without a model must not inherit the old harness's model.
	harness, model = ModelChoice{Harness: "claude-code"}.over("codex", "gpt-5")
	require.Equal(t, "claude-code", harness)
	require.Empty(t, model)
	harness, model = ModelChoice{}.over("codex", "gpt-5")
	require.Equal(t, "codex", harness)
	require.Equal(t, "gpt-5", model)
}

func TestValidModelNameRejectsControlCharactersAndLongNames(t *testing.T) {
	require.True(t, validModelName("claude-opus-4.5"))
	require.True(t, validModelName(""))
	require.False(t, validModelName("a\nb"))
	require.False(t, validModelName(string(make([]byte, 201))))
}
