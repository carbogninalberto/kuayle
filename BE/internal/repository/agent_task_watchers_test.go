package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type taskWatcherFixture struct {
	db           *sqlx.DB
	tasks        *AgentTaskRepository
	ctx          context.Context
	workspace    uuid.UUID
	user         uuid.UUID
	stranger     uuid.UUID
	team         uuid.UUID
	issue        uuid.UUID
	conversation uuid.UUID
	message      uuid.UUID
	task         uuid.UUID
}

func newTaskWatcherFixture(t *testing.T) taskWatcherFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	f := taskWatcherFixture{
		db: db, tasks: NewAgentTaskRepository(db), ctx: context.Background(),
		workspace: uuid.New(), user: uuid.New(), stranger: uuid.New(), team: uuid.New(), issue: uuid.New(), task: uuid.New(),
	}
	suffix := strings.ReplaceAll(f.workspace.String(), "-", "")
	exec := func(query string, args ...any) {
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Watcher','test'),($3,$4,'Stranger','test')`, f.user, suffix+"-watcher@example.test", f.stranger, suffix+"-stranger@example.test")
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Watcher',$2,$3)`, f.workspace, "watcher-"+suffix, f.user)
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, f.workspace, f.user, f.stranger)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Watcher','WAT')`, f.team, f.workspace)
	status := uuid.New()
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, f.team)
	exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id,created_at) VALUES($1,$2,$3,1,'WAT-1','Fix login',$4,$5,now())`, f.issue, f.workspace, f.team, f.user, status)
	require.NoError(t, db.Get(&f.conversation, `INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Fix login chat') RETURNING id`, f.workspace, f.user))
	require.NoError(t, db.Get(&f.message, `INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,$2,'user','Ping me when it finishes','completed') RETURNING id`, f.conversation, uuid.New()))
	exec(`INSERT INTO agent_tasks(id,workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state) VALUES($1,$2,$3,$4,$5,$6,$7,'Implement','codex','running')`, f.task, f.workspace, f.conversation, f.user, f.issue, f.team, uuid.New())
	return f
}

// transition records a transition row the way recordAgentTaskTransition does,
// without driving the state machine.
func (f taskWatcherFixture) transition(t *testing.T, to string) {
	t.Helper()
	_, err := f.db.Exec(`INSERT INTO agent_task_transitions(task_id,from_state,to_state,reason) VALUES($1,'running',$2,'Fixture')`, f.task, to)
	require.NoError(t, err)
}

func (f taskWatcherFixture) watches(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_task_watchers WHERE task_id=$1`, f.task))
	return count
}

// KUA-165: a watch fires exactly once, on the first matching transition after
// registration; earlier transitions and non-matching ones never fire.
func TestTaskWatchFiresOnceOnTheFirstMatchingLaterTransition(t *testing.T) {
	f := newTaskWatcherFixture(t)
	f.transition(t, "validating")
	require.NoError(t, f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, ""))
	require.Equal(t, 1, f.watches(t))

	f.transition(t, "publishing")
	f.transition(t, "awaiting_review")
	watches, err := f.tasks.ClaimTaskWatchers(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, watches, "an unnamed until fires only on an end state")

	f.transition(t, "completed")
	watches, err = f.tasks.ClaimTaskWatchers(f.ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []domain.AgentTaskWatch{{
		TaskID: f.task, WorkspaceID: f.workspace, UserID: f.user, ConversationID: f.conversation, ParentMessageID: f.message,
		Until: "", State: "completed", IssueID: f.issue, IssueIdentifier: "WAT-1", IssueTitle: "Fix login",
	}}, watches)

	// The claimed row is gone, so the ping cannot repeat.
	watches, err = f.tasks.ClaimTaskWatchers(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, watches)
	require.Zero(t, f.watches(t))
}

// A named until fires on that state only, and a cancelled task counts as an
// end state.
func TestTaskWatchFiresOnTheNamedStateAndOnCancelled(t *testing.T) {
	f := newTaskWatcherFixture(t)
	require.NoError(t, f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, "failed"))
	f.transition(t, "awaiting_review")
	f.transition(t, "completed")
	watches, err := f.tasks.ClaimTaskWatchers(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, watches, "completed does not satisfy a watch for failed")
	f.transition(t, "failed")
	watches, err = f.tasks.ClaimTaskWatchers(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, watches, 1)
	require.Equal(t, "failed", watches[0].State)
	require.Equal(t, "failed", watches[0].Until)

	f2 := newTaskWatcherFixture(t)
	require.NoError(t, f2.tasks.WatchTask(f2.ctx, f2.workspace, f2.user, f2.task, f2.conversation, f2.message, ""))
	f2.transition(t, "cancelled")
	watches, err = f2.tasks.ClaimTaskWatchers(f2.ctx, 10)
	require.NoError(t, err)
	require.Len(t, watches, 1, "a cancelled task finished working; its watcher is pinged")
	require.Equal(t, "cancelled", watches[0].State)
}

// A watch never fires for a state the task already reached before
// registration: registering then is refused by the tool, and even a direct
// row stays silent.
func TestTaskWatchIgnoresTransitionsBeforeRegistration(t *testing.T) {
	f := newTaskWatcherFixture(t)
	f.transition(t, "completed")
	require.NoError(t, f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, "completed"))
	watches, err := f.tasks.ClaimTaskWatchers(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, watches)
	require.Equal(t, 1, f.watches(t), "the row stays; only later transitions can satisfy it")
}

// Registering again from the same conversation replaces the watch, so a
// repeated request still pings exactly once with the latest choice.
func TestTaskWatchReregistrationReplacesTheWatch(t *testing.T) {
	f := newTaskWatcherFixture(t)
	require.NoError(t, f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, ""))
	require.NoError(t, f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, "awaiting_review"))
	require.Equal(t, 1, f.watches(t))
	var until *string
	require.NoError(t, f.db.Get(&until, `SELECT until_state FROM agent_task_watchers WHERE task_id=$1`, f.task))
	require.NotNil(t, until)
	require.Equal(t, "awaiting_review", *until)

	// Another user or conversation keeps its own watch.
	require.NoError(t, f.tasks.WatchTask(f.ctx, f.workspace, f.stranger, f.task, f.conversation, f.message, ""))
	require.Equal(t, 2, f.watches(t))
}

// A task of another workspace is never watched, whatever the caller passes.
func TestTaskWatchIsScopedToTheWorkspace(t *testing.T) {
	f := newTaskWatcherFixture(t)
	other := uuid.New()
	_, err := f.db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, other, "other-"+strings.ReplaceAll(other.String(), "-", ""), f.user)
	require.NoError(t, err)
	require.NoError(t, f.tasks.WatchTask(f.ctx, other, f.user, f.task, f.conversation, f.message, ""))
	require.Zero(t, f.watches(t))
}

// A claimed watch posts its ping as a completed assistant message, without a
// model call, and only the conversation's creator can post it.
func TestPostAssistantNoticePostsForTheConversationCreator(t *testing.T) {
	f := newTaskWatcherFixture(t)
	conversations := NewConversationRepository(f.db)
	asUser := domain.WithAccess(f.ctx, domain.Access{WorkspaceID: f.workspace, UserID: f.user})
	content := "Ping, as you asked: coding task completed on WAT-1: Fix login."

	notice, err := conversations.PostAssistantNotice(asUser, f.workspace, f.conversation, f.message, content)
	require.NoError(t, err)
	require.Equal(t, "assistant", notice.Role)
	require.Equal(t, "completed", notice.Status)
	require.Equal(t, f.message, *notice.ParentID)
	require.Equal(t, content, notice.Content)

	var stored domain.ConversationMessage
	require.NoError(t, f.db.Get(&stored, `SELECT * FROM conversation_messages WHERE id=$1`, notice.ID))
	require.Equal(t, content, stored.Content)
	var events int
	require.NoError(t, f.db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='message.created'`, f.conversation))
	require.Equal(t, 1, events, "the ping is announced like any other message")

	asStranger := domain.WithAccess(f.ctx, domain.Access{WorkspaceID: f.workspace, UserID: f.stranger})
	_, err = conversations.PostAssistantNotice(asStranger, f.workspace, f.conversation, f.message, "No")
	require.ErrorIs(t, err, ErrConversationNotFound, "only the conversation's creator is posted for")

	_, err = conversations.PostAssistantNotice(asUser, f.workspace, f.conversation, f.message, "  ")
	require.ErrorIs(t, err, ErrConversationInvalid)
	_, err = conversations.PostAssistantNotice(asUser, f.workspace, f.conversation, uuid.New(), "No")
	require.ErrorIs(t, err, ErrConversationNotFound, "the parent must be this conversation's user message")
}
