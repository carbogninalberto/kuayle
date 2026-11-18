package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type registeredWatch struct {
	ws, user, task, conversation, parent uuid.UUID
	until                                string
}

// taskWatchersFake records registered watches; err refuses registration.
type taskWatchersFake struct {
	registered []registeredWatch
	err        error
}

func (f *taskWatchersFake) WatchTask(_ context.Context, ws, user, task, conversation, parent uuid.UUID, until string) error {
	if f.err != nil {
		return f.err
	}
	f.registered = append(f.registered, registeredWatch{ws, user, task, conversation, parent, until})
	return nil
}

type watchFixture struct {
	tools    *AssistantTools
	watchers *taskWatchersFake
	ws       uuid.UUID
	creator  uuid.UUID
	guest    uuid.UUID
	issue    *domain.Issue
	task     uuid.UUID
	attempt  domain.ConversationAttempt
}

func newWatchFixture() *watchFixture {
	f := &watchFixture{ws: uuid.New(), creator: uuid.New(), guest: uuid.New()}
	f.issue = &domain.Issue{ID: uuid.New(), Identifier: "ENG-1", Title: "Dark mode"}
	f.task = uuid.New()
	members := prMembers{f.creator: domain.RoleMember, f.guest: domain.RoleGuest}
	f.tools = NewAssistantTools(prIssues{issue: f.issue}, nil, members, nil)
	f.watchers = &taskWatchersFake{}
	f.tools.SetAgentWork(prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "running"}})
	f.tools.SetTaskWatchers(f.watchers)
	parent := uuid.New()
	f.attempt = domain.ConversationAttempt{
		Conversation: domain.Conversation{ID: uuid.New(), WorkspaceID: f.ws, CreatorID: f.creator},
		Message:      domain.ConversationMessage{ParentID: &parent},
	}
	return f
}

func (f *watchFixture) watch(t *testing.T, arguments string) (map[string]any, error) {
	t.Helper()
	result, _, err := f.tools.watchAgentWork(context.Background(), f.attempt, arguments)
	if err != nil {
		return nil, err
	}
	return result.(map[string]any), nil
}

// KUA-165: "ping me when KUA-X finishes" registers a one-shot watch bound to
// the conversation and the turn that asked, without an approval.
func TestWatchAgentWorkRegistersAOneShotWatch(t *testing.T) {
	f := newWatchFixture()
	result, err := f.watch(t, `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, true, result["watching"])
	require.Equal(t, "ENG-1", result["identifier"])
	require.Equal(t, "running", result["state"])
	require.Equal(t, "any end state", result["until"], "an omitted until means any end state")

	for _, tc := range []struct {
		arguments string
		until     string
	}{
		{`{"identifier":"ENG-1"}`, ""},
		{`{"identifier":"eng-1","until":"completed"}`, "completed"},
		{`{"identifier":"ENG-1","until":"awaiting_review"}`, "awaiting_review"},
		{`{"identifier":"ENG-1","until":"failed"}`, "failed"},
	} {
		result, err = f.watch(t, tc.arguments)
		require.NoError(t, err, tc.arguments)
		require.Equal(t, true, result["watching"], tc.arguments)
		require.Equal(t, "running", result["state"], tc.arguments)
		registered := f.watchers.registered[len(f.watchers.registered)-1]
		require.Equal(t, f.ws, registered.ws, tc.arguments)
		require.Equal(t, f.creator, registered.user, tc.arguments)
		require.Equal(t, f.task, registered.task, tc.arguments)
		require.Equal(t, f.attempt.Conversation.ID, registered.conversation, tc.arguments)
		require.Equal(t, *f.attempt.Message.ParentID, registered.parent, tc.arguments)
		require.Equal(t, tc.until, registered.until, tc.arguments)
		if tc.until == "" {
			require.Equal(t, "any end state", result["until"], tc.arguments)
		} else {
			require.Equal(t, tc.until, result["until"], tc.arguments)
		}
	}
}

func TestWatchAgentWorkRefusesWhatItCannotWatch(t *testing.T) {
	f := newWatchFixture()
	f.attempt.Conversation.CreatorID = f.guest
	_, err := f.watch(t, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "you no longer have access to this workspace")
	f.attempt.Conversation.CreatorID = f.creator

	for _, tc := range []struct{ arguments, message string }{
		{`{"identifier":"ENG-1","until":"cancelled"}`, "until must be awaiting_review"},
		{`{"identifier":"MISSING"}`, "issue not found"},
		{`{"identifier":"ENG-1","unknown":true}`, "invalid arguments"},
	} {
		_, err := f.watch(t, tc.arguments)
		require.ErrorContains(t, err, tc.message, tc.arguments)
	}
	require.Empty(t, f.watchers.registered)

	f.tools.work = prWork{}
	_, err = f.watch(t, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "this issue has no coding task")

	f.tools.watchers = nil
	_, err = f.watch(t, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "unknown tool")
}

// The watch never fires for a state the task already reached: the call reports
// the current state so the assistant answers instead of registering.
func TestWatchAgentWorkReportsAnAlreadyReachedState(t *testing.T) {
	f := newWatchFixture()
	for _, tc := range []struct {
		state string
		until string
	}{
		{"awaiting_review", "awaiting_review"},
		{"completed", "completed"},
		{"completed", ""},
		{"failed", ""},
		{"cancelled", ""},
	} {
		f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: tc.state}}
		arguments := `{"identifier":"ENG-1"}`
		if tc.until != "" {
			arguments = `{"identifier":"ENG-1","until":"` + tc.until + `"}`
		}
		_, err := f.watch(t, arguments)
		require.ErrorContains(t, err, "already "+tc.state, tc.until)
		require.Empty(t, f.watchers.registered, tc.until)
	}
	// A task that finished can still be retried, so watching it stay alight
	// for a later state remains possible.
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "failed"}}
	result, err := f.watch(t, `{"identifier":"ENG-1","until":"completed"}`)
	require.NoError(t, err)
	require.Equal(t, true, result["watching"])
	require.Len(t, f.watchers.registered, 1)
}

func TestWatchAgentWorkNeedsItsStoresWired(t *testing.T) {
	f := newWatchFixture()
	tool, ok := lookupAssistantTool("watch_agent_work")
	require.True(t, ok)
	require.True(t, tool.agentWork && tool.watch, "it needs both agent work and the watcher store")
	require.False(t, tool.approval, "registering a watch needs no approval")
	require.False(t, tool.write, "it changes no issue")
	require.True(t, f.tools.available(tool))
	f.tools.watchers = nil
	require.False(t, f.tools.available(tool), "offered only once SetTaskWatchers wired it")
	f.tools.work = nil
	require.False(t, (&AssistantTools{}).available(assistantTool{agentWork: true, watch: true}), "offered only once agent work is wired")

	// A store failure is reported as a generic tool failure to the model.
	f2 := newWatchFixture()
	f2.watchers.err = errors.New("database unavailable")
	_, err := f2.watch(t, `{"identifier":"ENG-1"}`)
	require.Error(t, err)
	require.Equal(t, "the tool failed", toolErrorText(err))
}
