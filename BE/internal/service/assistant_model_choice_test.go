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
	model, thinking, server := chatModel(modelChoiceOptions(), "codex", "", nil)
	require.Equal(t, "gpt-5", model)
	require.Equal(t, "high", thinking)
	require.Nil(t, server)
}

func TestChatModelKeepsTheServerDefaultOnlyForHarnessesItSpeaks(t *testing.T) {
	model, thinking, server := chatModel(modelChoiceOptions(), "claude-code", "", nil)
	require.Empty(t, model)
	require.Empty(t, thinking)
	require.Nil(t, server)
}

func TestChatModelPrefersTheExplicitModel(t *testing.T) {
	model, thinking, server := chatModel(modelChoiceOptions(), "codex", "gpt-5-mini", nil)
	require.Equal(t, "gpt-5-mini", model)
	require.Equal(t, "high", thinking)
	require.Nil(t, server)
	model, _, _ = chatModel(modelChoiceOptions(), "claude-code", "opus", nil)
	require.Equal(t, "opus", model)
}

func TestModelChoiceApprovalOverridesTheAssistantsProposal(t *testing.T) {
	choice := ModelChoice{Harness: "claude-code", Model: " opus "}
	harness, model, _ := choice.over("codex", "gpt-5")
	require.Equal(t, "claude-code", harness)
	require.Equal(t, "opus", model)
	// A new harness without a model must not inherit the old harness's model.
	harness, model, _ = ModelChoice{Harness: "claude-code"}.over("codex", "gpt-5")
	require.Equal(t, "claude-code", harness)
	require.Empty(t, model)
	harness, model, _ = ModelChoice{}.over("codex", "gpt-5")
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
	model, thinking, server := chatModel(options, "codex", "o4-mini", nil)
	require.Equal(t, "o4-mini", model)
	require.Equal(t, "low", thinking, "the thinking level belongs to the server that offers the model")
	require.NotNil(t, server)
	require.Equal(t, second, *server)
	// A model the default server offers keeps the default server implicit.
	_, thinking, server = chatModel(options, "codex", "gpt-5", nil)
	require.Equal(t, "high", thinking)
	require.Nil(t, server)
}

func TestChatModelNoServerOffersTheModelLeavesValidationToReject(t *testing.T) {
	options, _ := twoServerOptions()
	model, thinking, server := chatModel(options, "codex", "unknown", nil)
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

func TestValidateAgentModelRejectsAModelTheChosenServerOrHarnessCannotServe(t *testing.T) {
	url := "https://models.example"
	models, _ := json.Marshal([]string{"gpt-5"})
	service := &DevMachineService{store: modelServerStore{server: &domain.AgentModelServer{Name: "main", OpenAIBaseURL: &url, Models: models}}}
	ctx := context.Background()
	require.NoError(t, service.validateAgentModel(ctx, uuid.New(), "codex", nil, "gpt-5", ""))
	require.ErrorIs(t, service.validateAgentModel(ctx, uuid.New(), "codex", nil, "other", ""), ErrInvalidMachineInput)
	// The default server does not speak Claude Code's protocol, so Claude Code
	// runs on its own sign-in, which takes any model; naming that server does not.
	require.NoError(t, service.validateAgentModel(ctx, uuid.New(), "claude-code", nil, "opus", ""))
	require.ErrorIs(t, service.validateAgentModel(ctx, uuid.New(), "claude-code", &uuid.UUID{}, "opus", ""), ErrInvalidMachineInput)
	require.ErrorIs(t, service.validateAgentModel(ctx, uuid.New(), "opencode", nil, "gpt-5", ""), ErrInvalidMachineInput)
}

func TestReviewOnAnotherHarnessDropsTheProposedModel(t *testing.T) {
	harness, model, server := ModelChoice{Harness: "claude-code"}.over("codex", "gpt-5")
	require.Equal(t, "claude-code", harness)
	require.Empty(t, model)
	require.Nil(t, server)
}

func TestModelChoiceKeepsTheServerOnlyWithThePickedModel(t *testing.T) {
	chosen := uuid.New()
	_, model, server := ModelChoice{Model: "o4", ModelServerID: &chosen}.over("codex", "gpt-5")
	require.Equal(t, "o4", model)
	require.Equal(t, &chosen, server)
	// A server without a model is no choice; the proposal stays on its own server.
	_, model, server = ModelChoice{ModelServerID: &chosen}.over("codex", "gpt-5")
	require.Equal(t, "gpt-5", model)
	require.Nil(t, server)
}

// Two servers can offer the same model name; the approver's server is the one
// that runs it, with its own thinking level.
func TestChatModelRunsTheChosenServerEvenWhenTheDefaultOffersTheModel(t *testing.T) {
	options, second := twoServerOptions()
	options.ModelServers[1].Models = append(options.ModelServers[1].Models, "gpt-5")
	model, thinking, server := chatModel(options, "codex", "gpt-5", &second)
	require.Equal(t, "gpt-5", model)
	require.Equal(t, "low", thinking)
	require.Equal(t, &second, server)
	defaultID := options.ModelServers[0].ID
	_, thinking, server = chatModel(options, "codex", "gpt-5", &defaultID)
	require.Equal(t, "high", thinking)
	require.Nil(t, server, "the default server stays implicit")
	unknown := uuid.New()
	_, thinking, server = chatModel(options, "codex", "gpt-5", &unknown)
	require.Empty(t, thinking)
	require.Equal(t, &unknown, server, "an unknown server is left for validation to reject")
}

func TestRetryModelKeepsOrSwitchesTheTasksServer(t *testing.T) {
	options, second := twoServerOptions()
	options.ModelServers[1].Models = append(options.ModelServers[1].Models, "gpt-5")
	defaultID := options.ModelServers[0].ID
	task := &domain.AgentTask{Provider: "codex", Model: "gpt-5", Thinking: "max"}
	servers := options.ModelServers

	model, thinking, server := retryModel(servers, task, "", nil)
	require.Equal(t, []any{"gpt-5", "max", (*uuid.UUID)(nil)}, []any{model, thinking, server}, "no model keeps the task's")
	model, thinking, server = retryModel(servers, task, "gpt-5", &defaultID)
	require.Equal(t, []any{"gpt-5", "max", (*uuid.UUID)(nil)}, []any{model, thinking, server}, "the same model on the same server keeps the task's thinking")
	model, thinking, server = retryModel(servers, task, "gpt-5", &second)
	require.Equal(t, []any{"gpt-5", "low", &second}, []any{model, thinking, server}, "the same model on another server switches server")
	model, thinking, server = retryModel(servers, task, "o4", nil)
	require.Equal(t, []any{"o4", "low", &second}, []any{model, thinking, server}, "a model only another server offers moves there")
	pinned := &domain.AgentTask{Provider: "codex", Model: "o4", Thinking: "low", ModelServerID: &second}
	model, thinking, server = retryModel(servers, pinned, "gpt-5", nil)
	require.Equal(t, []any{"gpt-5", "low", &second}, []any{model, thinking, server}, "the task's own server is preferred when it offers the model")
	model, thinking, server = retryModel(servers, pinned, "gpt-5", &defaultID)
	require.Equal(t, []any{"gpt-5", "high", &defaultID}, []any{model, thinking, server}, "a pinned task records the default explicitly")
}
