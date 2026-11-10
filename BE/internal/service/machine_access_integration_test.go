package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// In demo mode a member who is not an instance administrator cannot start a
// coding task through the assistant: approving a delegation never passes the
// Dev Machine routes, so the services refuse it.
func TestDemoModeRefusesAssistantDelegation(t *testing.T) {
	taskCount := func(t *testing.T, f toolFixture) int {
		t.Helper()
		var count int
		require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
		return count
	}
	t.Run("approving is refused by the approval check", func(t *testing.T) {
		f := newToolFixture(t)
		// The approval was requested before the rule applied to this member.
		a, approval, tasks := requestDelegation(t, f)
		approvals := NewAssistantApprovals(f.repo, tasks)
		approvals.SetMachineAccess(demoAccess(uuid.New()))
		f.service.SetApprovals(approvals)
		refused, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		require.Equal(t, "failed", refused.Status)
		require.Equal(t, demoMachineRefusal, *refused.Error)
		require.Nil(t, refused.TaskID)
		require.Zero(t, tasks.creates, "the task service is not asked")
		require.Zero(t, taskCount(t, f))
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationConflict, "the refusal is final")
	})
	t.Run("approving is refused by the task service", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, _ := requestDelegation(t, f)
		store := repository.NewDevMachineRepository(f.db)
		require.NoError(t, store.UpsertPolicy(f.ctx, &domain.DevMachineWorkspacePolicy{WorkspaceID: f.ws, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10, MaxDailyAgentRuns: 25, MaxDiskGB: 100,
			AllowedProviders: json.RawMessage(`["codex"]`), AllowedRepositories: json.RawMessage(`[]`)}))
		machines := NewDevMachineService(store, agent.NewRegistry(agent.NewCodexProvider("test")), true, "machines.test.invalid", make([]byte, 32), time.Minute, DevMachineImages{})
		machines.SetMachineAccess(demoAccess(uuid.New()))
		// Only the task service knows the rule here, as for automations,
		// follow-ups and GitHub mentions, which have no approval check.
		f.service.SetApprovals(NewAssistantApprovals(f.repo, NewAgentTaskService(repository.NewAgentTaskRepository(f.db), machines, nil)))
		refused, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		require.Equal(t, "failed", refused.Status)
		require.Equal(t, demoMachineRefusal, *refused.Error)
		require.Zero(t, taskCount(t, f))
	})
	t.Run("asking is refused", func(t *testing.T) {
		f := newToolFixture(t)
		var team uuid.UUID
		require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
		_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
		require.NoError(t, err)
		f.service.tools.SetMachineAccess(demoAccess(uuid.New()))
		f.service.SetApprovals(NewAssistantApprovals(f.repo, &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)}))
		a := f.attempt(t, "Implement dark mode on ENG-1")
		provider, requests := scriptedProvider(t, toolCallStream("delegate_issue", `{"identifier":"eng-1","instructions":"Implement dark mode"}`), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Delegate"}}, func(string) error { return nil }))
		history := (*requests)[1]["messages"].([]any)
		require.Contains(t, history[len(history)-1].(map[string]any)["content"], demoMachineRefusal)
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Empty(t, h.Approvals, "no approval card is created")
	})
}
