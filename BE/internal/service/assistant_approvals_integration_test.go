package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// approvalTaskFake keeps Dev Machine policy out of the test but uses the real
// task repository, so task identity, authorization and replay are genuine.
type approvalTaskFake struct {
	repo       *repository.AgentTaskRepository
	optionsErr error
	createErr  error
	mu         sync.Mutex
	creates    int
}

func (f *approvalTaskFake) Options(context.Context, uuid.UUID, uuid.UUID) (*AgentTaskOptions, error) {
	if f.optionsErr != nil {
		return nil, f.optionsErr
	}
	return &AgentTaskOptions{Providers: []dto.AgentProviderResponse{{ID: "codex"}}, ModelProvider: &dto.WorkspaceModelProviderResponse{Configured: true, DefaultModel: "deepseek-flash", Thinking: "high"}}, nil
}
func (f *approvalTaskFake) Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	f.mu.Lock()
	f.creates++
	err := f.createErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if accepted, err := f.repo.ReplayCreate(ctx, ws, conversation, input); err != nil || accepted != nil {
		return accepted, err
	}
	return f.repo.Create(ctx, ws, conversation, input)
}

// requestDelegation drives a real assistant reply that asks to delegate ENG-1.
func requestDelegation(t *testing.T, f toolFixture) (domain.ConversationAttempt, domain.ConversationApproval, *approvalTaskFake) {
	t.Helper()
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
	require.NoError(t, err)
	tasks := &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)}
	f.service.SetApprovals(NewAssistantApprovals(f.repo, tasks))
	a := f.attempt(t, "Implement dark mode on ENG-1")
	provider, requests := scriptedProvider(t, toolCallStream("delegate_issue", `{"identifier":"eng-1","instructions":"Implement dark mode and add tests"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Delegate"}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Zero(t, count, "requesting delegation starts nothing")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	approval := h.Approvals[0]
	require.Equal(t, "pending", approval.Status)
	require.Equal(t, f.user, approval.ActorID)
	var payload domain.DelegationPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, domain.DelegationPayload{Identifier: "ENG-1", Title: "Dark mode", Instructions: "Implement dark mode and add tests"}, payload)
	return a, approval, tasks
}

func TestApprovedDelegationStartsExactlyOneTask(t *testing.T) {
	f := newToolFixture(t)
	a, approval, tasks := requestDelegation(t, f)
	var wg sync.WaitGroup
	results := make([]*domain.ConversationApproval, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		})
	}
	wg.Wait()
	for i := range results {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i].TaskID)
		require.Equal(t, *results[0].TaskID, *results[i].TaskID, "double clicks and two clients converge on one task")
	}
	var tasksCreated int
	require.NoError(t, f.db.Get(&tasksCreated, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, tasksCreated)
	var task struct {
		Prompt   string `db:"prompt"`
		Provider string `db:"provider"`
		Model    string `db:"model"`
		Issue    string `db:"identifier_text"`
	}
	require.NoError(t, f.db.Get(&task, `SELECT t.prompt,t.provider,t.model,i.identifier_text FROM agent_tasks t JOIN issues i ON i.id=t.issue_id WHERE t.id=$1`, *results[0].TaskID))
	require.Equal(t, "Implement dark mode and add tests", task.Prompt, "the approved instructions, not later model text, are executed")
	require.Equal(t, "codex", task.Provider)
	require.Equal(t, "deepseek-flash", task.Model)
	require.Equal(t, "ENG-1", task.Issue)
	_, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
	require.ErrorIs(t, err, repository.ErrConversationConflict, "an approved decision cannot be denied")
	var events []string
	require.NoError(t, f.db.Select(&events, `SELECT type FROM conversation_events WHERE conversation_id=$1 AND type LIKE 'approval.%' ORDER BY cursor`, a.Conversation.ID))
	require.Equal(t, []string{"approval.requested", "approval.updated", "approval.resolved"}, events)
	require.LessOrEqual(t, tasks.creates, 4)
}

func TestApprovalDenialExpiryAndActorBinding(t *testing.T) {
	t.Run("deny_is_final", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		denied, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
		require.NoError(t, err)
		require.Equal(t, "denied", denied.Status)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationConflict)
		again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
		require.NoError(t, err)
		require.Equal(t, "denied", again.Status)
		require.Zero(t, tasks.creates)
	})
	t.Run("expired", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		_, err := f.db.Exec(`UPDATE conversation_approvals SET expires_at=now()-interval '1 second' WHERE id=$1`, approval.ID)
		require.NoError(t, err)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrApprovalExpired)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrApprovalExpired)
		require.Zero(t, tasks.creates)
	})
	t.Run("other_member_cannot_resolve", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		other := uuid.New()
		_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
		_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, f.ws, other)
		require.NoError(t, err)
		otherCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other})
		_, err = f.service.ResolveApproval(otherCtx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationNotFound)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, uuid.New(), approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationNotFound, "an approval is bound to its conversation")
		require.Zero(t, tasks.creates)
	})
	t.Run("permanent_and_transient_failures", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		tasks.createErr = errors.New("database briefly unavailable")
		_, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.Error(t, err)
		var status string
		require.NoError(t, f.db.Get(&status, `SELECT status FROM conversation_approvals WHERE id=$1`, approval.ID))
		require.Equal(t, "approving", status, "a transient failure can be resumed")
		tasks.createErr = ErrDevMachinesDisabled
		failed, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		require.Equal(t, "failed", failed.Status)
		require.Equal(t, "Coding agents are not enabled for this workspace", *failed.Error)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationConflict)
	})
}
