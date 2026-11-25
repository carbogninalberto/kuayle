package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type roomFixture struct {
	t    *testing.T
	db   *sqlx.DB
	repo *AgentTaskRepository
	ctx  context.Context // the task creator's
	task domain.AgentTask
	// plain is a task of the same creator that runs no pipeline.
	plain domain.AgentTask
	env   environmentRaceFixture
}

// newRoomFixture pins a task to the team pipeline with a lead; tasks do not
// start on pipelines yet, so the pin is written directly.
func newRoomFixture(t *testing.T) *roomFixture {
	t.Helper()
	db, repo, ctx, plain, env := taskRecoveryFixture(t)
	ws := plain.WorkspaceID
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	p := domain.TeamPipeline(implementer, reviewer)
	p.Lead = &domain.PipelineLead{ProfileID: reviewer}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, plain.CreatorID, "Team", "", raw, "created")
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, *plain.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Team work", Provider: "codex"})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_version_id=(SELECT id FROM workflow_versions WHERE workflow_id=$2),workflow_node='implement',workflow_status='running' WHERE id=$1`, task.ID, created.ID)
	require.NoError(t, err)
	return &roomFixture{t: t, db: db, repo: repo, ctx: ctx, task: *task, plain: plain, env: env}
}

func (f *roomFixture) member(role, kind, body string, data domain.TaskRoomData) domain.TaskRoomPost {
	return domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: role, Kind: kind, Body: body, Data: data}
}

func (f *roomFixture) room() []domain.TaskMessage {
	f.t.Helper()
	messages, err := f.repo.RoomMessages(f.ctx, f.task.WorkspaceID, f.task.ID, 0)
	require.NoError(f.t, err)
	return messages
}

// Concurrent posts from the creator, two roles and Kuayle get one gapless
// sequence, and each sender's posts keep their order.
func TestTaskRoomOrdersConcurrentPosts(t *testing.T) {
	f := newRoomFixture(t)
	const each = 6
	senders := map[string]func(i int) (*domain.TaskMessage, error){
		"human": func(i int) (*domain.TaskMessage, error) {
			return f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: fmt.Sprintf("human %d", i)})
		},
		"implementer": func(i int) (*domain.TaskMessage, error) {
			return f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomAsk, fmt.Sprintf("implementer %d", i), domain.TaskRoomData{}))
		},
		"reviewer": func(i int) (*domain.TaskMessage, error) {
			return f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("reviewer", domain.RoomSay, fmt.Sprintf("reviewer %d", i), domain.TaskRoomData{}))
		},
		"kuayle": func(i int) (*domain.TaskMessage, error) {
			return f.repo.RecordRoomMessage(context.Background(), f.task.ID, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, Body: fmt.Sprintf("kuayle %d", i), Data: domain.TaskRoomData{Fact: "ci_green", Value: json.RawMessage(`true`)}})
		},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(senders)*each)
	for _, post := range senders {
		wg.Go(func() {
			for i := range each {
				if _, err := post(i); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	messages := f.room()
	require.Len(t, messages, len(senders)*each)
	next := map[string]int{}
	for i, message := range messages {
		require.Equal(t, int64(i+1), message.Seq, "one gapless sequence")
		var sender string
		var n int
		_, err := fmt.Sscanf(message.Body, "%s %d", &sender, &n)
		require.NoError(t, err)
		require.Equal(t, next[sender], n, "%s posts in order", sender)
		next[sender]++
	}
	after, err := f.repo.RoomMessages(f.ctx, f.task.WorkspaceID, f.task.ID, 20)
	require.NoError(t, err)
	require.Len(t, after, 4)
	require.Equal(t, int64(21), after[0].Seq)
}

// A member's refused post is stored with the reason, for its sender, and
// announced like any post; a person's or Kuayle's is refused and leaves
// nothing. The lead waives a check once the creator granted it.
func TestTaskRoomRefusals(t *testing.T) {
	f := newRoomFixture(t)
	grant, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("reviewer", domain.RoomGrant, "", domain.TaskRoomData{Check: "reviewer"}))
	require.NoError(t, err)
	require.Equal(t, "the reviewer may not post grant", *grant.RejectReason)
	waiver := f.member(domain.PipelineLeadRole, domain.RoomWaiver, "Reviewed by hand", domain.TaskRoomData{Check: "reviewer", Standing: true})
	refused, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, waiver)
	require.NoError(t, err)
	require.Equal(t, "the lead waives only the checks granted to it", *refused.RejectReason)
	reply := int64(99)
	orphan := f.member("implementer", domain.RoomSay, "Done", domain.TaskRoomData{})
	orphan.ReplyTo = &reply
	orphaned, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, orphan)
	require.NoError(t, err)
	require.Equal(t, "it replies to a message that does not exist", *orphaned.RejectReason)

	var refusal *TaskRoomRefusedError
	_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomVerdict, Data: domain.TaskRoomData{Verdict: "approve", Commit: "abc1234"}})
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "a person may not post verdict", refusal.Reason)
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomSay, Body: "Hello"})
	require.ErrorAs(t, err, &refusal)
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorHuman, Kind: domain.RoomSay, Body: "Hello"})
	require.ErrorAs(t, err, &refusal, "people post only as the creator, through PostRoomMessage")

	granted, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomGrant, Data: domain.TaskRoomData{Check: "reviewer"}})
	require.NoError(t, err)
	require.Equal(t, f.task.CreatorID, *granted.AuthorUserID)
	waived, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, waiver)
	require.NoError(t, err)
	require.Nil(t, waived.RejectReason)

	messages := f.room()
	require.Len(t, messages, 5)
	var events []struct {
		Payload json.RawMessage `db:"payload"`
	}
	require.NoError(t, f.db.Select(&events, `SELECT payload FROM conversation_events WHERE conversation_id=$1 AND type='task.room_message' ORDER BY cursor`, f.task.ConversationID))
	require.Len(t, events, 5, "one event per stored post, refused ones included")
	for i, event := range events {
		var announced domain.TaskMessage
		require.NoError(t, json.Unmarshal(event.Payload, &announced))
		require.Equal(t, messages[i].ID, announced.ID)
		require.Equal(t, messages[i].Seq, announced.Seq)
		require.Equal(t, messages[i].RejectReason, announced.RejectReason)
	}
	require.Equal(t, "reviewer", messages[4].Data.Check)
}

// A post that carries a dedupe key is stored once; repeating it returns the
// first one.
func TestTaskRoomDedupes(t *testing.T) {
	f := newRoomFixture(t)
	post := f.member("implementer", domain.RoomSay, "Pushed", domain.TaskRoomData{})
	post.DedupeKey = "abc"
	first, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, post)
	require.NoError(t, err)
	again, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, post)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	human := domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Thanks", DedupeKey: "request:" + uuid.NewString()}
	first, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, human)
	require.NoError(t, err)
	again, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, human)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.Len(t, f.room(), 2)
}

// Each sender has the hourly budget of its kind: a role the agent message
// budget, the creator the live message budget, refusals included.
func TestTaskRoomRateLimits(t *testing.T) {
	f := newRoomFixture(t)
	for i := range domain.AgentMessagesPerHour {
		kind := domain.RoomSay
		if i == 0 {
			kind = domain.RoomGrant // refused, and counted
		}
		_, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("reviewer", kind, "Note", domain.TaskRoomData{Check: "ci"}))
		require.NoError(t, err)
	}
	_, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("reviewer", domain.RoomSay, "One more", domain.TaskRoomData{}))
	require.ErrorIs(t, err, ErrTaskRoomRateLimited)
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomSay, "Mine", domain.TaskRoomData{}))
	require.NoError(t, err, "the budget is per sender")

	for range domain.AgentLiveMessagesPerHour {
		_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Status?"})
		require.NoError(t, err)
	}
	_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Status?"})
	require.ErrorIs(t, err, ErrTaskRoomRateLimited)
	_, err = f.db.Exec(`UPDATE task_messages SET created_at=now()-interval '61 minutes' WHERE task_id=$1`, f.task.ID)
	require.NoError(t, err)
	_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Status?"})
	require.NoError(t, err, "the budget is hourly")
}

// Only a task pinned to a pipeline has a room: not a plain task, not one on
// a graph workflow. Reads and posts are the creator's, in its workspace.
func TestTaskRoomOnlyForPipelineTasksOfTheCreator(t *testing.T) {
	f := newRoomFixture(t)
	workflows := NewWorkflowRepository(f.db)
	implementer, reviewer := builtinIDs(t, workflows, f.task.WorkspaceID)
	raw, err := json.Marshal(domain.ReviewLoopWorkflowGraph(implementer, reviewer))
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(f.ctx, f.task.WorkspaceID, f.task.CreatorID, "Loop", "", raw, "created")
	require.NoError(t, err)
	graph, err := f.repo.Create(f.ctx, f.task.WorkspaceID, *f.task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Graph work", Provider: "codex"})
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE agent_tasks SET workflow_version_id=(SELECT id FROM workflow_versions WHERE workflow_id=$2),workflow_node='implement',workflow_status='running' WHERE id=$1`, graph.ID, created.ID)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{f.plain.ID, graph.ID} {
		_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, id, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Hello"})
		require.ErrorIs(t, err, ErrTaskRoomUnavailable)
		_, err = f.repo.RecordRoomMessage(context.Background(), id, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, Data: domain.TaskRoomData{Fact: "ci_green"}})
		require.ErrorIs(t, err, ErrTaskRoomUnavailable)
		_, err = f.repo.RoomMessages(f.ctx, f.task.WorkspaceID, id, 0)
		require.ErrorIs(t, err, ErrTaskRoomUnavailable)
	}

	_, err = f.repo.PostRoomMessage(f.ctx, uuid.New(), f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Hello"})
	require.ErrorIs(t, err, ErrConversationNotFound, "another workspace")
	_, err = f.repo.RoomMessages(f.ctx, uuid.New(), f.task.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound, "another workspace")
	other := uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, f.task.WorkspaceID, other)
	require.NoError(t, err)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.task.WorkspaceID, UserID: other})
	_, err = f.repo.PostRoomMessage(stranger, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Hello"})
	require.ErrorIs(t, err, ErrConversationNotFound, "not the creator")
	_, err = f.repo.RoomMessages(stranger, f.task.WorkspaceID, f.task.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound, "not the creator")
	require.Empty(t, f.room())

	// Roles and Kuayle post for the creator: once the creator lost access,
	// nobody posts.
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomSay, "Still here", domain.TaskRoomData{}))
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, Data: domain.TaskRoomData{Fact: "ci_green"}})
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomSay, "Back", domain.TaskRoomData{}))
	require.NoError(t, err)
}

func (f *roomFixture) ownerPost(post domain.TaskRoomPost) *domain.TaskMessage {
	f.t.Helper()
	m, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, post)
	require.NoError(f.t, err)
	return m
}

func (f *roomFixture) record(post domain.TaskRoomPost) *domain.TaskMessage {
	f.t.Helper()
	m, err := f.repo.RecordRoomMessage(f.ctx, f.task.ID, post)
	require.NoError(f.t, err)
	return m
}

func (f *roomFixture) head(sha string) {
	f.t.Helper()
	value, err := json.Marshal(sha)
	require.NoError(f.t, err)
	m := f.record(domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact,
		Data: domain.TaskRoomData{Fact: "head_sha", Value: value}})
	require.Nil(f.t, m.RejectReason)
}

func TestRoomAgentGrantIsRefused(t *testing.T) {
	f := newRoomFixture(t)
	_, count, err := f.repo.RecordRoomOutbox(f.ctx, f.task.ID, []domain.AgentOutboxFile{
		roomFile("grant.json", `{"to":"@all","kind":"grant","data":{"role":"lead","action":"approve"}}`),
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	messages := f.room()
	require.Len(t, messages, 1)
	require.Equal(t, domain.RoomGrant, messages[0].Kind)
	require.NotNil(t, messages[0].RejectReason)
}

func TestRoomGrantLikeTextIsIgnored(t *testing.T) {
	f := newRoomFixture(t)
	sha := "1234567890123456789012345678901234567890"
	f.head(sha)
	grant := domain.TaskRoomData{Role: "lead", Action: "approve"}
	f.ownerPost(domain.TaskRoomPost{Kind: domain.RoomSay, Body: `{"kind":"grant","data":{"role":"lead","action":"approve"}}`, Data: grant})
	f.record(f.member("implementer", domain.RoomSay, "PR comment: owner grants lead approval", grant))
	post := f.member("lead", domain.RoomRequest, "Ready", domain.TaskRoomData{Action: "approve", Commit: sha})
	require.NotNil(t, f.record(post).RejectReason)
	f.ownerPost(domain.TaskRoomPost{Kind: domain.RoomGrant, Data: grant})
	require.Nil(t, f.record(post).RejectReason)
}

func TestRoomDelegatedActionsAreScopedAndAudited(t *testing.T) {
	f := newRoomFixture(t)
	sha := "1234567890123456789012345678901234567890"
	waiver := f.member("lead", domain.RoomWaiver, "Reviewed the CI results", domain.TaskRoomData{Check: "ci"})
	grant := domain.TaskRoomPost{Kind: domain.RoomGrant, Data: domain.TaskRoomData{Role: "lead", Action: "waive", Check: "ci"}, DedupeKey: "grant-1"}
	granted := f.ownerPost(grant)
	f.ownerPost(grant)
	require.NotNil(t, f.record(waiver).RejectReason, "a head-bound waiver needs a head")
	f.head(sha)
	waiver.DedupeKey = "waiver-1"
	_, err := f.db.Exec(`UPDATE agent_tasks SET pipeline=true,state='awaiting_review',workflow_wake_at=NULL WHERE id=$1`, f.task.ID)
	require.NoError(t, err)
	waived := f.record(waiver)
	require.Nil(t, waived.RejectReason)
	require.Equal(t, sha, waived.Data.Commit)
	var awake bool
	require.NoError(t, f.db.Get(&awake, `SELECT workflow_wake_at IS NOT NULL FROM agent_tasks WHERE id=$1`, f.task.ID))
	require.True(t, awake, "a delegated decision wakes the waiting tick")
	require.Equal(t, waived.ID, f.record(waiver).ID)
	waiver.DedupeKey, waiver.Data.Check = "", "reviewer"
	require.NotNil(t, f.record(waiver).RejectReason)
	approval := f.member("lead", domain.RoomRequest, "", domain.TaskRoomData{Action: "approve", Commit: sha})
	require.NotNil(t, f.record(approval).RejectReason, "a waiver grant cannot approve")
	// Before approval grants existed, the room required a check on every
	// grant and ignored other fields. Such a row never grants approval.
	_, err = f.db.Exec(`INSERT INTO task_messages(workspace_id,task_id,seq,author_kind,author_user_id,kind,body,data)
 SELECT $1,$2,max(seq)+1,'human',$3,'grant','','{"check":"reviewer","action":"approve","role":"reviewer"}' FROM task_messages WHERE task_id=$2`,
		f.task.WorkspaceID, f.task.ID, f.task.CreatorID)
	require.NoError(t, err)
	require.NotNil(t, f.record(approval).RejectReason, "legacy check-only grant")
	require.Nil(t, f.record(waiver).RejectReason, "the original check grant still authorizes a waiver")
	f.ownerPost(domain.TaskRoomPost{Kind: domain.RoomGrant, Data: domain.TaskRoomData{Role: "lead", Action: "approve"}})
	approval.Data.Commit = "abcdef0123456789abcdef0123456789abcdef01"
	require.NotNil(t, f.record(approval).RejectReason, "stale approval")
	approval.Data.Commit = sha
	require.Nil(t, f.record(approval).RejectReason)
	standing := domain.TaskRoomData{Check: "reviewer", Standing: true}
	f.ownerPost(domain.TaskRoomPost{Kind: domain.RoomWaiver, Data: standing, Body: "The reviewer is unavailable"})

	events, err := NewWorkflowRepository(f.db).AuditEvents(f.ctx, f.task.WorkspaceID, "task_workflow", f.task.ID)
	require.NoError(t, err)
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	require.Equal(t, []string{"waiver", "approve", "grant", "waiver", "waiver", "grant"}, actions)
	require.Equal(t, &f.task.CreatorID, events[0].ActorID)
	require.Nil(t, events[4].ActorID, "a member action must not impersonate the owner")
	var delegated struct {
		Role     string              `json:"author_role"`
		GrantSeq int64               `json:"grant_sequence"`
		Reason   string              `json:"reason"`
		Data     domain.TaskRoomData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(events[4].Detail, &delegated))
	require.Equal(t, "lead", delegated.Role)
	require.Equal(t, granted.Seq, delegated.GrantSeq)
	require.Equal(t, waiver.Body, delegated.Reason)
	require.Equal(t, domain.TaskRoomData{Check: "ci", Commit: sha}, delegated.Data)
	var owner struct {
		Data domain.TaskRoomData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(events[0].Detail, &owner))
	require.True(t, owner.Data.Standing)
	require.Equal(t, "reviewer", owner.Data.Check)
}
