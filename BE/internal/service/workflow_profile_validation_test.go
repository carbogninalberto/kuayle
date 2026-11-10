package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// Qwen Code is a coding agent that takes no model or effort.
const noModelForQwen = "Qwen Code does not accept a model or effort"

// profileFixture is a workspace whose owner manages profiles and workflows
// through the real repositories; only the Dev Machine store is a fake.
type profileFixture struct {
	db        *sqlx.DB
	ctx       context.Context
	ws, user  uuid.UUID
	issue     uuid.UUID
	machines  *devMachineStoreFake
	repo      *repository.WorkflowRepository
	tasks     *AgentTaskService
	workflows *WorkflowService
}

func newProfileFixture(t *testing.T) profileFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	user, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Profile test','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Profile test',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Backlog','backlog','backlog')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Dark mode',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)

	registry := agent.NewRegistry(agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""), agent.NewOpenCodeProvider(""), agent.NewQwenCodeProvider(""))
	machines := &devMachineStoreFake{policy: testPolicy(ws)}
	machines.policy.AllowedProviders = json.RawMessage(`["codex","claude-code","opencode","qwen-code"]`)
	repo := repository.NewWorkflowRepository(db)
	tasks := NewAgentTaskService(repository.NewAgentTaskRepository(db),
		NewDevMachineService(machines, registry, true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{}), nil)
	tasks.SetWorkflows(repo)
	return profileFixture{db: db, ctx: domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user}),
		ws: ws, user: user, issue: issue, machines: machines, repo: repo, tasks: tasks, workflows: NewWorkflowService(repo, registry, tasks)}
}

// storedProfile writes a profile as an earlier release accepted it, without
// the save-time checks.
func (f profileFixture) storedProfile(t *testing.T, name, harness, model, effort string) *domain.AgentProfile {
	t.Helper()
	profile, err := f.repo.CreateProfile(f.ctx, f.ws, f.user, domain.AgentProfileInput{Name: name, Role: domain.AgentRoleImplementer, Harness: harness, Model: model, Effort: effort})
	require.NoError(t, err)
	return profile
}

// oneStepGraph runs a single agent step named Implementer.
func oneStepGraph(t *testing.T, profile uuid.UUID) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(domain.WorkflowGraph{
		Nodes: []domain.WorkflowNode{
			{ID: "start", Type: domain.WorkflowNodeStart, Label: "Start"},
			{ID: "implement", Type: domain.WorkflowNodeAgent, Label: "Implementer", ProfileID: &profile},
			{ID: "done", Type: domain.WorkflowNodeEnd, Label: "Done", Outcome: domain.WorkflowEndDone},
			{ID: "failed", Type: domain.WorkflowNodeEnd, Label: "Failed", Outcome: domain.WorkflowEndFailed},
		},
		Edges: []domain.WorkflowEdge{
			{ID: "start-implement", Source: "start", Target: "implement", Condition: domain.WorkflowElse},
			{ID: "implement-done", Source: "implement", Target: "done", Condition: domain.WorkflowAgentFinished},
			{ID: "implement-failed", Source: "implement", Target: "failed", Condition: domain.WorkflowAgentFailed},
		},
		Budget: domain.WorkflowBudget{MaxIterations: 8, MaxMinutes: 60, OnExhausted: domain.WorkflowOnLimitFail},
	})
	require.NoError(t, err)
	return raw
}

func requireProfileRefusal(t *testing.T, err error, message string) {
	t.Helper()
	var invalid *ErrInvalidWorkflowInput
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, message, invalid.Message)
}

func requireUnusableStep(t *testing.T, issues []domain.WorkflowIssue) {
	t.Helper()
	require.Len(t, issues, 1)
	require.Equal(t, "unusable_profile", issues[0].Code)
	require.Equal(t, "implement", issues[0].NodeID, "the editor shows it on the step")
	require.Equal(t, `Implementer: the profile "Cheap" cannot start a task: `+noModelForQwen, issues[0].Message)
}

// Saving a profile applies the model and effort rules a task start applies,
// so a profile that could never run is refused with the reason.
func TestProfileSaveRefusesSettingsItsCodingAgentCannotRun(t *testing.T) {
	f := newProfileFixture(t)
	request := func(name, harness, model, effort string) AgentProfileRequest {
		return AgentProfileRequest{Name: name, Role: domain.AgentRoleImplementer, Harness: harness, Model: model, Effort: effort}
	}

	_, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Cheap", "qwen-code", "deepseek-v4.1-flash", "high"))
	requireProfileRefusal(t, err, noModelForQwen)
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Cheap", "qwen-code", "", "high"))
	requireProfileRefusal(t, err, noModelForQwen)
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Loud", "codex", "gpt-5.5", "extreme"))
	requireProfileRefusal(t, err, "the effort is one of off, low, medium, high, xhigh, max")
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Odd", "codex", "not a model", ""))
	requireProfileRefusal(t, err, "the model name is not valid")

	plain, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Plain", "qwen-code", "", ""))
	require.NoError(t, err)
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Tuned", "codex", "gpt-5.5", "high"))
	require.NoError(t, err)
	_, err = f.workflows.UpdateProfile(f.ctx, f.ws, plain.ID, request("Plain", "qwen-code", "deepseek-v4.1-flash", ""))
	requireProfileRefusal(t, err, noModelForQwen)

	// The workspace policy can change after a save, so it is not a save rule.
	f.machines.policy.AllowedProviders = json.RawMessage(`["codex"]`)
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Later", "qwen-code", "", ""))
	require.NoError(t, err, "an agent the workspace does not allow now can still have a profile")
}

// A profile stored before the check still lists and opens; the rule applies
// when it is next saved.
func TestStoredProfileThatCannotRunStaysEditable(t *testing.T) {
	f := newProfileFixture(t)
	stored := f.storedProfile(t, "Cheap", "qwen-code", "deepseek-v4.1-flash", "high")

	list, err := f.workflows.Profiles(f.ctx, f.ws)
	require.NoError(t, err)
	var listed *domain.AgentProfile
	for i := range list.Profiles {
		if list.Profiles[i].ID == stored.ID {
			listed = &list.Profiles[i]
		}
	}
	require.NotNil(t, listed)
	require.Equal(t, "deepseek-v4.1-flash", listed.Model)
	require.Equal(t, "high", listed.Effort)

	// The sheet learns from the list which coding agents take a model.
	raw, err := json.Marshal(list)
	require.NoError(t, err)
	var body struct {
		Harnesses      []string `json:"harnesses"`
		ModelHarnesses []string `json:"model_harnesses"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.ElementsMatch(t, []string{"codex", "claude-code", "opencode", "qwen-code"}, body.Harnesses)
	require.ElementsMatch(t, []string{"codex", "claude-code", "opencode"}, body.ModelHarnesses)

	update := AgentProfileRequest{Name: "Cheap", Role: stored.Role, Harness: "qwen-code", Model: stored.Model, Effort: stored.Effort}
	_, err = f.workflows.UpdateProfile(f.ctx, f.ws, stored.ID, update)
	requireProfileRefusal(t, err, noModelForQwen)
	update.Model, update.Effort = "", ""
	fixed, err := f.workflows.UpdateProfile(f.ctx, f.ws, stored.ID, update)
	require.NoError(t, err)
	require.Empty(t, fixed.Model)
	require.Empty(t, fixed.Effort)
}

// A workflow whose agent step runs a profile that cannot start is reported
// on that step, naming the step and the profile, when it is validated, saved
// or chosen as a default.
func TestWorkflowWithAProfileThatCannotStartIsReported(t *testing.T) {
	f := newProfileFixture(t)
	cheap := f.storedProfile(t, "Cheap", "qwen-code", "deepseek-v4.1-flash", "high")
	sound := f.storedProfile(t, "Sound", "codex", "gpt-5.5", "high")
	issuesOf := func(err error) []domain.WorkflowIssue {
		var invalid *domain.WorkflowInvalidError
		require.ErrorAs(t, err, &invalid)
		return invalid.Issues
	}

	issues, err := f.workflows.Validate(f.ctx, f.ws, oneStepGraph(t, cheap.ID))
	require.NoError(t, err)
	requireUnusableStep(t, issues)
	issues, err = f.workflows.Validate(f.ctx, f.ws, oneStepGraph(t, sound.ID))
	require.NoError(t, err)
	require.Empty(t, issues)

	_, err = f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Cheap Workflow", Graph: oneStepGraph(t, cheap.ID)})
	requireUnusableStep(t, issuesOf(err))
	created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Sound Workflow", Graph: oneStepGraph(t, sound.ID)})
	require.NoError(t, err)
	_, err = f.workflows.Save(f.ctx, f.ws, created.ID, WorkflowRequest{Name: "Sound Workflow", ExpectedVersion: 1, Graph: oneStepGraph(t, cheap.ID)})
	requireUnusableStep(t, issuesOf(err))
	kept, err := f.workflows.Get(f.ctx, f.ws, created.ID)
	require.NoError(t, err)
	require.Equal(t, 1, kept.LatestVersion, "the refused version was not saved")

	// A workflow saved before the check cannot become a default as it is.
	legacy, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Cheap Workflow", "", oneStepGraph(t, cheap.ID), "created")
	require.NoError(t, err)
	legacyID := legacy.ID.String()
	_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: &legacyID})
	requireUnusableStep(t, issuesOf(err))
	defaults, err := f.repo.Defaults(f.ctx, f.ws)
	require.NoError(t, err)
	require.Empty(t, defaults, "the refused default was not stored")
	list, err := f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: &created.ID})
	require.NoError(t, err)
	require.Len(t, list.Defaults, 1)
}

// A task that cannot start because of a workflow step's profile says which
// workflow, step and profile, and what is wrong with it, both to the task
// composer (the error's text) and on the assistant's approval card.
func TestTaskStartNamesTheWorkflowStepThatCannotRun(t *testing.T) {
	f := newProfileFixture(t)
	cheap := f.storedProfile(t, "Cheap", "qwen-code", "deepseek-v4.1-flash", "high")
	legacy, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Cheap Workflow", "", oneStepGraph(t, cheap.ID), "created")
	require.NoError(t, err)
	require.NoError(t, f.repo.SetDefault(f.ctx, f.ws, f.user, domain.WorkflowScopeWorkspace, f.ws, &legacy.ID, false))
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	start := func() error {
		_, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement it", Provider: "codex"})
		return err
	}

	const unusable = `Workflow "Cheap Workflow", step "Implementer" (profile "Cheap"): ` + noModelForQwen
	err = start()
	require.ErrorIs(t, err, ErrInvalidMachineInput, "the task composer still gets a 400")
	require.Equal(t, unusable, err.Error())
	message, permanent := delegationFailure(err)
	require.True(t, permanent)
	require.Equal(t, unusable, message)

	// The approval card shows that reason instead of the generic sentence.
	approvals := newToolFixture(t)
	a, approval, tasks := requestDelegation(t, approvals)
	tasks.createErr = err
	failed, resolveErr := approvals.service.ResolveApproval(approvals.ctx, approvals.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, resolveErr)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, unusable, *failed.Error)

	// An agent the workspace stopped allowing is named the same way.
	_, err = f.workflows.UpdateProfile(f.ctx, f.ws, cheap.ID, AgentProfileRequest{Name: "Cheap", Role: cheap.Role, Harness: "qwen-code"})
	require.NoError(t, err)
	f.machines.policy.AllowedProviders = json.RawMessage(`["codex"]`)
	err = start()
	require.ErrorIs(t, err, ErrProviderNotAllowed)
	message, permanent = delegationFailure(err)
	require.True(t, permanent)
	require.Equal(t, `Workflow "Cheap Workflow", step "Implementer" (profile "Cheap"): qwen-code is not allowed in this workspace`, message)

	f.machines.policy.AllowedProviders = json.RawMessage(`["codex","qwen-code"]`)
	require.NoError(t, start(), "the repaired profile starts a task")
}

// A refusal without a specific reason keeps the generic sentence; one that
// names a model problem says it.
func TestDelegationFailureKeepsTheGenericSentenceAsAFallback(t *testing.T) {
	message, permanent := delegationFailure(ErrInvalidMachineInput)
	require.True(t, permanent)
	require.Equal(t, "The coding task request was rejected", message)

	service := &DevMachineService{store: modelServerStore{}}
	message, permanent = delegationFailure(service.validateAgentModel(context.Background(), uuid.New(), "qwen-code", nil, "gpt-5", ""))
	require.True(t, permanent)
	require.Equal(t, "The coding task request was rejected: qwen-code does not accept a model or effort", message)
}

// An OpenCode profile chooses a provider/model and an effort: it saves, its
// workflow validates and becomes a default, and a task started through it is
// an OpenCode task with that model and effort and no model server, even when
// the workspace has a default one.
func TestOpenCodeProfileStartsATaskWithItsModelAndEffort(t *testing.T) {
	f := newProfileFixture(t)
	const model = "opencode-go/deepseek-v4.1-flash"
	request := func(name, model, effort string) AgentProfileRequest {
		return AgentProfileRequest{Name: name, Role: domain.AgentRoleImplementer, Harness: "opencode", Model: model, Effort: effort}
	}
	// The workspace's default model server serves Codex and Claude Code.
	server := deepSeekModelServer(f.ws)
	require.NoError(t, f.db.Get(&server.ID, `INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,anthropic_base_url,encrypted_api_key,models,default_model,thinking,is_default) VALUES($1,'DeepSeek','https://api.deepseek.com','https://api.deepseek.com/anthropic','sealed','["deepseek-flash","deepseek-v4-pro"]','deepseek-flash','high',true) RETURNING id`, f.ws))
	f.machines.modelServers = []domain.AgentModelServer{server}

	_, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Bare", "deepseek-v4.1-flash", "high"))
	requireProfileRefusal(t, err, "OpenCode takes its model as provider/model, for example opencode-go/deepseek-v4.1-flash")
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Loud", model, "minimal"))
	requireProfileRefusal(t, err, "the effort is one of off, low, medium, high, xhigh, max")
	flash, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Flash", model, "high"))
	require.NoError(t, err)
	require.Equal(t, model, flash.Model)
	require.Equal(t, "high", flash.Effort)
	plain, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Plain", "", ""))
	require.NoError(t, err)

	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	startThrough := func(name string, profile uuid.UUID) *domain.AgentTask {
		t.Helper()
		issues, err := f.workflows.Validate(f.ctx, f.ws, oneStepGraph(t, profile))
		require.NoError(t, err)
		require.Empty(t, issues)
		workflow, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: name, Graph: oneStepGraph(t, profile)})
		require.NoError(t, err)
		_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: &workflow.ID})
		require.NoError(t, err)
		// The composer asked for Codex; the workflow's first step decides.
		task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement it", Provider: "codex", Model: "gpt-5.5", Thinking: "low"})
		require.NoError(t, err)
		var stored domain.AgentTask
		require.NoError(t, f.db.Get(&stored, `SELECT * FROM agent_tasks WHERE id=$1`, task.ID))
		return &stored
	}

	task := startThrough("Flash Workflow", flash.ID)
	require.Equal(t, "opencode", task.Provider)
	require.Equal(t, model, task.Model)
	require.Equal(t, "high", task.Thinking)
	require.Nil(t, task.ModelServerID, "OpenCode does not run on the default model server")

	_, err = f.db.Exec(`UPDATE agent_tasks SET state='cancelled' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	task = startThrough("Plain Workflow", plain.ID)
	require.Equal(t, "opencode", task.Provider)
	require.Empty(t, task.Model, "OpenCode keeps the model it is configured with")
	require.Empty(t, task.Thinking)
	require.Nil(t, task.ModelServerID)
}
