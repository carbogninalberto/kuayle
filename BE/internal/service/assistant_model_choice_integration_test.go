package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// twoServerTasks offers the workspace default server and a second one that
// both list the same model name, as two gateways to one provider would.
type twoServerTasks struct {
	*approvalTaskFake
	defaultID, otherID uuid.UUID
}

func (f *twoServerTasks) Options(context.Context, uuid.UUID, uuid.UUID) (*AgentTaskOptions, error) {
	return &AgentTaskOptions{Providers: []dto.AgentProviderResponse{{ID: "codex"}}, ModelServers: []dto.ModelServerResponse{
		{ID: f.defaultID, IsDefault: true, Harnesses: []string{"codex"}, DefaultModel: "deepseek-flash", Models: []string{"deepseek-flash"}, Thinking: "high"},
		{ID: f.otherID, Harnesses: []string{"codex"}, DefaultModel: "deepseek-flash", Models: []string{"deepseek-flash", "deepseek-pro"}, Thinking: "low"},
	}}, nil
}

// recordingReviews records the retry request a follow-up approval makes.
type recordingReviews struct {
	*reviewTaskFake
	retries []domain.AgentTaskRetryRequest
}

func (f *recordingReviews) Retry(ctx context.Context, ws, id uuid.UUID, input domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	f.retries = append(f.retries, input)
	return f.reviewTaskFake.Retry(ctx, ws, id, input)
}

func insertModelServer(t *testing.T, f toolFixture, name string, isDefault bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, f.db.Get(&id, `INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,encrypted_api_key,default_model,is_default) VALUES($1,$2,'https://models.example.test','x','deepseek-flash',$3) RETURNING id`, f.ws, name, isDefault))
	return id
}

// The approver's model server runs the task, even when the default server lists
// the same model name, and a follow-up hands the chosen server to the retry.
func TestApprovalRunsTheModelServerTheApproverPicked(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, tasks := requestDelegation(t, f)
	fake := &twoServerTasks{approvalTaskFake: tasks, defaultID: insertModelServer(t, f, "Default", true), otherID: insertModelServer(t, f, "Other", false)}
	f.service.SetApprovals(NewAssistantApprovals(f.repo, fake))

	started, err := f.service.ResolveApprovalWithModel(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true, ModelChoice{Model: "deepseek-flash", ModelServerID: &fake.otherID})
	require.NoError(t, err)
	require.Equal(t, "approved", started.Status)
	var task struct {
		Model    string     `db:"model"`
		Thinking string     `db:"thinking"`
		Server   *uuid.UUID `db:"model_server_id"`
	}
	require.NoError(t, f.db.Get(&task, `SELECT model,thinking,model_server_id FROM agent_tasks WHERE id=$1`, *started.TaskID))
	require.Equal(t, "deepseek-flash", task.Model)
	require.Equal(t, "low", task.Thinking, "the thinking level of the server that runs the model")
	require.Equal(t, &fake.otherID, task.Server)

	// A follow-up shows the task's model and server and passes the picked server on.
	publishTask(t, f, *started.TaskID)
	work := repository.NewAgentTaskRepository(f.db)
	reviews := &recordingReviews{reviewTaskFake: &reviewTaskFake{approvalTaskFake: tasks}}
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetMerging(work, repository.NewWorkspaceRepository(f.db), &mergerFake{})
	f.service.approvals.SetReviews(reviews)
	b := f.attempt(t, "Follow up")
	provider, _ := scriptedProvider(t, toolCallStream("follow_up_task", `{"identifier":"ENG-1","instructions":"Fix it"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, b, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Follow up"}}, func(string) error { return nil }))
	h, err := f.repo.History(f.ctx, f.ws, b.Conversation.ID, 0)
	require.NoError(t, err)
	var followUp *domain.ConversationApproval
	for i := range h.Approvals {
		if h.Approvals[i].Action == "follow_up_task" {
			followUp = &h.Approvals[i]
		}
	}
	require.NotNil(t, followUp)
	var payload domain.FollowUpPayload
	require.NoError(t, json.Unmarshal(followUp.Payload, &payload))
	require.Equal(t, "deepseek-flash", payload.CurrentModel)
	require.Equal(t, &fake.otherID, payload.ModelServerID, "the card shows the server the task runs on")
	result, err := f.service.ResolveApprovalWithModel(f.ctx, f.ws, b.Conversation.ID, followUp.ID, true, ModelChoice{Model: "deepseek-flash", ModelServerID: &fake.defaultID})
	require.NoError(t, err)
	require.Equal(t, "approved", result.Status)
	require.Len(t, reviews.retries, 1)
	require.Equal(t, "deepseek-flash", reviews.retries[0].Model)
	require.Equal(t, &fake.defaultID, reviews.retries[0].RequestedModelServerID)
}
