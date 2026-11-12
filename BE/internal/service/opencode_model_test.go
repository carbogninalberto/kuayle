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
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

const (
	openCodeModel       = "opencode-go/deepseek-v4.1-flash"
	openCodeModelFormat = "OpenCode takes its model as provider/model, for example opencode-go/deepseek-v4.1-flash"
	openCodeNoServer    = "OpenCode does not run on a model server"
)

// One rule says which coding agents take a model and an effort, and another
// which of them run on a model server: OpenCode takes the first two only.
func TestValidateAgentModelSeparatesTakingAModelFromRunningOnAModelServer(t *testing.T) {
	ws := uuid.New()
	server := deepSeekModelServer(ws)
	store := &devMachineStoreFake{modelServers: []domain.AgentModelServer{server}}
	svc := NewDevMachineService(store, agent.NewRegistry(agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""), agent.NewOpenCodeProvider(""), agent.NewQwenCodeProvider("")),
		true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})
	missing := uuid.New()

	for _, tc := range []struct {
		name, provider  string
		server          *uuid.UUID
		model, thinking string
		reason          string
	}{
		{name: "opencode model and effort", provider: "opencode", model: openCodeModel, thinking: "high"},
		{name: "opencode model only", provider: "opencode", model: openCodeModel},
		{name: "opencode effort only", provider: "opencode", thinking: "max"},
		{name: "opencode effort off", provider: "opencode", model: openCodeModel, thinking: "off"},
		{name: "opencode with nothing chosen", provider: "opencode"},
		{name: "opencode model with a nested name", provider: "opencode", model: "openrouter/anthropic/claude-sonnet-5"},
		{name: "opencode model that the default server does not list", provider: "opencode", model: "deepseek/deepseek-v4-pro"},
		{name: "opencode model without a provider", provider: "opencode", model: "deepseek-v4.1-flash", reason: openCodeModelFormat},
		{name: "opencode model with an empty provider", provider: "opencode", model: "/deepseek-v4.1-flash", reason: "the model name is not valid"},
		{name: "opencode model with an empty name", provider: "opencode", model: "opencode-go/", reason: openCodeModelFormat},
		{name: "opencode model with a space", provider: "opencode", model: "opencode-go/deepseek v4", reason: "the model name is not valid"},
		{name: "opencode model read as a flag", provider: "opencode", model: "--auto/x", reason: "the model name is not valid"},
		{name: "opencode unknown effort", provider: "opencode", model: openCodeModel, thinking: "minimal", reason: "the effort is one of off, low, medium, high, xhigh, max"},
		{name: "opencode on a model server", provider: "opencode", server: &server.ID, model: openCodeModel, reason: openCodeNoServer},
		{name: "opencode on a model server without a model", provider: "opencode", server: &server.ID, reason: openCodeNoServer},
		{name: "opencode on a missing model server", provider: "opencode", server: &missing, reason: openCodeNoServer},
		{name: "codex on its model server", provider: "codex", server: &server.ID, model: "deepseek-flash", thinking: "high"},
		{name: "codex model the server does not offer", provider: "codex", model: "gpt-5.5", reason: `the model server DeepSeek does not offer the model "gpt-5.5"`},
		{name: "claude code model without a provider part", provider: "claude-code", server: &server.ID, model: "deepseek-v4-pro"},
		{name: "qwen code with nothing chosen", provider: "qwen-code"},
		{name: "qwen code model", provider: "qwen-code", model: "qwen/qwen3-coder", reason: "Qwen Code does not accept a model or effort"},
		{name: "qwen code effort", provider: "qwen-code", thinking: "high", reason: "Qwen Code does not accept a model or effort"},
		{name: "qwen code on a model server", provider: "qwen-code", server: &server.ID, reason: "Qwen Code does not run on a model server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateAgentModel(context.Background(), ws, tc.provider, tc.server, tc.model, tc.thinking)
			if tc.reason == "" {
				require.NoError(t, err)
				return
			}
			var refused *AgentModelError
			require.ErrorAs(t, err, &refused)
			require.Equal(t, tc.reason, refused.Reason)
			require.ErrorIs(t, err, ErrInvalidMachineInput)
		})
	}
}

func openCodeRunService(ws, machine, user uuid.UUID) (*DevMachineService, *devMachineStoreFake) {
	// A default model server and sign-in defaults of the other agents: none
	// of them may reach an OpenCode run.
	store := ownSignInClaudeStore(ws, machine, user)
	store.policy.AllowedProviders = json.RawMessage(`["opencode","claude-code","codex"]`)
	store.agentProvider.ProviderID = "opencode"
	store.modelServers = []domain.AgentModelServer{deepSeekModelServer(ws)}
	store.agentModelDefaults = append(store.agentModelDefaults, domain.AgentModelDefault{WorkspaceID: ws, Provider: "codex", Model: "gpt-5.5", Thinking: "xhigh"})
	return NewDevMachineService(store, agent.NewRegistry(agent.NewClaudeCodeProvider(""), agent.NewCodexProvider(""), agent.NewOpenCodeProvider("")), true,
		"machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{}), store
}

// The model and effort of an OpenCode task reach the command it runs, and the
// workspace's model server and the other agents' defaults never do.
func TestOpenCodeRunCarriesItsModelAndEffortAndNoModelServer(t *testing.T) {
	ws, machine, user := uuid.New(), uuid.New(), uuid.New()
	svc, store := openCodeRunService(ws, machine, user)
	request := func(model, thinking string) dto.CreateAgentRunRequest {
		return dto.CreateAgentRunRequest{UseRootWorkspace: true, PushBranch: dmBoolPtr(false), Provider: "opencode",
			Mode: "autonomous", Prompt: "Implement it", MaxRuntimeSeconds: 30, Model: model, Thinking: thinking}
	}

	run, err := svc.createAgentRun(context.Background(), ws, machine, user, request(openCodeModel, "high"), uuid.New())
	require.NoError(t, err)
	argv := runArgv(t, run)
	require.Equal(t, []string{"opencode", "run", "--format", "json", "--auto", "--print-logs", "--log-level", "WARN", "--model", openCodeModel, "--variant", "high", "--"}, argv[:len(argv)-1])
	require.Contains(t, argv[len(argv)-1], "Implement it")
	require.Nil(t, run.ModelServerID, "OpenCode never runs on the workspace's model server")
	require.JSONEq(t, `{}`, string(run.CommandEnvironment))
	require.Equal(t, openCodeModel, run.Model)
	require.Equal(t, "high", run.Thinking)

	// Nothing chosen: OpenCode keeps the model it is configured with.
	run, err = svc.createAgentRun(context.Background(), ws, machine, user, request("", ""), uuid.New())
	require.NoError(t, err)
	argv = runArgv(t, run)
	require.Equal(t, []string{"opencode", "run", "--format", "json", "--auto", "--print-logs", "--log-level", "WARN", "--"}, argv[:len(argv)-1])
	require.Nil(t, run.ModelServerID)
	require.Empty(t, run.Model, "no default of the model server or of another agent is inherited")
	require.Empty(t, run.Thinking)

	// Naming a model server is refused with the reason, not dropped silently.
	withServer := request(openCodeModel, "")
	withServer.ModelServerID = &store.modelServers[0].ID
	_, err = svc.createAgentRun(context.Background(), ws, machine, user, withServer, uuid.New())
	var refused *AgentModelError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, openCodeNoServer, refused.Reason)
}

func openCodeChatOptions() (*AgentTaskOptions, uuid.UUID, uuid.UUID) {
	fallback, other := uuid.New(), uuid.New()
	return &AgentTaskOptions{
		Providers:       []dto.AgentProviderResponse{{ID: "claude-code"}, {ID: "codex"}, {ID: "opencode"}},
		DefaultProvider: "codex",
		ModelServers: []dto.ModelServerResponse{
			{ID: fallback, IsDefault: true, DefaultModel: "gpt-5", Thinking: "high", Harnesses: []string{"codex", "claude-code"}},
			{ID: other, DefaultModel: "o4", Thinking: "low", Harnesses: []string{"codex"}},
		},
	}, fallback, other
}

// A model the user named for OpenCode in chat is passed on as it is: no model
// server's default model, thinking level or ID is attached to it.
func TestChatModelKeepsAnOpenCodeModelOffTheModelServers(t *testing.T) {
	options, fallback, other := openCodeChatOptions()

	model, thinking, server := chatModel(options, "opencode", "", nil)
	require.Empty(t, model, "OpenCode does not inherit the default server's model")
	require.Empty(t, thinking)
	require.Nil(t, server)

	model, thinking, server = chatModel(options, "opencode", openCodeModel, nil)
	require.Equal(t, openCodeModel, model)
	require.Empty(t, thinking)
	require.Nil(t, server)

	request, err := delegationRequest(options, uuid.New(), "Implement it", "opencode", openCodeModel, nil, "")
	require.NoError(t, err)
	require.Equal(t, "opencode", request.Provider)
	require.Equal(t, openCodeModel, request.Model)
	require.Empty(t, request.Thinking)
	require.Nil(t, request.ModelServerID)

	// A server picked for a harness it does not speak is kept for validation
	// to refuse; its thinking level is never borrowed, default server or not.
	for _, chosen := range []uuid.UUID{fallback, other} {
		model, thinking, server = chatModel(options, "opencode", openCodeModel, &chosen)
		require.Equal(t, openCodeModel, model)
		require.Empty(t, thinking)
		require.NotNil(t, server)
		require.Equal(t, chosen, *server)
	}
	model, thinking, server = chatModel(options, "claude-code", "opus", &other)
	require.Equal(t, "opus", model)
	require.Empty(t, thinking)
	require.Equal(t, other, *server)
}

// A follow-up keeps an OpenCode task's model and effort, and a new model is
// never moved onto a model server.
func TestRetryModelKeepsAnOpenCodeTaskOffTheModelServers(t *testing.T) {
	options, fallback, _ := openCodeChatOptions()
	task := &domain.AgentTask{Provider: "opencode", Model: openCodeModel, Thinking: "high"}

	model, thinking, server := retryModel(options.ModelServers, task, "", nil)
	require.Equal(t, openCodeModel, model)
	require.Equal(t, "high", thinking)
	require.Nil(t, server)

	model, thinking, server = retryModel(options.ModelServers, task, "opencode-go/glm-5", nil)
	require.Equal(t, "opencode-go/glm-5", model)
	require.Empty(t, thinking, "efforts are per model, so a new model starts on its own default")
	require.Nil(t, server)

	model, thinking, server = retryModel(options.ModelServers, task, "opencode-go/glm-5", &fallback)
	require.Equal(t, "opencode-go/glm-5", model)
	require.Empty(t, thinking)
	require.Equal(t, fallback, *server, "kept for validation to refuse")
}

// A harness and provider/model name the user named in chat reach the task as
// they were written: the delegate_issue tool neither refuses nor rewrites
// them, and no model server is attached on approval.
func TestDelegatedOpenCodeModelReachesTheTaskUnchanged(t *testing.T) {
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
	require.NoError(t, err)
	tasks := &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db), providers: []string{"codex", "opencode"}}
	f.service.SetApprovals(NewAssistantApprovals(f.repo, tasks))
	a := f.attempt(t, "Implement dark mode on ENG-1 with OpenCode")
	provider, _ := scriptedProvider(t, toolCallStream("delegate_issue", `{"identifier":"eng-1","instructions":"Implement dark mode","harness":"opencode","model":"`+openCodeModel+`"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Delegate"}}, func(string) error { return nil }))
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	var payload domain.DelegationPayload
	require.NoError(t, json.Unmarshal(h.Approvals[0].Payload, &payload))
	require.Equal(t, "opencode", payload.Harness)
	require.Equal(t, openCodeModel, payload.Model)

	approved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, h.Approvals[0].ID, true)
	require.NoError(t, err)
	require.NotNil(t, approved.TaskID)
	var task struct {
		Provider string     `db:"provider"`
		Model    string     `db:"model"`
		Thinking string     `db:"thinking"`
		Server   *uuid.UUID `db:"model_server_id"`
	}
	require.NoError(t, f.db.Get(&task, `SELECT provider,model,thinking,model_server_id FROM agent_tasks WHERE id=$1`, *approved.TaskID))
	require.Equal(t, "opencode", task.Provider)
	require.Equal(t, openCodeModel, task.Model)
	require.Empty(t, task.Thinking, "the default model server's thinking level is not borrowed")
	require.Nil(t, task.Server)
}
