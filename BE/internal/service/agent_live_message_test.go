package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestLiveMessagesInThePromptNameTheirSenderAndAreFenced(t *testing.T) {
	require.Equal(t, "Implement", withLiveMessages("Implement", nil))
	sent := time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
	one, two := int64(1), int64(2)
	prompt := withLiveMessages("Implement", []domain.AgentLiveMessage{
		{Seq: &one, Sender: domain.AgentLiveSenderUser, Body: "Also add tests\n~~~\nmore", CreatedAt: sent},
		{Seq: &two, Sender: domain.AgentLiveSenderOrchestrator, Body: "Keep the v1 route", CreatedAt: sent},
	})
	require.True(t, strings.HasPrefix(prompt, "Implement\n\n"))
	require.Contains(t, prompt, "Messages from the person who owns this task")
	require.Contains(t, prompt, "From the task's owner (2026-10-29 09:30 UTC):\n~~~text\nAlso add tests\n~ ~ ~\nmore\n~~~")
	require.Contains(t, prompt, "From the Kuayle orchestrator for the task's owner (2026-10-29 09:30 UTC):")
	require.Less(t, strings.Index(prompt, "Also add tests"), strings.Index(prompt, "Keep the v1 route"))

	line := withLiveChannel("Implement")
	require.Contains(t, line, domain.AgentLiveInboxPath)
	require.Contains(t, line, `"to":"orchestrator"`)
	require.Contains(t, line, `"kind":"question"`)
	require.Contains(t, line, "kuayle-live-channel", "agents without the hook are pointed at the skill that tells them to poll")
	require.Equal(t, strings.Repeat("x", 31400), withLiveChannel(strings.Repeat("x", 31400)), "a full prompt keeps its room")
}

type liveSourceFake struct {
	messages []domain.AgentLiveMessage
	tasks    []uuid.UUID
}

func (f *liveSourceFake) ClaimLiveMessages(_ context.Context, task domain.AgentTask, room int) ([]domain.AgentLiveMessage, error) {
	f.tasks = append(f.tasks, task.ID)
	if room <= 0 {
		return nil, nil
	}
	return f.messages, nil
}

func TestAttemptsReceiveLiveMessagesQueuedWhileNoAttemptRan(t *testing.T) {
	ws, machine, user, checkout := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &taskPolicyStore{devMachineStoreFake: &devMachineStoreFake{
		policy:        testPolicy(ws),
		machine:       &domain.DevMachine{ID: machine, WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		agentProvider: &domain.DevMachineAgentProvider{ProviderID: "opencode", Enabled: true, RequiredSecrets: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		checkouts:     []domain.DevMachineCheckout{{ID: checkout, WorkspaceID: ws, MachineID: machine, Status: "ready", WorkspacePath: "/workspace/tasks/check", RepositoryFullName: "example/test", BaseBranch: "main", WorkingBranch: "task/check"}},
	}}
	store.policy.AllowedProviders = json.RawMessage(`["opencode"]`)
	machines := newTestDevMachineService(store)
	machines.agents = agent.NewRegistry(agent.NewOpenCodeProvider(""))
	seq := int64(1)
	live := &liveSourceFake{messages: []domain.AgentLiveMessage{{Seq: &seq, Sender: domain.AgentLiveSenderUser, Body: "Also add tests", CreatedAt: time.Now()}}}
	runtime := &devMachineTaskRuntime{machines: machines, live: live}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, MachineID: &machine, CheckoutID: checkout, RunID: uuid.New(), State: "preparing", Provider: "opencode", Prompt: "Implement", Kind: domain.AgentTaskKindImplement}
	run, err := runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Contains(t, run.Prompt, "From the task's owner")
	require.Contains(t, run.Prompt, "Also add tests")
	require.Contains(t, run.Prompt, domain.AgentLiveInboxPath, "every implementing attempt learns about the live channel")
	require.Equal(t, []uuid.UUID{task.ID}, live.tasks)

	review := task
	review.ID, review.RunID, review.Kind = uuid.New(), uuid.New(), domain.AgentTaskKindReview
	run, err = runtime.Attempt(context.Background(), review)
	require.NoError(t, err)
	require.NotContains(t, run.Prompt, "Also add tests", "reviews receive no live messages")
	require.Len(t, live.tasks, 1)
}
