package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

const e2eToolParameters = `{"type":"object","properties":{"spec":{"type":"string","maxLength":200}},"required":["spec"]}`

func withCustomTools(t *testing.T, f toolFixture) *repository.AssistantCustomToolRepository {
	t.Helper()
	tools := repository.NewAssistantCustomToolRepository(f.db)
	f.service.tools.SetCustomTools(tools)
	f.service.tools.SetSettings(repository.NewAssistantToolSettingsRepository(f.db))
	f.service.tools.customToolWait = 50 * time.Millisecond
	return tools
}

func addCustomTool(t *testing.T, f toolFixture, tools *repository.AssistantCustomToolRepository, name, status, mode string) *domain.AssistantCustomTool {
	t.Helper()
	setRole(t, f, "admin")
	defer setRole(t, f, "member")
	in, err := validateCustomTool(AssistantCustomToolRequest{Name: name, Description: "Run the Playwright spec the user names.", Parameters: json.RawMessage(e2eToolParameters),
		Script: `npx playwright test "$KUAYLE_ARG_SPEC"`, RunMode: mode, ReadOnly: mode == domain.AssistantToolRunAuto, Status: status})
	require.NoError(t, err)
	tool, err := tools.Create(f.ctx, f.ws, f.user, in)
	require.NoError(t, err)
	return tool
}

func ownMachine(t *testing.T, f toolFixture) uuid.UUID {
	t.Helper()
	_, err := f.db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true) ON CONFLICT (workspace_id) DO NOTHING`, f.ws)
	require.NoError(t, err)
	machine := uuid.New()
	_, err = f.db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at)
 VALUES($1,$2,$3,$4,'Mine','running','running',1,'','github','','','','','small',1000,2048,20,60,now()+interval '1 hour')`, machine, f.ws, f.user, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	return machine
}

func TestCustomToolNamesCannotTakeBuiltInNames(t *testing.T) {
	base := AssistantCustomToolRequest{Description: "x", Script: "echo"}
	for _, name := range []string{"run_check", "propose_tool", "get_issue", "attach_images_to_issue", "Run", "run-e2e"} {
		base.Name = name
		_, err := validateCustomTool(base)
		require.Error(t, err, name)
	}
	base.Name = "run_e2e"
	in, err := validateCustomTool(base)
	require.NoError(t, err)
	require.Equal(t, "sh", in.Interpreter)
	require.Equal(t, domain.AssistantToolRunApproval, in.RunMode)
	require.Equal(t, 600, in.TimeoutSeconds)
	base.RunMode = domain.AssistantToolRunAuto
	_, err = validateCustomTool(base)
	require.ErrorContains(t, err, "read-only")
	base.RunMode, base.Interpreter = "", "perl"
	_, err = validateCustomTool(base)
	require.ErrorContains(t, err, "interpreter")
}

// Only active, enabled tools are offered, namespaced; a draft, a disabled or
// an unknown custom tool called anyway never runs.
func TestCustomToolsAreOfferedOnlyWhenActiveAndEnabled(t *testing.T) {
	f := newToolFixture(t)
	tools := withCustomTools(t, f)
	addCustomTool(t, f, tools, "run_e2e", "active", "")
	addCustomTool(t, f, tools, "draft_tool", "draft", "")
	off := addCustomTool(t, f, tools, "old_tool", "active", "")
	setRole(t, f, "admin")
	_, err := tools.SetEnabled(f.ctx, f.ws, off.ID, f.user, false)
	require.NoError(t, err)
	setRole(t, f, "member")
	machine := ownMachine(t, f)
	a := f.attempt(t, "Run it")
	provider, requests := scriptedProvider(t, toolCallStream("custom_draft_tool", `{"machine_id":"`+machine.String()+`","inputs":{"spec":"a"}}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Run"}}, func(string) error { return nil }))
	offered := offeredToolNames((*requests)[0])
	require.Contains(t, offered, "custom_run_e2e")
	require.Contains(t, offered, "propose_tool")
	require.Contains(t, offered, "get_tool_run")
	require.NotContains(t, offered, "custom_draft_tool")
	require.NotContains(t, offered, "custom_old_tool")
	status, failure, _ := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "failed", status)
	require.Contains(t, failure, "not active")
	var runs int
	require.NoError(t, f.db.Get(&runs, `SELECT count(*) FROM assistant_tool_runs WHERE workspace_id=$1`, f.ws))
	require.Zero(t, runs)

	for _, spec := range f.service.tools.specs() {
		require.False(t, strings.HasPrefix(spec.Function.Name, domain.CustomToolPrefix), "no built-in uses the custom namespace")
	}
	disableAssistantTool(t, f, "propose_tool")
	capabilities, err := f.service.Capabilities(f.ctx, f.ws)
	require.NoError(t, err)
	names := map[string]AssistantCapability{}
	for _, capability := range capabilities.Tools {
		names[capability.Name] = capability
	}
	require.True(t, names["custom_run_e2e"].Custom)
	require.True(t, names["custom_run_e2e"].RequiresApproval)
	require.False(t, names["custom_old_tool"].Enabled)
	require.NotContains(t, names, "custom_draft_tool")
	require.False(t, names["propose_tool"].Enabled)
}

// propose_tool saves a draft only, for owners and admins, and the card shows
// the full script and schema.
func TestProposeToolSavesADraft(t *testing.T) {
	f := newToolFixture(t)
	tools := withCustomTools(t, f)
	propose := `{"name":"run_spec","description":"Run a Playwright spec","parameters":{"type":"object","properties":{"spec":{"type":"string"}},"required":["spec"]},"script":"npx playwright test \"$KUAYLE_ARG_SPEC\"","interpreter":"sh","read_only":true}`
	a := f.attempt(t, "Make a tool")
	provider, _ := scriptedProvider(t, toolCallStream("propose_tool", propose), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Make"}}, func(string) error { return nil }))
	status, failure, _ := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "failed", status)
	require.Contains(t, failure, "owners and admins")

	setRole(t, f, "admin")
	a = f.attempt(t, "Make a tool")
	provider, requests := scriptedProvider(t, toolCallStream("propose_tool", propose), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Make"}}, func(string) error { return nil }))
	status, _, card := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "completed", status)
	require.Equal(t, `npx playwright test "$KUAYLE_ARG_SPEC"`, card["script"])
	require.Equal(t, "draft", card["status"])
	require.Equal(t, "approval", card["run_mode"], "a proposal never runs without approval")
	require.Contains(t, card["parameters"], "properties")
	history := (*requests)[1]["messages"].([]any)
	receipt := history[len(history)-1].(map[string]any)["content"].(string)
	require.Contains(t, receipt, "untrusted_data")
	require.NotContains(t, receipt, "playwright test", "the model gets a receipt, not the script")
	list, err := tools.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "draft", list[0].Status)
}

// An approval-mode tool asks first, binds the exact version and inputs, and
// queues one run on the task's checkout once approved; the result reaches the
// model through get_tool_run.
func TestApprovalToolRunsOnTheTaskMachineAfterApproval(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	taskID := *started.TaskID
	checkableTask(t, f, taskID)
	work := repository.NewAgentTaskRepository(f.db)
	f.service.tools.SetAgentWork(work)
	tools := withCustomTools(t, f)
	f.service.approvals.SetCustomTools(tools)
	tool := addCustomTool(t, f, tools, "run_e2e", "active", "")
	runs := func() int {
		var n int
		require.NoError(t, f.db.Get(&n, `SELECT count(*) FROM assistant_tool_runs WHERE tool_id=$1`, tool.ID))
		return n
	}

	a = f.attempt(t, "Run the e2e for login on ENG-1")
	provider, requests := scriptedProvider(t, toolCallStream("custom_run_e2e", `{"identifier":"eng-1","inputs":{"spec":"; rm -rf / $(id)"}}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Run"}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
	require.Zero(t, runs(), "asking runs nothing")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	approval := h.Approvals[0]
	require.Equal(t, "run_custom_tool", approval.Action)
	var payload domain.CustomToolRunPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, "ENG-1", payload.Identifier)
	require.Equal(t, 1, payload.Version)
	require.Equal(t, taskID, *payload.TaskID)
	require.JSONEq(t, `{"spec":"; rm -rf / $(id)"}`, string(payload.Inputs))

	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, 1, runs())
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", again.Status)
	require.Equal(t, 1, runs(), "a repeated approval runs nothing more")
	var run domain.AssistantToolRun
	require.NoError(t, f.db.Get(&run, `SELECT id,working_directory,task_id,inputs,status FROM assistant_tool_runs WHERE tool_id=$1`, tool.ID))
	require.Equal(t, "/workspace/tasks/eng-1", run.WorkingDirectory)
	require.Equal(t, taskID, *run.TaskID)
	require.JSONEq(t, `{"spec":"; rm -rf / $(id)"}`, string(run.Inputs))

	output, _, err := f.service.tools.runCustomToolTool(f.ctx, a, "get_tool_run", `{"approval_id":"`+approval.ID.String()+`"}`)
	require.NoError(t, err)
	require.Equal(t, "queued", output.(map[string]any)["status"])

	// An edit since the request is refused when approving.
	a = f.attempt(t, "Run it again")
	provider, _ = scriptedProvider(t, toolCallStream("custom_run_e2e", `{"identifier":"ENG-1","inputs":{"spec":"login"}}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Run"}}, func(string) error { return nil }))
	h, err = f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	setRole(t, f, "admin")
	in, err := validateCustomTool(AssistantCustomToolRequest{Name: "run_e2e", Description: "Changed", Parameters: json.RawMessage(e2eToolParameters), Script: "echo changed"})
	require.NoError(t, err)
	_, err = tools.Update(f.ctx, f.ws, tool.ID, f.user, in)
	require.NoError(t, err)
	setRole(t, f, "member")
	failed, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, h.Approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, "The tool changed or was turned off; ask again", *failed.Error)

	// Inputs are checked against the schema before anything is asked.
	_, err = f.service.tools.prepareCustomTool(f.ctx, f.ws, f.user, "run_e2e", `{"identifier":"ENG-1","inputs":{"spec":5}}`)
	require.ErrorContains(t, err, "must be a string")
	_, err = f.service.tools.prepareCustomTool(f.ctx, f.ws, f.user, "run_e2e", `{"identifier":"ENG-1","inputs":{}}`)
	require.ErrorContains(t, err, "spec is required")
}

// A read-only auto tool runs without approval on the user's own machine; the
// reply returns its state as untrusted data.
func TestAutoToolRunsWithoutApproval(t *testing.T) {
	f := newToolFixture(t)
	tools := withCustomTools(t, f)
	addCustomTool(t, f, tools, "list_specs", "active", domain.AssistantToolRunAuto)
	machine := ownMachine(t, f)
	a := f.attempt(t, "List the specs")
	provider, requests := scriptedProvider(t, toolCallStream("custom_list_specs", `{"machine_id":"`+machine.String()+`","inputs":{"spec":"*"}}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "List"}}, func(string) error { return nil }))
	status, failure, result := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "completed", status, failure)
	require.Equal(t, "queued", result["status"])
	require.Contains(t, result["note"], "get_tool_run")
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "untrusted_data")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Empty(t, h.Approvals)

	// The machine is held: a second run waits for the first.
	a = f.attempt(t, "Again")
	provider, _ = scriptedProvider(t, toolCallStream("custom_list_specs", `{"machine_id":"`+machine.String()+`","inputs":{"spec":"*"}}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Again"}}, func(string) error { return nil }))
	status, failure, _ = toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "failed", status)
	require.Contains(t, failure, "the agent is working on this machine")

	// Someone else's machine is refused.
	other := uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	_, err = f.db.Exec(`UPDATE dev_machines SET created_by_user_id=$2 WHERE id=$1`, machine, other)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE machine_id=$1`, machine)
	require.NoError(t, err)
	a = f.attempt(t, "Again")
	provider, _ = scriptedProvider(t, toolCallStream("custom_list_specs", `{"machine_id":"`+machine.String()+`","inputs":{"spec":"*"}}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Again"}}, func(string) error { return nil }))
	_, failure, _ = toolCallRow(t, f, a.Message.ID)
	require.Contains(t, failure, "running machine of yours")
}
