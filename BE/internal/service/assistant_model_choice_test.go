package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
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
	model, thinking, server := chatModel(modelChoiceOptions(), "codex", "")
	require.Equal(t, "gpt-5", model)
	require.Equal(t, "high", thinking)
	require.Nil(t, server)
}

func TestChatModelKeepsTheServerDefaultOnlyForHarnessesItSpeaks(t *testing.T) {
	model, thinking, server := chatModel(modelChoiceOptions(), "claude-code", "")
	require.Empty(t, model)
	require.Empty(t, thinking)
	require.Nil(t, server)
}

func TestChatModelPrefersTheExplicitModel(t *testing.T) {
	model, thinking, server := chatModel(modelChoiceOptions(), "codex", "gpt-5-mini")
	require.Equal(t, "gpt-5-mini", model)
	require.Equal(t, "high", thinking)
	require.Nil(t, server)
	model, _, _ = chatModel(modelChoiceOptions(), "claude-code", "opus")
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

func twoServerOptions() (*AgentTaskOptions, uuid.UUID) {
	second := uuid.New()
	return &AgentTaskOptions{
		Providers: []dto.AgentProviderResponse{{ID: "codex"}},
		ModelServers: []dto.ModelServerResponse{
			{ID: uuid.New(), IsDefault: true, DefaultModel: "gpt-5", Models: []string{"gpt-5"}, Thinking: "high", Harnesses: []string{"codex"}},
			{ID: second, DefaultModel: "o4", Models: []string{"o4", "o4-mini"}, Thinking: "low", Harnesses: []string{"codex"}},
		},
	}, second
}

func TestChatModelFollowsTheServerThatOffersTheChosenModel(t *testing.T) {
	options, second := twoServerOptions()
	model, thinking, server := chatModel(options, "codex", "o4-mini")
	require.Equal(t, "o4-mini", model)
	require.Equal(t, "low", thinking, "the thinking level belongs to the server that offers the model")
	require.NotNil(t, server)
	require.Equal(t, second, *server)
	// A model the default server offers keeps the default server implicit.
	_, thinking, server = chatModel(options, "codex", "gpt-5")
	require.Equal(t, "high", thinking)
	require.Nil(t, server)
}

func TestChatModelNoServerOffersTheModelLeavesValidationToReject(t *testing.T) {
	options, _ := twoServerOptions()
	model, thinking, server := chatModel(options, "codex", "unknown")
	require.Equal(t, "unknown", model)
	require.Empty(t, thinking)
	require.Nil(t, server)
}

type modelServerStore struct {
	repository.DevMachineStore
	server *domain.AgentModelServer
}

func (s modelServerStore) GetModelServer(context.Context, uuid.UUID, *uuid.UUID) (*domain.AgentModelServer, error) {
	return s.server, nil
}

func TestValidateAgentModelRejectsAModelTheServerOrHarnessCannotServe(t *testing.T) {
	url := "https://models.example"
	models, _ := json.Marshal([]string{"gpt-5"})
	service := &DevMachineService{store: modelServerStore{server: &domain.AgentModelServer{Name: "main", OpenAIBaseURL: &url, Models: models}}}
	ctx := context.Background()
	require.NoError(t, service.validateAgentModel(ctx, uuid.New(), "codex", nil, "gpt-5", ""))
	require.ErrorIs(t, service.validateAgentModel(ctx, uuid.New(), "codex", nil, "other", ""), ErrInvalidMachineInput)
	require.ErrorIs(t, service.validateAgentModel(ctx, uuid.New(), "claude-code", nil, "gpt-5", ""), ErrInvalidMachineInput)
	require.ErrorIs(t, service.validateAgentModel(ctx, uuid.New(), "opencode", nil, "gpt-5", ""), ErrInvalidMachineInput)
}

func TestReviewOnAnotherHarnessDropsTheProposedModel(t *testing.T) {
	harness, model := ModelChoice{Harness: "claude-code"}.over("codex", "gpt-5")
	require.Equal(t, "claude-code", harness)
	require.Empty(t, model)
}
