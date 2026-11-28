package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestAssistantEffortUsesHarnessAndModelCapabilities(t *testing.T) {
	machines, _ := catalogService(t, &devMachineStoreFake{}, newGoPlanUpstream(t))
	tasks := &AgentTaskService{machines: machines}
	for _, tc := range []struct {
		harness, model, effort, refusal string
	}{
		{harness: "claude-code", model: "claude-opus-5-5", effort: "medium"},
		{harness: "claude-code", model: "claude-opus-5-5", effort: "off", refusal: "low, medium, high, xhigh, max"},
		{harness: "opencode", model: openCodeModel, effort: "max"},
		{harness: "opencode", model: openCodeModel, effort: "medium", refusal: "low, high, max"},
		{harness: "opencode", model: "opencode-go/qwen-coder", effort: "low", refusal: "supports no explicit effort values"},
		{harness: "qwen-code", effort: "low", refusal: "supports no explicit effort values"},
	} {
		t.Run(tc.harness+"/"+tc.model+"/"+tc.effort, func(t *testing.T) {
			model, err := tasks.validateAssistantEffort(context.Background(), uuid.New(), domain.AgentTaskRequest{
				Provider: tc.harness, Model: tc.model, RequestedEffort: tc.effort,
			})
			if tc.refusal != "" {
				require.ErrorContains(t, err, tc.refusal)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.model, model)
		})
	}
}

func TestAssistantEffortParameters(t *testing.T) {
	for _, tool := range assistantToolCatalog {
		if tool.name != "delegate_issue" && tool.name != "follow_up_task" {
			continue
		}
		t.Run(tool.name, func(t *testing.T) {
			var schema struct {
				Properties map[string]struct{ Enum []string }
				Required   []string
			}
			require.NoError(t, json.Unmarshal([]byte(tool.parameters), &schema))
			require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, schema.Properties["effort"].Enum)
			require.NotContains(t, schema.Required, "effort")
		})
	}
}

func TestAssistantApprovalsUseRequestedEffort(t *testing.T) {
	f := newToolFixture(t)
	requestDelegation(t, f) // Seeds ENG-1 without starting any agent work.
	store := &devMachineStoreFake{policy: &domain.DevMachineWorkspacePolicy{
		Enabled: true, AllowedProviders: json.RawMessage(`["claude-code","opencode"]`),
	}}
	machines, _ := catalogService(t, store, newGoPlanUpstream(t))
	work := repository.NewAgentTaskRepository(f.db)
	tasks := NewAgentTaskService(work, machines, nil)
	f.service.tools.SetAgentWork(work)
	f.service.tools.SetAgentTasks(tasks)
	approvals := NewAssistantApprovals(f.repo, tasks)
	approvals.SetReviews(tasks)
	f.service.SetApprovals(approvals)

	ask := func(tool, arguments string) (domain.ConversationAttempt, *domain.ConversationHistory) {
		t.Helper()
		a := f.attempt(t, "Run the coding agent")
		provider, _ := scriptedProvider(t, toolCallStream(tool, arguments), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Run the coding agent"}}, func(string) error { return nil }))
		history, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Len(t, history.Tools, 1)
		return a, history
	}
	assertRejected := func(tool, arguments, valid string) {
		t.Helper()
		_, history := ask(tool, arguments)
		require.Empty(t, history.Approvals)
		require.Equal(t, "failed", history.Tools[0].Status)
		require.NotNil(t, history.Tools[0].Error)
		require.Contains(t, *history.Tools[0].Error, valid)
	}
	assertStarted := func(tool, arguments, model, effort string) *domain.AgentTask {
		t.Helper()
		a, history := ask(tool, arguments)
		require.Len(t, history.Approvals, 1)
		var output map[string]any
		require.NotNil(t, history.Tools[0].Output)
		require.NoError(t, json.Unmarshal(*history.Tools[0].Output, &output))
		require.Equal(t, "pending_user_approval", output["status"])
		require.Equal(t, model, output["model"])
		require.Equal(t, effort, output["effort"])
		approved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, history.Approvals[0].ID, true)
		require.NoError(t, err)
		require.Equal(t, "approved", approved.Status, "%+v", approved.Error)
		require.NotNil(t, approved.TaskID)
		task, err := work.Get(f.ctx, f.ws, *approved.TaskID)
		require.NoError(t, err)
		require.Equal(t, model, task.Model)
		require.Equal(t, effort, task.Thinking)
		_, history = ask("get_agent_work", `{"identifier":"ENG-1"}`)
		require.NotNil(t, history.Tools[0].Output)
		var workResult struct{ Tasks []repository.IssueAgentWork }
		require.NoError(t, json.Unmarshal(*history.Tools[0].Output, &workResult))
		require.Len(t, workResult.Tasks, 1)
		require.Equal(t, model, workResult.Tasks[0].Model)
		require.Equal(t, effort, workResult.Tasks[0].Effort)
		return task
	}
	block := func(id uuid.UUID) {
		t.Helper()
		_, err := f.db.Exec(`UPDATE agent_tasks SET state='blocked' WHERE id=$1`, id)
		require.NoError(t, err)
	}

	assertRejected("delegate_issue", `{"identifier":"ENG-1","instructions":"Implement it","harness":"opencode","model":"opencode-go/deepseek-v4.1-flash","effort":"medium"}`, "low, high, max")
	a, history := ask("delegate_issue", `{"identifier":"ENG-1","instructions":"Implement it","harness":"opencode","model":"opencode-go/deepseek-v4.1-flash","effort":"low"}`)
	require.Len(t, history.Approvals, 1)
	failed, err := f.service.ResolveApprovalWithModel(f.ctx, f.ws, a.Conversation.ID, history.Approvals[0].ID, true, ModelChoice{Model: "opencode-go/glm-6"})
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Nil(t, failed.TaskID)
	require.NotNil(t, failed.Error)
	require.Contains(t, *failed.Error, "allows the effort levels high")

	task := assertStarted("delegate_issue", `{"identifier":"ENG-1","instructions":"Implement it","harness":"claude-code","model":"claude-sonnet-5","effort":"low"}`, "claude-sonnet-5", "low")
	block(task.ID)
	assertRejected("follow_up_task", `{"identifier":"ENG-1","instructions":"Fix it","model":"Opus 5.5","effort":"off"}`, "low, medium, high, xhigh, max")
	followUp := assertStarted("follow_up_task", `{"identifier":"ENG-1","instructions":"Fix it","model":"Opus 5.5","effort":"medium"}`, "claude-opus-5-5", "medium")
	require.Equal(t, task.ID, followUp.ID)
	require.Equal(t, task.Generation+1, followUp.Generation)
	block(task.ID)
	assertStarted("follow_up_task", `{"identifier":"ENG-1","instructions":"Fix it with more effort","effort":"high"}`, "claude-opus-5-5", "high")
}

// The assistant's effort is checked against the model of the profile that
// writes the code in the chosen pipeline, not the harness it proposed.
func TestAssistantEffortValidatesTheWorkflowsFirstAgent(t *testing.T) {
	f := newProfileFixture(t)
	f.tasks.SetPipelines(true)
	profile := f.storedProfile(t, "Flash", "opencode", openCodeModel, "high")
	workflow, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Flash Workflow", Graph: oneRolePipeline(t, profile.ID)})
	require.NoError(t, err)
	proposal := domain.DelegationPayload{Instructions: "Implement it", Harness: "claude-code", WorkflowID: workflow.ID}
	proposal.Effort = "medium"
	_, err = f.tasks.delegationEffort(f.ctx, f.ws, f.issue, proposal)
	require.ErrorContains(t, err, "low, high, max")
	proposal.Effort = "max"
	model, err := f.tasks.delegationEffort(f.ctx, f.ws, f.issue, proposal)
	require.NoError(t, err)
	require.Equal(t, openCodeModel, model)

	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	options, err := f.tasks.Options(f.ctx, f.ws, chat.ID)
	require.NoError(t, err)
	request, err := delegationRequest(options, uuid.New(), proposal.Instructions, proposal.Harness, proposal.Model, nil, proposal.WorkflowID, proposal.Effort)
	require.NoError(t, err)
	task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, request)
	require.NoError(t, err)
	require.Equal(t, "opencode", task.Provider)
	require.Equal(t, openCodeModel, task.Model)
	require.Equal(t, "max", task.Thinking)
}
