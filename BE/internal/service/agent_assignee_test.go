package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/stretchr/testify/require"
)

// assigneeStoreFake is an in-memory agentAssigneeStore for one issue.
type assigneeStoreFake struct {
	setErr      error
	added       []repository.AgentAssignment
	removed     []repository.AgentAssignment
	assignable  map[uuid.UUID]domain.AgentProfile
	mentionable []domain.AgentProfile
	open        map[uuid.UUID]*domain.AgentTask
	pending     []repository.AgentAssignment
	expired     []repository.AgentAssignment
	// gone makes Finish find no assignment, as after an unassignment.
	gone     bool
	finished map[uuid.UUID]string
	started  map[uuid.UUID]uuid.UUID
}

func (f *assigneeStoreFake) Identities(context.Context, uuid.UUID) ([]domain.AgentIdentity, error) {
	return []domain.AgentIdentity{{ID: uuid.New(), Name: "Fixer", Harness: "codex"}, {ID: uuid.New(), Name: "Writer", Harness: "goose"}}, nil
}
func (f *assigneeStoreFake) UpdateIdentity(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *string, *[]uuid.UUID) error {
	return nil
}
func (f *assigneeStoreFake) ForIssues(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.IssueAgentAssignee, error) {
	return map[uuid.UUID][]domain.IssueAgentAssignee{}, nil
}
func (f *assigneeStoreFake) Set(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []uuid.UUID) ([]repository.AgentAssignment, []repository.AgentAssignment, error) {
	return f.added, f.removed, f.setErr
}
func (f *assigneeStoreFake) Assignable(_ context.Context, _, _, profile uuid.UUID) (*domain.AgentProfile, error) {
	if p, ok := f.assignable[profile]; ok {
		return &p, nil
	}
	return nil, repository.ErrAgentNotAssignable
}
func (f *assigneeStoreFake) Pending(context.Context, int) ([]repository.AgentAssignment, error) {
	return f.pending, nil
}
func (f *assigneeStoreFake) ExpirePending(context.Context) ([]repository.AgentAssignment, error) {
	return f.expired, nil
}
func (f *assigneeStoreFake) Finish(_ context.Context, a repository.AgentAssignment, task *uuid.UUID, failure string) (bool, error) {
	if f.gone {
		return false, nil
	}
	if task != nil {
		f.started[a.ProfileID] = *task
	} else {
		f.finished[a.ProfileID] = failure
	}
	return true, nil
}
func (f *assigneeStoreFake) MentionedAgents(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []uuid.UUID) ([]domain.AgentProfile, error) {
	return f.mentionable, nil
}
func (f *assigneeStoreFake) OpenAgentTask(_ context.Context, _, _, profile uuid.UUID) (*domain.AgentTask, error) {
	return f.open[profile], nil
}
func (f *assigneeStoreFake) AgentNames(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]string, error) {
	return nil, nil
}

type assigneeRuntimeFake struct {
	chatErr   error
	createErr error
	actors    []uuid.UUID
	requests  []domain.AgentTaskRequest
	cancelled []uuid.UUID
	messages  []string
	events    []realtime.Event
}

func (f *assigneeRuntimeFake) OpenIssue(ctx context.Context, _, _ uuid.UUID) (*domain.Conversation, error) {
	access, _ := domain.AccessFromContext(ctx)
	f.actors = append(f.actors, access.UserID)
	if f.chatErr != nil {
		return nil, f.chatErr
	}
	return &domain.Conversation{ID: uuid.New()}, nil
}
func (f *assigneeRuntimeFake) Create(_ context.Context, _, _ uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.requests = append(f.requests, input)
	return &domain.AgentTask{ID: uuid.New(), State: "queued"}, nil
}
func (f *assigneeRuntimeFake) CancelUnassigned(_ context.Context, _, id, _, _, _ uuid.UUID) (bool, error) {
	f.cancelled = append(f.cancelled, id)
	return true, nil
}
func (f *assigneeRuntimeFake) QueueLiveMessage(_ context.Context, _, _ uuid.UUID, _, body string) (*domain.AgentLiveMessage, error) {
	f.messages = append(f.messages, body)
	return &domain.AgentLiveMessage{}, nil
}
func (f *assigneeRuntimeFake) Broadcast(_ uuid.UUID, event realtime.Event) {
	f.events = append(f.events, event)
}

func newAssigneeFakes() (*AgentAssigneeService, *assigneeStoreFake, *assigneeRuntimeFake) {
	store := &assigneeStoreFake{assignable: map[uuid.UUID]domain.AgentProfile{}, open: map[uuid.UUID]*domain.AgentTask{}, finished: map[uuid.UUID]string{}, started: map[uuid.UUID]uuid.UUID{}}
	runtime := &assigneeRuntimeFake{}
	return NewAgentAssigneeService(store, runtime, runtime, runtime, runtime, runtime), store, runtime
}

func assigneeProfile(store *assigneeStoreFake, ws uuid.UUID) domain.AgentProfile {
	profile := domain.AgentProfile{ID: uuid.New(), WorkspaceID: ws, Name: "Fixer", Role: "Implementer", Behaviour: domain.AgentBehaviourChangesCode, Harness: "goose", Model: "m1", Effort: "high", Instructions: "Always add tests."}
	store.assignable[profile.ID] = profile
	return profile
}

func TestAssigningAnAgentStartsItsTaskAsTheAssigningMember(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, issue, user := uuid.New(), uuid.New(), uuid.New()
	profile := assigneeProfile(store, ws)
	request := uuid.New()
	store.added = []repository.AgentAssignment{{IssueID: issue, ProfileID: profile.ID, WorkspaceID: ws, AssignedBy: &user, RequestID: request}}
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})

	_, err := svc.SetIssueAgents(ctx, ws, issue, []uuid.UUID{profile.ID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{user}, runtime.actors, "the task runs with the assigner's permissions")
	require.Len(t, runtime.requests, 1)
	require.Equal(t, request, runtime.requests[0].RequestID, "the assignment's request ID makes the start idempotent")
	require.Equal(t, profile.ID, runtime.requests[0].AgentProfile.ID)
	require.Contains(t, store.started, profile.ID)
	require.Len(t, runtime.events, 1)
	require.Equal(t, "issue.agents_updated", runtime.events[0].Type)
}

func TestAssignmentRefusalsChangeNothingAndStartNoTask(t *testing.T) {
	for _, refusal := range []error{repository.ErrAgentAssignForbidden, repository.ErrAgentNotAssignable} {
		svc, store, runtime := newAssigneeFakes()
		ws, user := uuid.New(), uuid.New()
		store.setErr = refusal
		_, err := svc.SetIssueAgents(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user}), ws, uuid.New(), []uuid.UUID{uuid.New()})
		require.ErrorIs(t, err, refusal)
		require.Empty(t, runtime.requests)
		require.Empty(t, runtime.events)
	}
	// A caller without workspace access is refused before any write.
	svc, _, _ := newAssigneeFakes()
	_, err := svc.SetIssueAgents(context.Background(), uuid.New(), uuid.New(), nil)
	require.ErrorIs(t, err, repository.ErrAgentAssignForbidden)
}

func TestAStartThatCanNeverSucceedIsRecordedAndATransientOneIsLeftForTheWorker(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, issue, user := uuid.New(), uuid.New(), uuid.New()
	profile := assigneeProfile(store, ws)
	assignment := repository.AgentAssignment{IssueID: issue, ProfileID: profile.ID, WorkspaceID: ws, AssignedBy: &user, RequestID: uuid.New()}

	runtime.createErr = errors.New("database unavailable")
	svc.startAssignment(context.Background(), assignment)
	require.Empty(t, store.finished, "a transient failure is retried")
	require.Empty(t, store.started)

	runtime.createErr = ErrDevMachinesDisabled
	svc.startAssignment(context.Background(), assignment)
	require.Equal(t, "Coding agents are not enabled for this workspace", store.finished[profile.ID])

	// The assigner lost access to the issue (or became a guest): opening
	// their issue chat fails, so the agent gets nothing they do not have.
	delete(store.finished, profile.ID)
	runtime.createErr, runtime.chatErr = nil, repository.ErrConversationNotFound
	svc.startAssignment(context.Background(), assignment)
	require.Equal(t, "The issue is no longer available to you", store.finished[profile.ID])

	// The team policy was switched off after the assignment was made.
	delete(store.finished, profile.ID)
	delete(store.assignable, profile.ID)
	runtime.chatErr = nil
	svc.startAssignment(context.Background(), assignment)
	require.Equal(t, "The agent can no longer be assigned in this team", store.finished[profile.ID])
	require.Empty(t, runtime.requests)
}

func TestTheWorkerResumesInterruptedStarts(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, user := uuid.New(), uuid.New()
	profile := assigneeProfile(store, ws)
	store.pending = []repository.AgentAssignment{{IssueID: uuid.New(), ProfileID: profile.ID, WorkspaceID: ws, AssignedBy: &user, RequestID: uuid.New()}}
	svc.resume(context.Background())
	require.Len(t, runtime.requests, 1)
	require.Contains(t, store.started, profile.ID)
	require.Len(t, runtime.events, 1, "a start that changed the assignment is announced")

	// A start that fails transiently changes nothing, so nothing is announced
	// on every tick while it is retried.
	runtime.createErr = errors.New("database unavailable")
	svc.resume(context.Background())
	require.Len(t, runtime.events, 1)

	// Starts that never succeeded within the window are settled with an error
	// by the store; each is announced once.
	store.pending = nil
	store.expired = []repository.AgentAssignment{{IssueID: uuid.New(), ProfileID: profile.ID, WorkspaceID: ws}}
	svc.resume(context.Background())
	require.Len(t, runtime.events, 2)
}

// The agent is unassigned after its task was created but before the
// assignment recorded it: unassigning saw no task to cancel, so the start
// cancels the task it just created instead of leaving it running unassigned.
func TestATaskCreatedForAnAgentUnassignedMeanwhileIsCancelled(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, user := uuid.New(), uuid.New()
	profile := assigneeProfile(store, ws)
	store.gone = true
	changed := svc.startAssignment(context.Background(), repository.AgentAssignment{IssueID: uuid.New(), ProfileID: profile.ID, WorkspaceID: ws, AssignedBy: &user, RequestID: uuid.New()})
	require.False(t, changed)
	require.Len(t, runtime.requests, 1)
	require.Len(t, runtime.cancelled, 1, "the orphaned task is cancelled")
	require.Empty(t, store.started)
}

func TestUnassigningAnAgentCancelsItsTask(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, issue, user, task := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store.removed = []repository.AgentAssignment{{IssueID: issue, ProfileID: uuid.New(), WorkspaceID: ws, TaskID: &task}, {IssueID: issue, ProfileID: uuid.New(), WorkspaceID: ws}}
	_, err := svc.SetIssueAgents(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user}), ws, issue, nil)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{task}, runtime.cancelled, "an assignment that never started a task has nothing to cancel")
}

func mentionComment(author uuid.UUID, ids ...uuid.UUID) domain.Comment {
	body := "<p>"
	for _, id := range ids {
		body += `<span data-type="mention" data-kind="agent" data-id="` + id.String() + `">@agent</span> `
	}
	return domain.Comment{ID: uuid.New(), UserID: author, Body: body + "why does the import fail ~~~ on Windows?</p>"}
}

func TestMentioningAnAgentWithoutATaskStartsOneForTheCommenter(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, issue, user := uuid.New(), uuid.New(), uuid.New()
	profile := assigneeProfile(store, ws)
	store.mentionable = []domain.AgentProfile{profile}
	comment := mentionComment(user, profile.ID)

	require.NoError(t, svc.HandleComment(context.Background(), ws, issue, comment))
	require.NoError(t, svc.HandleComment(context.Background(), ws, issue, comment))
	require.Equal(t, []uuid.UUID{user, user}, runtime.actors)
	require.Len(t, runtime.requests, 2)
	require.Equal(t, runtime.requests[0].RequestID, runtime.requests[1].RequestID, "the same comment asks for the same task")
	require.Contains(t, runtime.requests[0].Prompt, "why does the import fail ~ ~ ~ on Windows?", "the comment cannot close its fence")
	require.Equal(t, profile.ID, runtime.requests[0].AgentProfile.ID)
}

func TestMentioningAnAgentReachesOnlyTheCommentersOwnOpenTask(t *testing.T) {
	svc, store, runtime := newAssigneeFakes()
	ws, issue, user, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	profile := assigneeProfile(store, ws)
	store.mentionable = []domain.AgentProfile{profile}

	store.open[profile.ID] = &domain.AgentTask{ID: uuid.New(), CreatorID: user, State: "running"}
	require.NoError(t, svc.HandleComment(context.Background(), ws, issue, mentionComment(user, profile.ID)))
	require.Equal(t, []string{"@agent why does the import fail ~~~ on Windows?"}, runtime.messages)

	// Another member's task carries that member's permissions.
	store.open[profile.ID] = &domain.AgentTask{ID: uuid.New(), CreatorID: other, State: "running"}
	require.NoError(t, svc.HandleComment(context.Background(), ws, issue, mentionComment(user, profile.ID)))
	// A task awaiting review is steered by the comment follow-up instead.
	store.open[profile.ID] = &domain.AgentTask{ID: uuid.New(), CreatorID: user, State: "awaiting_review"}
	require.NoError(t, svc.HandleComment(context.Background(), ws, issue, mentionComment(user, profile.ID)))
	require.Len(t, runtime.messages, 1)
	require.Empty(t, runtime.requests)
}

// The editor marks an agent chip with kind "agent". It must survive the
// comment sanitizer and be read as an agent mention, never as a person's;
// chips of other kinds (people, issues, wiki pages) never reach an agent.
func TestAgentMentionChipsAreToldApartFromPeopleIssuesAndWikiPages(t *testing.T) {
	agent, person, issue, page := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	chip := func(kind string, id uuid.UUID) string {
		return `<span data-type="mention" data-id="` + id.String() + `" data-label="x" data-kind="` + kind + `" class="mention">@x</span>`
	}
	body := sanitize.SanitizeEditorContent("<p>" + chip("agent", agent) + chip("user", person) + chip("issue", issue) + chip("wiki", page) + "</p>")
	require.Equal(t, []uuid.UUID{agent}, extractMentionedAgentIDs(body))
	require.Equal(t, []uuid.UUID{person}, extractMentionedUserIDs(body))
	legacy := `<span data-type="mention" data-id="` + person.String() + `">@x</span>`
	require.Equal(t, []uuid.UUID{person}, extractMentionedUserIDs(legacy), "a chip without a kind is a person")
	require.Empty(t, extractMentionedAgentIDs(legacy))
}

func TestCommentsThatMentionNoAssignableAgentDoNothing(t *testing.T) {
	svc, _, runtime := newAssigneeFakes()
	require.NoError(t, svc.HandleComment(context.Background(), uuid.New(), uuid.New(), domain.Comment{ID: uuid.New(), UserID: uuid.New(), Body: "<p>plain</p>"}))
	// A mention of a person, a guest's mention, or an agent not enabled for
	// the team: the store returns no agents.
	require.NoError(t, svc.HandleComment(context.Background(), uuid.New(), uuid.New(), mentionComment(uuid.New(), uuid.New())))
	require.Empty(t, runtime.requests)
	require.Empty(t, runtime.messages)
	require.Empty(t, runtime.events)
}

func TestAgentListingsReportTheMachinePolicy(t *testing.T) {
	svc, _, _ := newAssigneeFakes()
	svc.SetMachinePolicy(func(_ context.Context, _ uuid.UUID, profile domain.AgentProfile) error {
		if profile.Harness == "goose" {
			return ErrProviderNotAllowed
		}
		return nil
	})
	agents, err := svc.Identities(context.Background(), uuid.New())
	require.NoError(t, err)
	require.True(t, agents[0].Available)
	require.False(t, agents[1].Available)
	require.Equal(t, "No allowed coding agent is available", agents[1].Unavailable)
}

func TestAgentIdentityInputIsValidatedBeforeAnyWrite(t *testing.T) {
	svc, _, _ := newAssigneeFakes()
	ws := uuid.New()
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: uuid.New()})
	bad, good := "red", "#12abEF"
	require.ErrorIs(t, svc.UpdateIdentity(ctx, ws, uuid.New(), AgentIdentityInput{AvatarColor: &bad}), repository.ErrAgentIdentityInvalid)
	require.NoError(t, svc.UpdateIdentity(ctx, ws, uuid.New(), AgentIdentityInput{AvatarColor: &good}))
	require.ErrorIs(t, svc.UpdateIdentity(context.Background(), ws, uuid.New(), AgentIdentityInput{}), repository.ErrAgentIdentityDenied)
}

func TestATaskRunAsAnAgentTakesItsHarnessModelAndInstructionsFromTheProfile(t *testing.T) {
	workflow := "some-workflow"
	server := uuid.New()
	profile := domain.AgentProfile{ID: uuid.New(), Name: "Fixer", Role: "Implementer", Behaviour: domain.AgentBehaviourChangesCode, Harness: "goose", Model: "m1", Effort: "high", Instructions: "Always add tests."}
	input := domain.AgentTaskRequest{Prompt: "p", Provider: "codex", Model: "other", ModelServerID: &server, WorkflowID: &workflow, AgentProfile: &profile}
	applyAgentProfile(&input)
	require.Equal(t, "goose", input.Provider)
	require.Equal(t, "m1", input.Model)
	require.Equal(t, "high", input.Thinking)
	require.Nil(t, input.ModelServerID)
	require.Nil(t, input.WorkflowID, "an assigned agent follows the built-in flow, not the issue's default workflow")
	require.Contains(t, input.WorkflowInstructions, "Always add tests.")
	require.Contains(t, input.WorkflowInstructions, `Agent profile "Fixer"`)

	plain := domain.AgentTaskRequest{Prompt: "p", Provider: "codex", WorkflowID: &workflow}
	applyAgentProfile(&plain)
	require.Equal(t, "codex", plain.Provider, "delegation without an agent is unchanged")
	require.Equal(t, &workflow, plain.WorkflowID)
}

func TestOnlyWorkingTasksAreStoppedByUnassignment(t *testing.T) {
	for _, state := range []string{"queued", "preparing", "running", "waiting_input", "validating", "publishing", "blocked", "failed"} {
		require.True(t, domain.AgentTaskWorking(state), state)
		require.True(t, domain.ValidAgentTaskTransition(state, "cancelling"), state)
	}
	for _, state := range []string{"awaiting_review", "completed", "cancelled", "cancelling"} {
		require.False(t, domain.AgentTaskWorking(state), state)
	}
}
