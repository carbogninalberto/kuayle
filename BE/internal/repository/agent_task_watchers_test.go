package repository

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/assert"
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

// transition moves the task through states as the task paths do, so watches
// are marked due by the transition itself; actor is who made the change.
func (f taskWatcherFixture) transition(t *testing.T, actor *uuid.UUID, states ...string) {
	t.Helper()
	for _, to := range states {
		tx, err := f.db.BeginTxx(f.ctx, nil)
		require.NoError(t, err)
		var task domain.AgentTask
		require.NoError(t, tx.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1 FOR UPDATE`, f.task))
		require.NoError(t, transitionAgentTask(f.ctx, tx, &task, to, "Fixture: "+to, actor), to)
		require.NoError(t, tx.Commit())
	}
}

// watch registers a watch of user from a new conversation of theirs.
func (f taskWatcherFixture) watch(t *testing.T, user uuid.UUID, until string) (uuid.UUID, string, bool) {
	t.Helper()
	var conversation uuid.UUID
	require.NoError(t, f.db.Get(&conversation, `INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Another chat') RETURNING id`, f.workspace, user))
	state, watching, err := f.tasks.WatchTask(f.ctx, f.workspace, user, f.task, conversation, pingRequest(t, f.db, conversation), until)
	require.NoError(t, err)
	return conversation, state, watching
}

func (f taskWatcherFixture) watches(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_task_watchers WHERE task_id=$1`, f.task))
	return count
}

// KUA-165: a watch fires exactly once, on the first matching transition after
// registration; non-matching ones never fire.
func TestTaskWatchFiresOnceOnTheFirstMatchingLaterTransition(t *testing.T) {
	f := newTaskWatcherFixture(t)
	state, watching, err := f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, "")
	require.NoError(t, err)
	require.True(t, watching)
	require.Equal(t, "running", state)
	require.Equal(t, 1, f.watches(t))

	f.transition(t, nil, "validating", "publishing", "awaiting_review")
	require.Empty(t, claimTaskWatches(t, f.tasks, f.ctx, f.task), "an unnamed until fires only on an end state")

	f.transition(t, nil, "completed")
	require.Equal(t, []domain.AgentTaskWatch{{
		TaskID: f.task, WorkspaceID: f.workspace, UserID: f.user, ConversationID: f.conversation, ParentMessageID: f.message,
		Until: "", State: "completed", Notified: true, Kind: domain.AgentTaskKindImplement, IssueID: f.issue, IssueIdentifier: "WAT-1", IssueTitle: "Fix login",
	}}, claimTaskWatches(t, f.tasks, f.ctx, f.task), "the creator's own completion notice already notifies them")

	// The claimed row is gone, so the ping cannot repeat.
	require.Empty(t, claimTaskWatches(t, f.tasks, f.ctx, f.task))
	require.Zero(t, f.watches(t))
}

// A named until fires on that state only, and a cancelled task counts as an
// end state.
func TestTaskWatchFiresOnTheNamedStateAndOnCancelled(t *testing.T) {
	f := newTaskWatcherFixture(t)
	_, _, watching := f.watch(t, f.user, "failed")
	require.True(t, watching)
	f.transition(t, nil, "validating", "publishing", "awaiting_review", "cancelling")
	require.Empty(t, claimTaskWatches(t, f.tasks, f.ctx, f.task), "review and cancelling do not satisfy a watch for failed")
	f.transition(t, nil, "failed")
	watches := claimTaskWatches(t, f.tasks, f.ctx, f.task)
	require.Len(t, watches, 1)
	require.Equal(t, "failed", watches[0].State)
	require.Equal(t, "failed", watches[0].Until)

	f2 := newTaskWatcherFixture(t)
	f2.watch(t, f2.user, "")
	f2.transition(t, &f2.user, "cancelling", "cancelled")
	watches = claimTaskWatches(t, f2.tasks, f2.ctx, f2.task)
	require.Len(t, watches, 1, "a cancelled task finished working; its watcher is pinged")
	require.Equal(t, "cancelled", watches[0].State)
	require.False(t, watches[0].Notified, "no creator notice covers a cancellation, so the watch notifies")
}

// The creator notice and a watch of the same event reach a user once: a
// watcher who is the notified creator gets only the chat message, anyone else
// and a creator who made the change themselves get the watch's notification.
func TestTaskWatchKnowsWhetherTheCreatorNoticeCoversTheWatcher(t *testing.T) {
	f := newTaskWatcherFixture(t)
	f.watch(t, f.user, "awaiting_review")
	f.watch(t, f.stranger, "awaiting_review")
	f.transition(t, nil, "validating", "publishing", "awaiting_review")
	notified := map[uuid.UUID]bool{}
	for _, watch := range claimTaskWatches(t, f.tasks, f.ctx, f.task) {
		require.Equal(t, "awaiting_review", watch.State)
		notified[watch.UserID] = watch.Notified
	}
	require.Equal(t, map[uuid.UUID]bool{f.user: true, f.stranger: false}, notified)

	f2 := newTaskWatcherFixture(t)
	f2.watch(t, f2.user, "awaiting_review")
	f2.transition(t, &f2.user, "validating", "publishing", "awaiting_review")
	watches := claimTaskWatches(t, f2.tasks, f2.ctx, f2.task)
	require.Len(t, watches, 1)
	require.False(t, watches[0].Notified, "the creator's own change sends them no notice")
}

// A watch never fires for a state the task already reached: registering then
// reports the state instead, read together with the registration.
func TestTaskWatchReportsAStateAlreadyReached(t *testing.T) {
	f := newTaskWatcherFixture(t)
	f.transition(t, nil, "validating", "publishing", "awaiting_review")
	_, state, watching := f.watch(t, f.user, "awaiting_review")
	require.False(t, watching)
	require.Equal(t, "awaiting_review", state)
	require.Zero(t, f.watches(t))

	f.transition(t, nil, "completed")
	for _, until := range []string{"", "completed"} {
		_, state, watching = f.watch(t, f.user, until)
		require.False(t, watching, until)
		require.Equal(t, "completed", state, until)
	}
	require.Zero(t, f.watches(t))

	// A failed task can be retried, so it can still be watched until it completes.
	f2 := newTaskWatcherFixture(t)
	f2.transition(t, nil, "cancelling", "failed")
	_, state, watching = f2.watch(t, f2.user, "completed")
	require.True(t, watching)
	require.Equal(t, "failed", state)
}

// Registering again from the same conversation replaces the watch, so a
// repeated request still pings exactly once with the latest choice.
func TestTaskWatchReregistrationReplacesTheWatch(t *testing.T) {
	f := newTaskWatcherFixture(t)
	_, _, err := f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, "")
	require.NoError(t, err)
	_, _, err = f.tasks.WatchTask(f.ctx, f.workspace, f.user, f.task, f.conversation, f.message, "awaiting_review")
	require.NoError(t, err)
	require.Equal(t, 1, f.watches(t))
	var until *string
	require.NoError(t, f.db.Get(&until, `SELECT until_state FROM agent_task_watchers WHERE task_id=$1`, f.task))
	require.NotNil(t, until)
	require.Equal(t, "awaiting_review", *until)

	// Another user or conversation keeps its own watch.
	_, _, err = f.tasks.WatchTask(f.ctx, f.workspace, f.stranger, f.task, f.conversation, f.message, "")
	require.NoError(t, err)
	require.Equal(t, 2, f.watches(t))
}

// A task of another workspace is never watched, whatever the caller passes,
// and the caller is told so.
func TestTaskWatchIsScopedToTheWorkspace(t *testing.T) {
	f := newTaskWatcherFixture(t)
	other := uuid.New()
	_, err := f.db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, other, "other-"+strings.ReplaceAll(other.String(), "-", ""), f.user)
	require.NoError(t, err)
	_, watching, err := f.tasks.WatchTask(f.ctx, other, f.user, f.task, f.conversation, f.message, "")
	require.ErrorIs(t, err, ErrWatchedTaskNotFound)
	require.False(t, watching)
	require.Zero(t, f.watches(t))
}

// Several workers claiming at once hand out every due watch exactly once.
func TestTaskWatchesAreClaimedOnceByConcurrentWorkers(t *testing.T) {
	f := newTaskWatcherFixture(t)
	const watchers = 24
	want := map[uuid.UUID]bool{}
	for range watchers {
		conversation, _, watching := f.watch(t, f.user, "")
		require.True(t, watching)
		want[conversation] = true
	}
	f.transition(t, nil, "cancelling", "cancelled")

	var mu sync.Mutex
	claimed := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				watches, err := f.tasks.ClaimTaskWatchers(f.ctx, 3)
				if !assert.NoError(t, err) || len(watches) == 0 {
					return
				}
				mu.Lock()
				for _, watch := range watches {
					claimed[watch.ConversationID]++
				}
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	require.Len(t, claimed, watchers)
	for conversation, times := range claimed {
		require.True(t, want[conversation])
		require.Equal(t, 1, times, "each watch is handed out once")
	}
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

// pingRequest stores the user message a watch answers in the task's chat.
func pingRequest(t *testing.T, db *sqlx.DB, conversation uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.Get(&id, `INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,$2,'user','Ping me when it is ready for review','completed') RETURNING id`, conversation, uuid.New()))
	return id
}

// claimTaskWatches claims the due watches and returns this task's.
func claimTaskWatches(t *testing.T, repo *AgentTaskRepository, ctx context.Context, task uuid.UUID) []domain.AgentTaskWatch {
	t.Helper()
	watches, err := repo.ClaimTaskWatchers(ctx, 1000)
	require.NoError(t, err)
	var own []domain.AgentTaskWatch
	for _, watch := range watches {
		if watch.TaskID == task {
			own = append(own, watch)
		}
	}
	return own
}

// A workflow passes through awaiting_review for its agent review and its CI
// wait, which need nobody: a watch for awaiting_review fires only when a step
// waits for a person, as the creator's review notice does, although that
// later wait records no new transition.
func TestWorkflowTaskWatchForReviewWaitsForAPerson(t *testing.T) {
	db, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	watch := func() (string, bool) {
		t.Helper()
		var conversation uuid.UUID
		require.NoError(t, db.Get(&conversation, `INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Ping chat') RETURNING id`, claimed.WorkspaceID, claimed.CreatorID))
		state, watching, err := repo.WatchTask(ctx, claimed.WorkspaceID, claimed.CreatorID, claimed.ID, conversation, pingRequest(t, db, conversation), "awaiting_review")
		require.NoError(t, err)
		return state, watching
	}
	_, watching := watch()
	require.True(t, watching)
	now := time.Now()
	apply := func(outcome, wantNode string, entry WorkflowEntry) {
		t.Helper()
		decision := graph.Decide(claimed.Cursor(), outcome, now, nil)
		require.Equal(t, wantNode, decision.To.ID)
		entry.Reason = "Workflow · " + decision.Reason
		moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: outcome, Moves: []WorkflowMove{{Decision: decision}}, Entry: entry})
		require.NoError(t, err)
		require.Equal(t, "awaiting_review", moved.State)
		claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits
	}

	apply(domain.WorkflowPublished, "review", WorkflowEntry{Kind: WorkflowEnterReview, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}})
	require.Empty(t, claimTaskWatches(t, repo, ctx, claimed.ID), "an agent reviews; the task is not ready for a person yet")
	apply(domain.WorkflowReviewApproved, "ci", WorkflowEntry{Kind: WorkflowEnterWait, WakeAt: &now})
	require.Empty(t, claimTaskWatches(t, repo, ctx, claimed.ID), "CI is pending; the task is not ready for a person yet")
	state, watching := watch()
	require.True(t, watching, "waiting for CI is not ready for review yet, so it can be watched")
	require.Equal(t, "awaiting_review", state)

	apply(domain.WorkflowChecksPassed, "ship", WorkflowEntry{Kind: WorkflowEnterWait, WakeAt: &now, NeedsPerson: true})
	watches := claimTaskWatches(t, repo, ctx, claimed.ID)
	require.Len(t, watches, 2, "the person wait pings both watchers")
	for _, watch := range watches {
		require.Equal(t, "awaiting_review", watch.State)
		require.True(t, watch.Notified, "the creator's review notice already notifies them")
	}
	require.Empty(t, claimTaskWatches(t, repo, ctx, claimed.ID), "the ping is one-shot")
	_, watching = watch()
	require.False(t, watching, "the task now waits for a person: that state is reported instead")
}
