package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// messageFixture: AUT-1 is the parent of AUT-2 and AUT-4 (siblings); AUT-3 is
// an unrelated issue of the same team; PRV-1 sits in a private team.
type messageFixture struct {
	automationFixture
	messages              *AgentMessageRepository
	parent, sibling       uuid.UUID
	private, member       uuid.UUID
	memberCtx             context.Context
	sender, recipientTask *domain.AgentTask
}

func newMessageFixture(t *testing.T) messageFixture {
	t.Helper()
	f := messageFixture{automationFixture: newAutomationFixture(t), sibling: uuid.New(), private: uuid.New(), member: uuid.New()}
	f.parent = f.labelled
	f.messages = NewAgentMessageRepository(f.db)
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := f.db.Exec(query, args...)
		require.NoError(t, err)
	}
	exec(`UPDATE issues SET identifier_text='AUT-3' WHERE id=$1`, f.finished)
	exec(`UPDATE issues SET parent_id=$1 WHERE id=$2`, f.parent, f.other)
	exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id,parent_id) VALUES($1,$2,$3,4,'AUT-4','Sibling',$4,$5,$6)`, f.sibling, f.workspace, f.team, f.user, f.todo, f.parent)
	// Private teams require a workspace without dev machine environments.
	exec(`DELETE FROM dev_machine_environments WHERE workspace_id=$1`, f.workspace)
	privateTeam := uuid.New()
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Private','PRV',TRUE)`, privateTeam, f.workspace)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, privateTeam, f.user)
	exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, privateTeam)
	exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'PRV-1','Secret',$4,(SELECT id FROM team_statuses WHERE team_id=$3))`, f.private, f.workspace, privateTeam, f.user)
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','x')`, f.member, f.member.String()+"@example.test")
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.workspace, f.member)
	f.memberCtx = domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.workspace, UserID: f.member})
	f.sender = f.task(t, f.other)
	f.recipientTask = f.task(t, f.sibling)
	return f
}

func outbox(name, body string) domain.AgentOutboxFile {
	return domain.AgentOutboxFile{Worktree: "/workspace/tasks/aut-2", Name: name, Data: []byte(body)}
}

func (f messageFixture) states(t *testing.T) map[string]string {
	t.Helper()
	rows := []struct {
		Ref   string `db:"to_ref"`
		State string `db:"state"`
	}{}
	require.NoError(t, f.db.Select(&rows, `SELECT to_ref,state FROM agent_messages WHERE from_task_id=$1`, f.sender.ID))
	states := map[string]string{}
	for _, row := range rows {
		states[row.Ref] = row.State
	}
	return states
}

func TestOutboxMessagesReachOnlyRelatedIssuesOfTheTeam(t *testing.T) {
	f := newMessageFixture(t)
	recorded, err := f.messages.RecordTaskOutbox(context.Background(), f.sender.ID, []domain.AgentOutboxFile{
		outbox("1.json", `{"to":"AUT-4","body":"Schema is merged"}`),
		outbox("2.json", `{"to":"aut-1","channel":"comment","body":"API ready for review"}`),
		outbox("3.json", `{"to":"AUT-3","body":"Unrelated"}`),
		outbox("4.json", `{"to":"PRV-1","body":"Leak"}`),
		outbox("5.json", `{"to":"AUT-2","body":"Myself"}`),
		outbox("6.json", `{"to":"AUT-9","body":"x","extra":true}`),
		{Worktree: "/workspace/tasks/aut-2", Name: "7.json", Data: []byte(`{"to":"AUT-4"`), Oversize: true},
	})
	require.NoError(t, err)
	require.Equal(t, 7, recorded)
	require.Equal(t, map[string]string{"AUT-4": "queued", "AUT-1": "queued", "AUT-3": "rejected", "PRV-1": "rejected", "AUT-2": "rejected", "AUT-9": "rejected", "?": "rejected"}, f.states(t))
	var reasons []string
	require.NoError(t, f.db.Select(&reasons, `SELECT reject_reason FROM agent_messages WHERE from_task_id=$1 AND state='rejected' ORDER BY to_ref`, f.sender.ID))
	require.Contains(t, reasons, "not a related issue of this task")
	require.Contains(t, reasons, "message file is too large")

	var message domain.AgentMessage
	require.NoError(t, f.db.Get(&message, `SELECT * FROM agent_messages WHERE from_task_id=$1 AND to_ref='AUT-4'`, f.sender.ID))
	require.Equal(t, f.sibling, *message.ToIssueID)
	require.Equal(t, f.recipientTask.ID, *message.ToTaskID, "the recipient issue's newest task is named")
	require.Equal(t, f.other, message.FromIssueID)
	require.Equal(t, "none", message.CommentState)
	require.Equal(t, f.sender.RunID, *message.FromRunID)
	var commentState string
	require.NoError(t, f.db.Get(&commentState, `SELECT comment_state FROM agent_messages WHERE from_task_id=$1 AND to_ref='AUT-1'`, f.sender.ID))
	require.Equal(t, "pending", commentState)

	// A file taken twice is one message; the hourly budget counts refusals.
	again, err := f.messages.RecordTaskOutbox(context.Background(), f.sender.ID, []domain.AgentOutboxFile{outbox("1.json", `{"to":"AUT-4","body":"Schema is merged"}`)})
	require.NoError(t, err)
	require.Zero(t, again)
	files := []domain.AgentOutboxFile{}
	for i := range 20 {
		files = append(files, outbox("burst-"+strings.Repeat("x", i)+".json", `{"to":"AUT-4","body":"ping `+strings.Repeat("!", i)+`"}`))
	}
	recorded, err = f.messages.RecordTaskOutbox(context.Background(), f.sender.ID, files)
	require.NoError(t, err)
	require.Equal(t, domain.AgentMessagesPerHour-7, recorded, "at most the hourly budget is recorded")
}

func TestHeldConversationsWaitForAPersonAndAreAudited(t *testing.T) {
	f := newMessageFixture(t)
	ctx := f.ctx
	_, err := f.messages.RecordTaskOutbox(ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("1.json", `{"to":"AUT-4","body":"first"}`)})
	require.NoError(t, err)

	_, err = f.messages.HoldConversation(f.memberCtx, f.workspace, f.other, f.sibling)
	require.ErrorIs(t, err, ErrAgentMessageForbidden, "a member who created neither task cannot hold it")
	hold, err := f.messages.HoldConversation(ctx, f.workspace, f.sibling, f.other)
	require.NoError(t, err)
	require.Equal(t, 1, hold.HeldMessages, "queued messages of the pair are held")
	_, err = f.messages.RecordTaskOutbox(ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("2.json", `{"to":"AUT-4","channel":"comment","body":"second"}`), outbox("3.json", `{"to":"AUT-1","body":"other pair"}`)})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"AUT-4": "held", "AUT-1": "queued"}, f.states(t))
	var commentState string
	require.NoError(t, f.db.Get(&commentState, `SELECT comment_state FROM agent_messages WHERE body='second'`))
	require.Equal(t, "none", commentState, "a held comment is not posted")

	deliveries, err := f.messages.Claim(ctx, *f.recipientTask, 10000)
	require.NoError(t, err)
	require.Empty(t, deliveries, "held messages are not delivered")

	page, err := f.messages.List(ctx, f.workspace, domain.AgentMessageFilter{IssueID: &f.sibling})
	require.NoError(t, err)
	require.Len(t, page.Messages, 2)
	var second domain.AgentMessageView
	for _, message := range page.Messages {
		require.True(t, message.CanModerate, "an administrator moderates")
		require.Equal(t, hold.ID, *message.ConversationHoldID)
		require.Equal(t, "AUT-2", message.FromIdentifier)
		if message.Body == "second" {
			second = message
		}
	}
	memberPage, err := f.messages.List(f.memberCtx, f.workspace, domain.AgentMessageFilter{IssueID: &f.sibling})
	require.NoError(t, err)
	require.Len(t, memberPage.Messages, 2, "every member who can see the team reads the registry")
	require.False(t, memberPage.Messages[0].CanModerate)
	_, err = f.messages.Act(f.memberCtx, f.workspace, second.ID, "release")
	require.ErrorIs(t, err, ErrAgentMessageForbidden)

	released, err := f.messages.Act(ctx, f.workspace, second.ID, "release")
	require.NoError(t, err)
	require.Equal(t, "queued", released.State)
	require.Equal(t, "pending", released.CommentState, "a released comment message is posted")
	require.Equal(t, f.user, *released.ReleasedBy)
	_, err = f.messages.Act(ctx, f.workspace, second.ID, "release")
	require.ErrorIs(t, err, ErrAgentMessageConflict)
	require.NoError(t, f.messages.ReleaseConversation(ctx, f.workspace, hold.ID))
	first := f.states(t)
	require.Equal(t, "queued", first["AUT-4"])
	dropped, err := f.messages.Act(ctx, f.workspace, second.ID, "drop")
	require.NoError(t, err)
	require.Equal(t, "dropped", dropped.State)

	_, actions, err := f.messages.Get(ctx, f.workspace, second.ID)
	require.NoError(t, err)
	names := []string{}
	for _, action := range actions {
		require.Equal(t, f.user, action.ActorID)
		names = append(names, action.Action)
	}
	require.Equal(t, []string{"release", "drop"}, names)
	var conversationActions int
	require.NoError(t, f.db.Get(&conversationActions, `SELECT count(*) FROM agent_message_actions WHERE workspace_id=$1 AND action IN ('hold_conversation','release_conversation')`, f.workspace))
	require.Equal(t, 2, conversationActions)

	// The recipient task's creator moderates the messages it receives.
	_, err = f.db.Exec(`UPDATE agent_tasks SET creator_id=$1 WHERE id=$2`, f.member, f.recipientTask.ID)
	require.NoError(t, err)
	_, err = f.messages.HoldConversation(f.memberCtx, f.workspace, f.other, f.sibling)
	require.NoError(t, err)
}

func TestQueuedMessagesAreDeliveredOnceToTheRecipientsNextAttempt(t *testing.T) {
	f := newMessageFixture(t)
	_, err := f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, []domain.AgentOutboxFile{
		outbox("1.json", `{"to":"AUT-4","body":"first"}`),
		outbox("2.json", `{"to":"AUT-4","body":"`+strings.Repeat("b", 3000)+`"}`),
	})
	require.NoError(t, err)
	deliveries, err := f.messages.Claim(f.ctx, *f.recipientTask, 600)
	require.NoError(t, err)
	require.Len(t, deliveries, 1, "only messages that fit the prompt are claimed")
	require.Equal(t, "first", deliveries[0].Body)
	require.Equal(t, "AUT-2", deliveries[0].FromIdentifier)
	replayed, err := f.messages.Claim(f.ctx, *f.recipientTask, 10000)
	require.NoError(t, err)
	require.Equal(t, deliveries, replayed, "a repeated attempt creation receives the same messages")
	var delivered domain.AgentMessage
	require.NoError(t, f.db.Get(&delivered, `SELECT * FROM agent_messages WHERE body='first'`))
	require.Equal(t, "delivered", delivered.State)
	require.Equal(t, f.recipientTask.RunID, *delivered.DeliveredRunID)

	// The next attempt receives what is still queued, and messages claimed
	// for an attempt that was never created are claimed again.
	next := *f.recipientTask
	next.RunID = uuid.New()
	_, err = f.db.Exec(`UPDATE agent_tasks SET run_id=$1 WHERE id=$2`, next.RunID, next.ID)
	require.NoError(t, err)
	deliveries, err = f.messages.Claim(f.ctx, next, 10000)
	require.NoError(t, err)
	bodies := []string{}
	for _, delivery := range deliveries {
		bodies = append(bodies, delivery.Body)
	}
	require.ElementsMatch(t, []string{"first", strings.Repeat("b", 3000)}, bodies)
	held, err := f.messages.Claim(f.ctx, next, 10000)
	require.NoError(t, err)
	require.Len(t, held, 2, "the same attempt keeps its messages")
}

func TestCommentMessagesArePostedAsTheSendingAgent(t *testing.T) {
	f := newMessageFixture(t)
	_, err := f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("1.json", `{"to":"AUT-4","channel":"comment","body":"Use <b>v2</b> & keep\nthe flag"}`)})
	require.NoError(t, err)
	posted, err := f.messages.PostNextComment(context.Background())
	require.NoError(t, err)
	require.NotNil(t, posted)
	require.Equal(t, f.sibling, posted.IssueID)
	require.Equal(t, "AUT-4", posted.Identifier)
	var comment domain.Comment
	require.NoError(t, f.db.Get(&comment, `SELECT * FROM comments WHERE id=$1`, posted.CommentID))
	require.Equal(t, f.sender.ID, *comment.AgentTaskID)
	require.Equal(t, f.sender.CreatorID, comment.UserID)
	require.Contains(t, comment.Body, "AUT-2")
	require.Contains(t, comment.Body, "<p>Use &lt;b&gt;v2&lt;/b&gt; &amp; keep</p><p>the flag</p>")
	var message domain.AgentMessage
	require.NoError(t, f.db.Get(&message, `SELECT * FROM agent_messages WHERE from_task_id=$1`, f.sender.ID))
	require.Equal(t, "posted", message.CommentState)
	require.Equal(t, comment.ID, *message.CommentID)
	require.Equal(t, "queued", message.State, "the recipient agent still receives it in its prompt")
	none, err := f.messages.PostNextComment(context.Background())
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestTheRegistryHidesMessagesOfInvisibleTeams(t *testing.T) {
	f := newMessageFixture(t)
	_, err := f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("1.json", `{"to":"AUT-4","body":"secret plan"}`)})
	require.NoError(t, err)
	page, err := f.messages.List(f.memberCtx, f.workspace, domain.AgentMessageFilter{State: "queued", Channel: "internal"})
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)
	require.Equal(t, "AUT-4", *page.Messages[0].ToIdentifier)
	page, err = f.messages.List(f.memberCtx, f.workspace, domain.AgentMessageFilter{State: "held"})
	require.NoError(t, err)
	require.Empty(t, page.Messages)
	// The team becomes private and the member is not in it.
	_, err = f.db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, f.team, f.user)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, f.team)
	require.NoError(t, err)
	page, err = f.messages.List(f.memberCtx, f.workspace, domain.AgentMessageFilter{})
	require.NoError(t, err)
	require.Empty(t, page.Messages)
	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: uuid.New(), UserID: f.member})
	_, err = f.messages.List(outsider, f.workspace, domain.AgentMessageFilter{})
	require.ErrorIs(t, err, ErrAgentMessageNotFound)
	page, err = f.messages.List(f.ctx, f.workspace, domain.AgentMessageFilter{})
	require.NoError(t, err)
	require.Len(t, page.Messages, 1, "team members still see it")
	_, _, err = f.messages.Get(f.memberCtx, f.workspace, page.Messages[0].ID)
	require.ErrorIs(t, err, ErrAgentMessageNotFound)
}
