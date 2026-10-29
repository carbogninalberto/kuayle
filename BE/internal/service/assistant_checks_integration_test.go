package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// checkTaskFake runs checks through the real task repository.
type checkTaskFake struct {
	repo *repository.AgentTaskRepository
}

func (f checkTaskFake) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	return f.repo.Get(ctx, ws, id)
}
func (f checkTaskFake) RunCheck(ctx context.Context, ws, id uuid.UUID, command []string) (*domain.DevMachineAgentRunStep, error) {
	return f.repo.RunCheck(ctx, ws, id, command)
}

// checkableTask gives a published task its ready checkout, the attempt's
// redaction snapshot and an enabled Dev Machine policy.
func checkableTask(t *testing.T, f toolFixture, task uuid.UUID) {
	t.Helper()
	publishTask(t, f, task)
	_, err := f.db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true)`, f.ws)
	require.NoError(t, err)
	installation, repo := uuid.New(), uuid.New()
	_, err = f.db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,$3,'fixture','Organization',$4)`, installation, f.ws, time.Now().UnixNano(), f.user)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,$4,'acme/app','main')`, repo, installation, f.ws, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key)
 SELECT t.checkout_id,t.workspace_id,t.machine_id,t.issue_id,$2,'acme/app','main','kuayle/eng-1','/workspace/tasks/eng-1','ready',t.id::text FROM agent_tasks t WHERE t.id=$1`, task, repo)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE dev_machine_agent_runs SET encrypted_output_secrets='sealed',requested_by_user_id=$2 WHERE task_id=$1`, task, f.user)
	require.NoError(t, err)
}

// Running a check needs approval, runs the exact command shown on the card
// without an attempt, and its result reaches the assistant through get_agent_work.
func TestRunCheckNeedsApprovalAndReportsTheResult(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	taskID := *started.TaskID
	checkableTask(t, f, taskID)
	work := repository.NewAgentTaskRepository(f.db)
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetChecks(checkTaskFake{work})
	steps := func() int {
		var n int
		require.NoError(t, f.db.Get(&n, `SELECT count(*) FROM dev_machine_agent_run_steps s JOIN agent_tasks t ON t.run_id=s.agent_run_id WHERE t.id=$1 AND s.step_type='manual_check'`, taskID))
		return n
	}
	ask := func(arguments string) (domain.ConversationAttempt, domain.ConversationApproval) {
		t.Helper()
		a := f.attempt(t, "Run the tests")
		provider, requests := scriptedProvider(t, toolCallStream("run_check", arguments), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Run the tests"}}, func(string) error { return nil }))
		history := (*requests)[1]["messages"].([]any)
		require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Len(t, h.Approvals, 1)
		return a, h.Approvals[0]
	}

	a, approval := ask(`{"identifier":"eng-1","command":"npm test -- auth"}`)
	require.Equal(t, "run_check", approval.Action)
	var payload domain.RunCheckPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, domain.RunCheckPayload{Identifier: "ENG-1", Title: "Dark mode", TaskID: taskID, Generation: 1, Command: []string{"sh", "-c", "npm test -- auth"}}, payload)
	require.Zero(t, steps(), "asking runs nothing")
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, taskID, *result.TaskID)
	require.Equal(t, 1, steps())
	var argv string
	require.NoError(t, f.db.Get(&argv, `SELECT s.command_argv::text FROM dev_machine_agent_run_steps s JOIN agent_tasks t ON t.run_id=s.agent_run_id WHERE t.id=$1 AND s.step_type='manual_check'`, taskID))
	require.JSONEq(t, `["sh","-c","npm test -- auth"]`, argv)
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", again.Status)
	require.Equal(t, 1, steps(), "a repeated approval runs nothing more")
	current, err := work.Get(f.ctx, f.ws, taskID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", current.State)
	require.Equal(t, int64(1), current.Generation)

	// While that check runs, another one is refused with a clear reason.
	a, approval = ask(`{"identifier":"ENG-1"}`)
	failed, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, "The agent is working; try again when it pauses", *failed.Error)

	output, _, err := f.service.tools.run(f.ctx, f.ws, f.user, "get_agent_work", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	require.Contains(t, string(data), `"manual_check":{"status":"queued"`)
	require.Contains(t, string(data), `"command":["sh","-c","npm test -- auth"]`)

	_, err = f.db.Exec(`UPDATE agent_tasks SET state='running' WHERE id=$1`, taskID)
	require.NoError(t, err)
	_, _, err = f.service.tools.prepareRunCheck(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "the agent is working")
	_, _, err = f.service.tools.prepareRunCheck(f.ctx, f.ws, f.user, `{"identifier":"ENG-404"}`)
	require.ErrorContains(t, err, "issue not found")
	_, _, err = f.service.tools.prepareRunCheck(f.ctx, f.ws, f.user, `{"identifier":"ENG-1","command":"`+strings.Repeat("x", 2001)+`"}`)
	require.ErrorContains(t, err, "at most 2000 characters")
	other := uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Teammate','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.ws, other)
	require.NoError(t, err)
	_, _, err = f.service.tools.prepareRunCheck(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other}), f.ws, other, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "only the task's creator")
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, _, err = f.service.tools.prepareRunCheck(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "no longer have access")
}
