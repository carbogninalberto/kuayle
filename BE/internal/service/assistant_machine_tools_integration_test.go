package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// machineTasksFake stands in for the task repository: like it, it answers
// only the task's creator, and a start needs a machine and a preview command.
type machineTasksFake struct {
	task       *domain.AgentTask
	previewErr error
	previews   []bool
}

func (f *machineTasksFake) creator(ctx context.Context, ws uuid.UUID) error {
	access, ok := domain.AccessFromContext(ctx)
	if f.task == nil || !ok || access.UserID != f.task.CreatorID || access.WorkspaceID != ws || f.task.WorkspaceID != ws {
		return repository.ErrConversationNotFound
	}
	return nil
}

func (f *machineTasksFake) FollowUpTarget(_ context.Context, ws, issue uuid.UUID) (*repository.FollowUpTarget, error) {
	if f.task == nil || f.task.WorkspaceID != ws || f.task.IssueID != issue {
		return nil, repository.ErrNoMergeTarget
	}
	return &repository.FollowUpTarget{TaskID: f.task.ID, CreatorID: f.task.CreatorID, State: f.task.State, Generation: f.task.Generation}, nil
}

func (f *machineTasksFake) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	if err := f.creator(ctx, ws); err != nil || f.task.ID != id {
		return nil, repository.ErrConversationNotFound
	}
	copy := *f.task
	return &copy, nil
}

func (f *machineTasksFake) RequestPreview(ctx context.Context, ws, id uuid.UUID, start bool) (*domain.AgentTask, error) {
	if err := f.creator(ctx, ws); err != nil || f.task.ID != id {
		return nil, repository.ErrConversationNotFound
	}
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	if start && (f.task.MachineID == nil || len(previewCommand(*f.task)) == 0) {
		return nil, repository.ErrPreviewUnavailable
	}
	f.previews = append(f.previews, start)
	state := domain.AgentTaskPreviewStopped
	if start {
		state = domain.AgentTaskPreviewStarting
	}
	f.task.PreviewState = &state
	copy := *f.task
	return &copy, nil
}

type machineToolsFixture struct {
	toolFixture
	conversation domain.ConversationAttempt
	tasks        *machineTasksFake
	store        *portStoreFake
	machine      *domain.DevMachine
}

// newMachineToolsFixture gives ENG-1 a real coding task on a running machine
// owned by the conversation's user, with a preview command.
func newMachineToolsFixture(t *testing.T) machineToolsFixture {
	t.Helper()
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	containerID := "developer"
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: f.ws, CreatedByUserID: &f.user, Name: "eng-1-dark-mode", RoutingKey: "0123456789abcdef0123",
		Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour), ListeningPorts: pq.Int64Array{3000}}
	store := &portStoreFake{devMachineStoreFake: &devMachineStoreFake{policy: testPolicy(f.ws), machine: machine,
		services: []domain.DevMachineService{{ID: uuid.New(), MachineID: machine.ID, ServiceKey: "ide", ServiceType: "ide", Status: "running", InternalPort: 8080, ContainerID: &containerID}}}}
	machines := newTestDevMachineService(store)
	preferred := 5173
	tasks := &machineTasksFake{task: &domain.AgentTask{ID: *started.TaskID, WorkspaceID: f.ws, IssueID: delegation.IssueID, CreatorID: f.user, State: "awaiting_review",
		MachineID: &machine.ID, PreviewCommand: json.RawMessage(`["npm","run","dev"]`), PreviewPort: &preferred}}
	f.service.tools.SetMachines(tasks, machines)
	f.service.approvals.SetMachines(tasks, machines)
	return machineToolsFixture{toolFixture: f, conversation: a, tasks: tasks, store: store, machine: machine}
}

// request drives a real assistant reply that calls one approval tool.
func (f machineToolsFixture) request(t *testing.T, tool, arguments string) (domain.ConversationAttempt, domain.ConversationApproval) {
	t.Helper()
	a := f.attempt(t, "Use "+tool)
	provider, requests := scriptedProvider(t, toolCallStream(tool, arguments), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: tool}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	require.Equal(t, tool, h.Approvals[0].Action)
	return a, h.Approvals[0]
}

func (f machineToolsFixture) approve(t *testing.T, a domain.ConversationAttempt, approval domain.ConversationApproval) *domain.ConversationApproval {
	t.Helper()
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	return result
}

// Starting a preview from chat waits for approval, then queues the task's
// own preview as its creator; stopping it needs no approval.
func TestAssistantStartsPreviewsOnlyAfterApprovalAndStopsThemDirectly(t *testing.T) {
	f := newMachineToolsFixture(t)
	a, approval := f.request(t, "start_preview", `{"identifier":"eng-1"}`)
	var payload domain.PreviewPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	preferred := 5173
	require.Equal(t, domain.PreviewPayload{Identifier: "ENG-1", Title: "Dark mode", TaskID: f.tasks.task.ID, MachineID: f.machine.ID, Machine: "eng-1-dark-mode",
		Command: []string{"npm", "run", "dev"}, Port: &preferred, Audience: domain.AppPortAudienceOwner}, payload)
	require.Empty(t, f.tasks.previews, "requesting a preview starts nothing")

	result := f.approve(t, a, approval)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, f.tasks.task.ID, *result.TaskID)
	require.Equal(t, []bool{true}, f.tasks.previews)

	a, approval = f.request(t, "start_preview", `{"identifier":"ENG-1"}`)
	f.tasks.previewErr = repository.ErrPreviewUnavailable
	result = f.approve(t, a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "no preview command or no ready checkout")
	f.tasks.previewErr = errors.New("connection reset")
	_, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.Error(t, err, "a transient failure on a new approval can be retried")
	f.tasks.previewErr = nil

	output, _, err := f.service.tools.run(f.ctx, f.ws, f.user, "stop_preview", `{"identifier":"eng-1"}`)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false}, f.tasks.previews, "stopping needs no approval")
	require.Equal(t, "stopped", output.(map[string]any)["preview"].(map[string]any)["state"])

	// No machine, no preview command and another member's task are refused
	// before anything is asked of the user.
	f.tasks.task.PreviewCommand = nil
	_, _, err = f.service.tools.preparePreview(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "no preview command")
	f.tasks.task.PreviewCommand = json.RawMessage(`["npm","run","dev"]`)
	f.tasks.task.MachineID = nil
	_, _, err = f.service.tools.preparePreview(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "no machine")
	_, _, err = f.service.tools.preparePort(f.ctx, f.ws, f.user, `{"identifier":"ENG-1","port":3000}`)
	require.ErrorContains(t, err, "no machine")
	f.tasks.task.MachineID = &f.machine.ID
	f.tasks.task.CreatorID = uuid.New()
	_, _, err = f.service.tools.preparePreview(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "only the task's creator")
	_, _, err = f.service.tools.run(f.ctx, f.ws, f.user, "stop_preview", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "only the task's creator")
	_, _, err = f.service.tools.preparePreview(f.ctx, f.ws, f.user, `{"identifier":"ENG-404"}`)
	require.ErrorContains(t, err, "issue not found")
}

// Exposing a port from chat waits for approval and reuses the machine's port
// record for that port instead of adding another; listing returns its link.
func TestAssistantExposesPortsAfterApprovalAndReusesExistingOnes(t *testing.T) {
	f := newMachineToolsFixture(t)
	a, approval := f.request(t, "expose_port", `{"identifier":"eng-1","port":3000,"label":"web"}`)
	var payload domain.ExposePortPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	label := "web"
	require.Equal(t, domain.ExposePortPayload{Identifier: "ENG-1", Title: "Dark mode", TaskID: f.tasks.task.ID, MachineID: f.machine.ID, Machine: "eng-1-dark-mode",
		Port: 3000, Label: &label, Host: "0123456789abcdef0123-3000.machines.example.test", Audience: domain.AppPortAudienceOwner}, payload)
	require.Empty(t, f.store.ports, "requesting a port exposes nothing")
	result := f.approve(t, a, approval)
	require.Equal(t, "approved", result.Status)
	require.Len(t, f.store.ports, 1)
	require.Equal(t, "web", *f.store.ports[0].Label)
	require.Equal(t, f.user, f.store.ports[0].CreatedByUserID)

	a, approval = f.request(t, "expose_port", `{"identifier":"ENG-1","port":3000,"label":"api"}`)
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.True(t, payload.Existing, "the card says the port is already exposed")
	require.Equal(t, "approved", f.approve(t, a, approval).Status)
	require.Len(t, f.store.ports, 1, "the existing port is reused, not duplicated")
	require.Equal(t, "api", *f.store.ports[0].Label)

	// A port exposed between the request and the approval is reused as well.
	a, approval = f.request(t, "expose_port", `{"identifier":"ENG-1","port":4000}`)
	_, err := f.service.approvals.machines.CreatePort(f.ctx, f.ws, f.machine.ID, f.user, 4000, nil)
	require.NoError(t, err)
	require.Equal(t, "approved", f.approve(t, a, approval).Status)
	require.Len(t, f.store.ports, 2)

	output, _, err := f.service.tools.run(f.ctx, f.ws, f.user, "list_ports", `{"identifier":"eng-1"}`)
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	var listed struct {
		MachineID uuid.UUID `json:"machine_id"`
		Machine   string    `json:"machine"`
		Ports     []struct {
			Port      int    `json:"port"`
			URL       string `json:"url"`
			Listening bool   `json:"listening"`
		} `json:"ports"`
		Audience string `json:"audience"`
	}
	require.NoError(t, json.Unmarshal(data, &listed))
	require.Equal(t, f.machine.ID, listed.MachineID)
	require.Equal(t, "eng-1-dark-mode", listed.Machine)
	require.Equal(t, domain.AppPortAudienceOwner, listed.Audience)
	require.Len(t, listed.Ports, 2)
	require.Equal(t, "https://0123456789abcdef0123-3000.machines.example.test", listed.Ports[0].URL)
	require.True(t, listed.Ports[0].Listening)

	for arguments, message := range map[string]string{
		`{"identifier":"ENG-1","port":8080}`:               "belongs to the machine's",
		`{"identifier":"ENG-1","port":80}`:                 "between 1024 and 65535",
		`{"identifier":"ENG-1","port":3000,"label":"<b>"}`: "a label has at most 40",
	} {
		_, _, err = f.service.tools.preparePort(f.ctx, f.ws, f.user, arguments)
		require.ErrorContains(t, err, message, arguments)
	}

	// The gateway only admits the machine's owner, so only they may expose it.
	other := uuid.New()
	f.machine.CreatedByUserID = &other
	_, _, err = f.service.tools.preparePort(f.ctx, f.ws, f.user, `{"identifier":"ENG-1","port":5000}`)
	require.ErrorContains(t, err, "only the machine's owner")
	_, _, err = f.service.tools.run(f.ctx, f.ws, f.user, "list_ports", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "only the machine's owner")
	f.machine.CreatedByUserID = &f.user
	a, approval = f.request(t, "expose_port", `{"identifier":"ENG-1","port":5000}`)
	f.machine.CreatedByUserID = &other
	result = f.approve(t, a, approval)
	require.Equal(t, "failed", result.Status, "approval re-checks the owner")
	require.Equal(t, "Only the machine's owner can expose its ports", *result.Error)
	require.Len(t, f.store.ports, 2)
}

func TestMachineToolsNeedTheirServices(t *testing.T) {
	tools := &AssistantTools{}
	for _, spec := range tools.specs() {
		require.NotContains(t, []string{"start_preview", "stop_preview", "expose_port", "list_ports"}, spec.Function.Name)
	}
	tools.SetMachines(&machineTasksFake{}, newTestDevMachineService(&portStoreFake{devMachineStoreFake: &devMachineStoreFake{}}))
	names := []string{}
	for _, spec := range tools.specs() {
		names = append(names, spec.Function.Name)
	}
	require.Subset(t, names, []string{"start_preview", "stop_preview", "expose_port", "list_ports"})
}

// Telegram answers preview and port approvals for what they did.
func TestTelegramApprovalAnswerDescribesPreviewsAndPorts(t *testing.T) {
	answer, final := telegramApprovalAnswer(&domain.ConversationApproval{Action: "start_preview", Status: "approved"}, nil)
	require.True(t, final)
	require.Equal(t, "Approved: the preview is starting. Open it from the chat in Kuayle.", answer)
	answer, _ = telegramApprovalAnswer(&domain.ConversationApproval{Action: "expose_port", Status: "approved"}, nil)
	require.Equal(t, "Approved: the port is exposed. Open it from the chat in Kuayle.", answer)
	reason := "Only the machine's owner can expose its ports"
	answer, _ = telegramApprovalAnswer(&domain.ConversationApproval{Action: "expose_port", Status: "failed", Error: &reason}, nil)
	require.Equal(t, "The port could not be exposed. "+reason, answer)
	answer, _ = telegramApprovalAnswer(&domain.ConversationApproval{Action: "start_preview", Status: "failed", Error: &reason}, nil)
	require.Equal(t, "The preview could not be started. "+reason, answer)
}
