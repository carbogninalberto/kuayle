package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func (f *devMachineStoreFake) ListAgentModelDefaults(context.Context, uuid.UUID) ([]domain.AgentModelDefault, error) {
	return append([]domain.AgentModelDefault(nil), f.agentModelDefaults...), nil
}

func (f *devMachineStoreFake) GetAgentModelDefault(_ context.Context, ws uuid.UUID, provider string) (*domain.AgentModelDefault, error) {
	for _, item := range f.agentModelDefaults {
		if item.WorkspaceID == ws && item.Provider == provider {
			copy := item
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *devMachineStoreFake) SaveAgentModelDefault(_ context.Context, value *domain.AgentModelDefault) error {
	kept := f.agentModelDefaults[:0]
	for _, item := range f.agentModelDefaults {
		if item.WorkspaceID != value.WorkspaceID || item.Provider != value.Provider {
			kept = append(kept, item)
		}
	}
	f.agentModelDefaults = kept
	if value.Model != "" || value.Thinking != "" {
		value.UpdatedAt = time.Now()
		f.agentModelDefaults = append(f.agentModelDefaults, *value)
	}
	return nil
}

func TestWorkspaceAgentModelDefaultsAreValidatedPerAgent(t *testing.T) {
	ws, user := uuid.New(), uuid.New()
	store := &devMachineStoreFake{}
	svc := newTestDevMachineService(store)
	ctx := context.Background()

	saved, err := svc.SaveAgentModelDefault(ctx, ws, user, "claude-code", dto.SaveAgentModelDefaultRequest{Model: " claude-opus-5-5 ", Thinking: "medium"})
	require.NoError(t, err)
	require.Equal(t, "claude-opus-5-5", saved.Model)
	require.Equal(t, "medium", saved.Thinking)
	require.Equal(t, []string{"off", "low", "medium", "high", "xhigh", "max"}, saved.ThinkingLevels)
	require.Equal(t, &user, store.agentModelDefaults[0].UpdatedBy)

	for _, invalid := range []struct {
		provider string
		request  dto.SaveAgentModelDefaultRequest
	}{
		{"opencode", dto.SaveAgentModelDefaultRequest{Model: "m"}},
		{"claude-code", dto.SaveAgentModelDefaultRequest{Model: `opus" --dangerously`}},
		{"claude-code", dto.SaveAgentModelDefaultRequest{Thinking: "ultra"}},
	} {
		_, err = svc.SaveAgentModelDefault(ctx, ws, user, invalid.provider, invalid.request)
		require.ErrorIs(t, err, ErrInvalidMachineInput, invalid)
	}

	listed, err := svc.ListAgentModelDefaults(ctx, ws)
	require.NoError(t, err)
	require.Len(t, listed, 2, "every agent that takes a model is listed, configured or not")
	require.Equal(t, "codex", listed[0].Provider)
	require.Empty(t, listed[0].Model)
	require.Equal(t, "claude-code", listed[1].Provider)
	require.Equal(t, "claude-opus-5-5", listed[1].Model)

	_, err = svc.SaveAgentModelDefault(ctx, ws, user, "claude-code", dto.SaveAgentModelDefaultRequest{})
	require.NoError(t, err)
	require.Empty(t, store.agentModelDefaults, "an empty default resets the agent to its own defaults")
}

func ownSignInClaudeStore(ws, machine, user uuid.UUID) *devMachineStoreFake {
	policy := testPolicy(ws)
	policy.AllowedProviders = json.RawMessage(`["claude-code","codex"]`)
	return &devMachineStoreFake{
		policy: policy,
		machine: &domain.DevMachine{ID: machine, WorkspaceID: ws, CreatedByUserID: &user,
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		agentProvider: &domain.DevMachineAgentProvider{ProviderID: "claude-code", Enabled: true,
			SupportedModes: json.RawMessage(`["autonomous"]`), RequiredSecrets: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		agentModelDefaults: []domain.AgentModelDefault{{WorkspaceID: ws, Provider: "claude-code", Model: "claude-opus-5-5", Thinking: "medium"}},
	}
}

func ownSignInClaudeService(store *devMachineStoreFake) *DevMachineService {
	return NewDevMachineService(store, agent.NewRegistry(agent.NewClaudeCodeProvider(""), agent.NewCodexProvider("")), true,
		"machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})
}

func runArgv(t *testing.T, run *domain.DevMachineAgentRun) []string {
	t.Helper()
	var argv []string
	require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
	return argv
}

func requireModelArg(t *testing.T, argv []string, model string) {
	t.Helper()
	for i := range argv[:len(argv)-1] {
		if argv[i] == "--model" {
			require.Equal(t, model, argv[i+1])
			return
		}
	}
	t.Fatalf("no --model in %v", argv)
}

func TestOwnSignInRunsFillWhatTheyDidNotChooseFromTheWorkspaceDefault(t *testing.T) {
	ws, machine, user := uuid.New(), uuid.New(), uuid.New()
	store := ownSignInClaudeStore(ws, machine, user)
	svc := ownSignInClaudeService(store)
	request := func(model, thinking string) dto.CreateAgentRunRequest {
		return dto.CreateAgentRunRequest{UseRootWorkspace: true, PushBranch: dmBoolPtr(false), Provider: "claude-code",
			Mode: "autonomous", Prompt: "Implement it", MaxRuntimeSeconds: 30, Model: model, Thinking: thinking}
	}

	run, err := svc.createAgentRun(context.Background(), ws, machine, user, request("", ""), uuid.New())
	require.NoError(t, err)
	requireModelArg(t, runArgv(t, run), "claude-opus-5-5")
	require.JSONEq(t, `{"CLAUDE_CODE_EFFORT_LEVEL":"medium"}`, string(run.CommandEnvironment))
	require.Equal(t, "claude-opus-5-5", run.Model)
	require.Equal(t, "medium", run.Thinking)
	require.Nil(t, run.ModelServerID, "no model server: Claude Code keeps its own sign-in")

	run, err = svc.createAgentRun(context.Background(), ws, machine, user, request("claude-sonnet-5-5", "off"), uuid.New())
	require.NoError(t, err)
	requireModelArg(t, runArgv(t, run), "claude-sonnet-5-5")
	require.JSONEq(t, `{}`, string(run.CommandEnvironment), "an explicit choice wins over the default")
	require.Equal(t, "off", run.Thinking)

	run, err = svc.createAgentRun(context.Background(), ws, machine, user, request("claude-sonnet-5-5", ""), uuid.New())
	require.NoError(t, err)
	requireModelArg(t, runArgv(t, run), "claude-sonnet-5-5")
	require.Equal(t, "medium", run.Thinking, "each field the run leaves empty comes from the default")

	store.agentModelDefaults = nil
	run, err = svc.createAgentRun(context.Background(), ws, machine, user, request("", ""), uuid.New())
	require.NoError(t, err)
	require.NotContains(t, runArgv(t, run), "--model", "without a default the agent keeps its own defaults")
	require.JSONEq(t, `{}`, string(run.CommandEnvironment))
}

func TestAModelServerKeepsItsOwnDefaultsOverTheSignInDefault(t *testing.T) {
	ws, machine, user := uuid.New(), uuid.New(), uuid.New()
	store := ownSignInClaudeStore(ws, machine, user)
	store.modelServers = []domain.AgentModelServer{deepSeekModelServer(ws)}
	run, err := ownSignInClaudeService(store).createAgentRun(context.Background(), ws, machine, user, dto.CreateAgentRunRequest{
		UseRootWorkspace: true, PushBranch: dmBoolPtr(false), Provider: "claude-code", Mode: "autonomous", Prompt: "Implement it", MaxRuntimeSeconds: 30,
	}, uuid.New())
	require.NoError(t, err)
	require.Equal(t, "deepseek-flash", run.Model)
	require.Empty(t, run.Thinking, "the server's thinking level applies through its environment")
	require.Equal(t, store.modelServers[0].ID, *run.ModelServerID)
}

func TestAModelWithoutAModelServerUsesTheAgentsOwnSignIn(t *testing.T) {
	ws := uuid.New()
	store := &devMachineStoreFake{}
	svc := newTestDevMachineService(store)
	ctx := context.Background()
	require.NoError(t, svc.validateAgentModel(ctx, ws, "codex", nil, "gpt-5.5", "medium"))
	require.NoError(t, svc.validateAgentModel(ctx, ws, "claude-code", nil, "claude-opus-5-5", "xhigh"))

	openAIOnly := deepSeekModelServer(ws)
	openAIOnly.AnthropicBaseURL = nil
	store.modelServers = []domain.AgentModelServer{openAIOnly}
	require.NoError(t, svc.validateAgentModel(ctx, ws, "claude-code", nil, "claude-opus-5-5", "medium"),
		"a default server that cannot serve Claude Code leaves it on its own sign-in")
	require.ErrorIs(t, svc.validateAgentModel(ctx, ws, "claude-code", &openAIOnly.ID, "claude-opus-5-5", ""), ErrInvalidMachineInput,
		"an explicitly chosen server must speak the agent's protocol")
	require.ErrorIs(t, svc.validateAgentModel(ctx, ws, "codex", nil, "gpt-5.5", ""), ErrInvalidMachineInput,
		"a default server that serves the agent still limits its models")
	missing := uuid.New()
	require.ErrorIs(t, svc.validateAgentModel(ctx, ws, "codex", &missing, "", ""), ErrInvalidMachineInput)
	require.ErrorIs(t, svc.validateAgentModel(ctx, ws, "claude-code", nil, `bad model`, ""), ErrInvalidMachineInput)
}

// Each attempt of an open task creates a new run, so a task without its own
// model picks up the current default on retries and follow-ups; a run that
// already exists is returned unchanged.
func TestTaskAttemptsResolveTheDefaultWhenTheirRunIsCreated(t *testing.T) {
	ws, machine, user, checkout := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	inner := ownSignInClaudeStore(ws, machine, user)
	inner.checkouts = []domain.DevMachineCheckout{{ID: checkout, WorkspaceID: ws, MachineID: machine, Status: "ready",
		WorkspacePath: "/workspace/tasks/check", RepositoryFullName: "example/test", BaseBranch: "main", WorkingBranch: "task/check"}}
	store := &taskPolicyStore{devMachineStoreFake: inner}
	machines := ownSignInClaudeService(inner)
	machines.store = store
	runtime := &devMachineTaskRuntime{machines: machines}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, MachineID: &machine, CheckoutID: checkout, RunID: uuid.New(),
		State: "preparing", Provider: "claude-code", Prompt: "Implement", Kind: domain.AgentTaskKindImplement, Generation: 1}

	run, err := runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	requireModelArg(t, runArgv(t, run), "claude-opus-5-5")
	require.JSONEq(t, `{"CLAUDE_CODE_EFFORT_LEVEL":"medium"}`, string(run.CommandEnvironment))

	explicit := task
	explicit.ID, explicit.RunID, explicit.Model, explicit.Thinking = uuid.New(), uuid.New(), "claude-sonnet-5-5", "high"
	run, err = runtime.Attempt(context.Background(), explicit)
	require.NoError(t, err)
	requireModelArg(t, runArgv(t, run), "claude-sonnet-5-5")
	require.JSONEq(t, `{"CLAUDE_CODE_EFFORT_LEVEL":"high"}`, string(run.CommandEnvironment))

	started := &domain.DevMachineAgentRun{ID: task.RunID, Model: "", Thinking: ""}
	store.run = started
	inner.agentModelDefaults[0].Model = "claude-fable-5"
	run, err = runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Same(t, started, run, "a run in progress keeps the model it started with")
}
