package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

const (
	firstCommit  = "0123456789abcdef0123456789abcdef01234567"
	secondCommit = "89abcdef0123456789abcdef0123456789abcdef"
)

// memberFixture is a pipeline task on a machine, whose attempts can publish.
type memberFixture struct {
	*roomFixture
	machine uuid.UUID
}

func newMemberFixture(t *testing.T) *memberFixture {
	t.Helper()
	f := &memberFixture{roomFixture: newRoomFixture(t)}
	f.machine = f.env.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err := f.db.Exec(`UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, f.task.ID, f.machine)
	require.NoError(t, err)
	return f
}

// publish records an attempt of the root task that published commit on its
// pull request, as Kuayle's publication does.
func (f *memberFixture) publish(commit string) {
	f.t.Helper()
	run := uuid.New()
	_, err := f.db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at,completed_at)
 VALUES($1,$2,$3,$4,$5,'codex','autonomous','Implement','["codex"]',60,'succeeded',now(),now())`, run, f.task.WorkspaceID, f.machine, f.task.ID, f.task.CreatorID)
	require.NoError(f.t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source) VALUES
 ($1,$2,$3,'pull_request','fixture/repo','kuayle/rec-1',NULL,7,'https://github.com/fixture/repo/pull/7','published'),
 ($1,$2,$3,'commit','fixture/repo','kuayle/rec-1',$4,NULL,NULL,'published')`, f.task.WorkspaceID, f.machine, run, commit)
	require.NoError(f.t, err)
}

// requestReview is the implementer asking the reviewer for a review.
func (f *memberFixture) requestReview() {
	f.t.Helper()
	post := f.member("implementer", domain.RoomRequest, "Please review", domain.TaskRoomData{Action: domain.RoomRequestReview})
	post.ToRoles = []string{"reviewer"}
	message, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, post)
	require.NoError(f.t, err)
	require.Nil(f.t, message.RejectReason)
}

func (f *memberFixture) memberTask(member *domain.TaskMember) domain.AgentTask {
	f.t.Helper()
	require.NotNil(f.t, member.MemberTaskID)
	var task domain.AgentTask
	require.NoError(f.t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, *member.MemberTaskID))
	return task
}

func (f *memberFixture) reload(role string) domain.TaskMember {
	f.t.Helper()
	var member domain.TaskMember
	require.NoError(f.t, f.db.Get(&member, `SELECT * FROM task_members WHERE task_id=$1 AND role=$2`, f.task.ID, role))
	return member
}

// runTurn plays the member's queued turn on the machine: the worker claims
// its task, the agent runs in session (during, when set, is what happens
// while it works) and ends with final, and the worker completes the turn.
func (f *memberFixture) runTurn(member *domain.TaskMember, session, final string, during func(task domain.AgentTask)) domain.AgentTask {
	f.t.Helper()
	task := f.memberTask(member)
	_, err := f.db.Exec(`UPDATE agent_tasks SET next_poll_at=now()+interval '1 hour' WHERE workspace_id=$1 AND id<>$2`, task.WorkspaceID, task.ID)
	require.NoError(f.t, err)
	claimed, err := f.repo.Claim(f.ctx)
	require.NoError(f.t, err)
	require.NotNil(f.t, claimed)
	require.Equal(f.t, task.ID, claimed.ID)
	require.NoError(f.t, f.repo.Transition(f.ctx, *claimed, "preparing", "Preparing the turn"))
	_, err = f.db.Exec(`UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, task.ID, f.machine)
	require.NoError(f.t, err)
	claimed.MachineID = &f.machine
	_, err = f.db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at,native_session_id)
 VALUES($1,$2,$3,$4,$5,$6,'autonomous',$7,'["agent"]',60,'running',now(),$8)`, claimed.RunID, task.WorkspaceID, f.machine, task.ID, task.CreatorID, task.Provider, task.Prompt, session)
	require.NoError(f.t, err)
	_, err = f.repo.Start(f.ctx, *claimed)
	require.NoError(f.t, err)
	require.Equal(f.t, domain.MemberRunning, f.reload(member.Role).State)
	if during != nil {
		during(task)
	}
	_, err = f.db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),summary=$2 WHERE id=$1`, claimed.RunID, final)
	require.NoError(f.t, err)
	require.NoError(f.t, f.repo.CompleteReview(f.ctx, *claimed))
	_, err = f.db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE workspace_id=$1`, task.WorkspaceID)
	require.NoError(f.t, err)
	return f.memberTask(member)
}

// A pipeline task's writer is a member from the start: the root task plays
// it with the roster's profile, once. A task without a pipeline has none.
func TestThePipelineTaskIsItsWritersMember(t *testing.T) {
	f := newMemberFixture(t)
	implementer, _ := builtinIDs(t, NewWorkflowRepository(f.db), f.task.WorkspaceID)
	members, err := f.repo.Members(f.ctx, f.task.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	writer := members[0]
	require.Equal(t, "implementer", writer.Role)
	require.Nil(t, writer.MemberTaskID, "its turns are the root task's")
	require.Equal(t, implementer, writer.ProfileID)
	require.Equal(t, domain.MemberQueued, writer.State, "the root task is queued")
	again, err := f.repo.Members(f.ctx, f.task.ID)
	require.NoError(t, err)
	require.Equal(t, members, again)

	_, err = f.repo.Members(f.ctx, f.plain.ID)
	require.ErrorIs(t, err, ErrTaskRoomUnavailable)
	_, err = f.repo.StartMemberTurn(f.ctx, f.task.ID, "implementer", false)
	require.ErrorIs(t, err, ErrMemberWrites, "the writer's turns are iterations of the root task")
	_, err = f.repo.StartMemberTurn(f.ctx, f.task.ID, "security", false)
	require.ErrorIs(t, err, ErrTaskRoomUnavailable, "not a role of the pipeline")
}

// The reviewer gets its member and its review task the first time it is
// addressed; the second pass reuses that task as its next generation, which
// resumes the first pass's session, reads only what is new, and reviews the
// newest published commit.
func TestAReviewerIsOneTaskReusedAcrossPasses(t *testing.T) {
	f := newMemberFixture(t)
	_, reviewerProfile := builtinIDs(t, NewWorkflowRepository(f.db), f.task.WorkspaceID)
	_, err := f.db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,(random()*1e12)::bigint+1,'x','x',true,'{12345}')`, f.task.WorkspaceID)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,12345)`, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)
	f.publish(firstCommit)
	f.requestReview()

	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	require.Equal(t, "reviewer", member.Role)
	require.Equal(t, reviewerProfile, member.ProfileID)
	require.Equal(t, domain.MemberQueued, member.State)
	require.Equal(t, 1, member.Turns)
	require.Equal(t, int64(1), member.LastReadSeq)
	require.Equal(t, firstCommit, *member.PinnedCommit)
	require.True(t, member.VerdictAsked, "the room asked it for a review")
	task := f.memberTask(member)
	require.Equal(t, domain.AgentTaskKindReview, task.Kind, "a review's isolation: never published, no push")
	require.Equal(t, f.task.ID, *task.ReviewOfTaskID)
	require.Equal(t, *f.task.ConversationID, *task.ConversationID)
	require.Equal(t, f.task.CreatorID, task.CreatorID)
	require.Equal(t, "queued", task.State)
	require.Equal(t, int64(1), task.Generation)
	require.Contains(t, task.Prompt, "You are the @reviewer")
	require.Contains(t, task.Prompt, "git checkout --detach "+firstCommit)
	require.Contains(t, task.Prompt, "--verdict approve|request_changes|comment --commit "+firstCommit)
	require.Contains(t, task.Prompt, "#1 from the implementer (request, to @reviewer): review")
	_, err = f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.ErrorIs(t, err, ErrMemberBusy)

	first := f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000001", "**Verdict:** request_changes\n\n- `app.go:12` major: handle the error", nil)
	require.Equal(t, "blocked", first.State, "it rests, session kept, until its next turn")
	rested := f.reload("reviewer")
	require.Equal(t, domain.MemberIdle, rested.State)
	require.False(t, rested.VerdictAsked)
	var notices int
	require.NoError(t, f.db.Get(&notices, `SELECT count(*) FROM telegram_deliveries WHERE conversation_id=$1`, *f.task.ConversationID))
	require.Zero(t, notices, "a member resting between turns is not news")

	f.publish(secondCommit)
	f.requestReview()
	inbox, err := f.repo.MemberInbox(f.ctx, f.task.ID, "reviewer")
	require.NoError(t, err)
	require.Len(t, inbox, 1, "its own verdict is not news to it")
	require.Equal(t, int64(3), inbox[0].Seq)

	again, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	require.Equal(t, member.ID, again.ID)
	require.Equal(t, *member.MemberTaskID, *again.MemberTaskID, "one task for every pass")
	require.Equal(t, 2, again.Turns)
	require.Equal(t, int64(3), again.LastReadSeq)
	require.Equal(t, secondCommit, *again.PinnedCommit)
	second := f.memberTask(again)
	require.Equal(t, "queued", second.State)
	require.Equal(t, int64(2), second.Generation)
	require.NotEqual(t, first.RunID, second.RunID)
	require.Contains(t, second.Prompt, "git checkout --detach "+secondCommit)
	require.NotContains(t, second.Prompt, "#1 from", "posts read before are not repeated")

	previous, err := f.repo.PreviousAttempt(f.ctx, second)
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.Equal(t, "succeeded", previous.Status)
	require.Equal(t, "0c9a6c1e-0000-4000-8000-000000000001", previous.NativeSessionID, "the second pass resumes the first one's session")
	require.Equal(t, f.machine, previous.MachineID)
	require.False(t, previous.OtherProfile)

	// The writer reads what is new for it, and its cursor only moves forward.
	writerInbox, err := f.repo.MemberInbox(f.ctx, f.task.ID, "implementer")
	require.NoError(t, err)
	require.Len(t, writerInbox, 1)
	require.Equal(t, domain.RoomVerdict, writerInbox[0].Kind)
	require.NoError(t, f.repo.MarkMemberRead(f.ctx, f.task.ID, "implementer", 3))
	require.NoError(t, f.repo.MarkMemberRead(f.ctx, f.task.ID, "implementer", 1))
	require.Equal(t, int64(3), f.reload("implementer").LastReadSeq)
	writerInbox, err = f.repo.MemberInbox(f.ctx, f.task.ID, "implementer")
	require.NoError(t, err)
	require.Empty(t, writerInbox)
}

// A verdict counts only from the member task of its role, for the full
// commit it names, which must be the commit its turn reads or one Kuayle
// published from the task's attempts (the records of the task's branch).
func TestAVerdictIsBoundToACommitOfTheTaskBranch(t *testing.T) {
	f := newMemberFixture(t)
	f.publish(firstCommit)
	f.publish(secondCommit)
	f.requestReview()
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	require.Equal(t, secondCommit, *member.PinnedCommit, "the newest published commit")

	files := []domain.AgentOutboxFile{
		roomFile("a.json", `{"to":"@all","kind":"verdict","body":"Looks right.","data":{"verdict":"approve","commit":"89abcdef"}}`),
		roomFile("b.json", `{"to":"@all","kind":"verdict","body":"Old one.","data":{"verdict":"request_changes","commit":"`+firstCommit+`"}}`),
		roomFile("c.json", `{"to":"@all","kind":"verdict","data":{"verdict":"approve","commit":"fedcba9876543210"}}`),
		roomFile("d.json", `{"to":"@all","kind":"verdict","data":{"verdict":"approve","commit":"89abcdef","role":"security"}}`),
	}
	f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000002", "Posted my verdict.", func(task domain.AgentTask) {
		_, _, err := f.repo.RecordRoomOutbox(context.Background(), task.ID, files)
		require.NoError(t, err)
	})

	room := f.room()
	require.Len(t, room, 5)
	approve, old, unknown, spoofed := room[1], room[2], room[3], room[4]
	require.Nil(t, approve.RejectReason)
	require.Equal(t, secondCommit, approve.Data.Commit, "an abbreviated commit is stored whole")
	require.Equal(t, "reviewer", approve.Data.Role)
	require.Equal(t, member.ID, *approve.AuthorMemberID)
	require.Nil(t, old.RejectReason, "an older commit of the branch is a commit of the branch")
	require.Equal(t, `commit "fedcba9876543210" is not one Kuayle published on this task's branch`, *unknown.RejectReason)
	require.Equal(t, "a verdict speaks only for its author's role", *spoofed.RejectReason)

	verdicts, err := f.repo.LatestVerdicts(f.ctx, f.task.ID, secondCommit)
	require.NoError(t, err)
	require.Len(t, verdicts, 1)
	require.Equal(t, "approve", verdicts["reviewer"].Verdict)
	require.Equal(t, "Looks right.", verdicts["reviewer"].Findings)
	require.Equal(t, approve.Seq, verdicts["reviewer"].Seq)
	require.Equal(t, approve.RunID, verdicts["reviewer"].RunID)
	verdicts, err = f.repo.LatestVerdicts(f.ctx, f.task.ID, firstCommit)
	require.NoError(t, err)
	require.Equal(t, "request_changes", verdicts["reviewer"].Verdict, "a verdict counts for its own commit only")
	var stored int
	require.NoError(t, f.db.Get(&stored, `SELECT count(*) FROM task_verdicts WHERE task_id=$1`, f.task.ID))
	require.Equal(t, 2, stored, "the final message has no verdict, and the turn gave one: no fallback, no note")

	// A role's name alone is not its member: a verdict comes from the member task.
	named, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("reviewer", domain.RoomVerdict, "", domain.TaskRoomData{Verdict: "approve", Commit: secondCommit}))
	require.NoError(t, err)
	require.Equal(t, "a verdict comes from the member task of its role", *named.RejectReason)
}

// A turn asked for a verdict that ends without one in the room has its
// final message read as the review (the Verdict: line); one that has none
// there either is reported in the room by Kuayle. The machine asks it once
// more before that (MissingVerdict says what is missing).
func TestATurnAskedForAVerdictEndsWithOneOrIsReported(t *testing.T) {
	f := newMemberFixture(t)
	f.publish(firstCommit)
	f.requestReview()
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	task := f.memberTask(member)
	machines := NewDevMachineRepository(f.db)
	f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000003", "I looked at the change but ran out of time.", func(task domain.AgentTask) {
		missing, err := machines.MissingVerdict(context.Background(), task.RunID)
		require.NoError(t, err)
		require.Equal(t, firstCommit, missing, "the turn owes a verdict for its commit")
	})
	missing, err := machines.MissingVerdict(context.Background(), task.RunID)
	require.NoError(t, err)
	require.Empty(t, missing, "the turn is over")
	room := f.room()
	require.Len(t, room, 2)
	note := room[1]
	require.Equal(t, domain.RoomAuthorKuayle, note.AuthorKind)
	require.Equal(t, domain.RoomFact, note.Kind)
	require.Equal(t, domain.NoVerdictNote("reviewer", firstCommit), note.Body)
	require.Equal(t, domain.PipelineFactVerdict, note.Data.Fact)
	var value map[string]string
	require.NoError(t, json.Unmarshal(note.Data.Value, &value))
	require.Equal(t, map[string]string{"role": "reviewer", "commit": firstCommit, "verdict": "none"}, value)
	verdicts, err := f.repo.LatestVerdicts(f.ctx, f.task.ID, firstCommit)
	require.NoError(t, err)
	require.Empty(t, verdicts)
	require.Contains(t, f.memberTask(member).Reason, "without a verdict")
	var wake bool
	require.NoError(t, f.db.Get(&wake, `SELECT workflow_wake_at<=now() FROM agent_tasks WHERE id=$1`, f.task.ID))
	require.True(t, wake, "the end of a member's turn wakes the root task")

	// The fallback: the final message's Verdict: line is the member's verdict.
	f.requestReview()
	member, err = f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000003", "Verdict: approve\n\nNo findings.", nil)
	verdicts, err = f.repo.LatestVerdicts(f.ctx, f.task.ID, firstCommit)
	require.NoError(t, err)
	require.Equal(t, "approve", verdicts["reviewer"].Verdict)
	require.Equal(t, "No findings.", verdicts["reviewer"].Findings)
	posted := f.room()[3]
	require.Equal(t, domain.RoomVerdict, posted.Kind)
	require.Equal(t, "reviewer", *posted.AuthorRole, "posted as the member, from its own answer")
	require.Equal(t, member.ID, *posted.AuthorMemberID)
	require.Equal(t, f.memberTask(member).RunID, *posted.RunID)

	// A turn nobody asked for a verdict owes none.
	_, err = f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomAsk, "Is the retry bounded?", domain.TaskRoomData{}))
	require.NoError(t, err)
	member, err = f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	require.False(t, member.VerdictAsked)
	require.Contains(t, f.memberTask(member).Prompt, "Nobody asked you for a verdict")
	f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000003", "Yes, three attempts.", func(task domain.AgentTask) {
		missing, err := machines.MissingVerdict(context.Background(), task.RunID)
		require.NoError(t, err)
		require.Empty(t, missing, "a turn nobody asked for a verdict owes none")
	})
	require.Len(t, f.room(), 5, "no note for a turn that owed no verdict")
}

// A member's task posts to the room of the root task, as its role and
// member; a task that is no member and runs no pipeline has no room.
func TestAMemberTaskPostsToTheRootRoomAsItsRole(t *testing.T) {
	f := newMemberFixture(t)
	_, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomAsk, "Is v1 still used?", domain.TaskRoomData{}))
	require.NoError(t, err)
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	require.Nil(t, member.PinnedCommit, "nothing is published yet")
	task := f.memberTask(member)
	require.Contains(t, task.Prompt, "Nothing is published yet")

	rest, recorded, err := f.repo.RecordRoomOutbox(context.Background(), task.ID, []domain.AgentOutboxFile{
		roomFile("a.json", `{"to":"@implementer","reply_to":1,"body":"Yes: the CLI calls it."}`),
		roomFile("b.json", `{"to":"ENG-12","body":"for an issue"}`),
	})
	require.NoError(t, err)
	require.Equal(t, 1, recorded)
	require.Len(t, rest, 1)
	room := f.room()
	require.Len(t, room, 2)
	reply := room[1]
	require.Equal(t, f.task.ID, reply.TaskID, "the root task's room")
	require.Equal(t, "reviewer", *reply.AuthorRole)
	require.Equal(t, member.ID, *reply.AuthorMemberID)
	require.Equal(t, task.RunID, *reply.RunID)
	require.Nil(t, reply.RejectReason)
	require.True(t, strings.HasPrefix(reply.Body, "Yes"))

	members, err := f.repo.Members(f.ctx, f.task.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	writer := f.reload("implementer")
	posted, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: "implementer", AuthorMemberID: &member.ID, Kind: domain.RoomSay, Body: "x"})
	require.NoError(t, err)
	require.Equal(t, "the post names another member", *posted.RejectReason)
	_, _, err = f.repo.RecordRoomOutbox(context.Background(), f.task.ID, []domain.AgentOutboxFile{roomFile("c.json", `{"to":"@reviewer","body":"Thanks"}`)})
	require.NoError(t, err)
	require.Equal(t, writer.ID, *f.room()[3].AuthorMemberID, "the root task posts as the writer's member")
}

// A member's task is not a review the root task waits for: the root reaching
// awaiting_review is announced while a member rests between its turns.
func TestAMembersTaskIsNotTheRootsOwnPendingReview(t *testing.T) {
	f := newMemberFixture(t)
	_, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, f.member("implementer", domain.RoomAsk, "Is v1 still used?", domain.TaskRoomData{}))
	require.NoError(t, err)
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000004", "Yes.", nil)

	_, err = f.db.Exec(`UPDATE agent_tasks SET state='publishing' WHERE id=$1`, f.task.ID)
	require.NoError(t, err)
	tx, root, err := f.repo.transaction(f.ctx, f.task.ID, true)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, transitionAgentTask(f.ctx, tx, root, "awaiting_review", "Pull request opened", nil))
	require.NoError(t, tx.Commit())
	var pending bool
	require.NoError(t, f.db.Get(&pending, `SELECT notification_pending FROM agent_task_transitions WHERE task_id=$1 AND to_state='awaiting_review'`, f.task.ID))
	require.True(t, pending)
}

// The root task's workflow view lists the runs of its members' tasks with
// the role each played, so the thread shows every turn; only member tasks of
// the root, of its creator and in its workspace.
func TestTheRootTaskListsItsMembersRunsWithTheirRoles(t *testing.T) {
	f := newMemberFixture(t)
	f.publish(firstCommit)
	f.requestReview()
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	turn := f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000005", "Verdict: approve", nil)

	runs, err := f.repo.StepRuns(f.ctx, f.task.WorkspaceID, f.task.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, f.task.ID, runs[0].TaskID)
	require.Equal(t, "implementer", *runs[0].Role, "the root task plays the writer")
	require.Equal(t, turn.ID, runs[1].TaskID, "its transcript is read through the member task")
	require.Equal(t, turn.RunID, runs[1].ID)
	require.Equal(t, "reviewer", *runs[1].Role)
	require.Nil(t, runs[1].Step)
	require.Equal(t, "succeeded", runs[1].Status)

	_, err = f.repo.StepRuns(f.ctx, uuid.New(), f.task.ID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound, "another workspace")
	other := uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE agent_tasks SET creator_id=$2 WHERE id=$1`, turn.ID, other)
	require.NoError(t, err)
	runs, err = f.repo.StepRuns(f.ctx, f.task.WorkspaceID, f.task.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs, 1, "a task of another creator is not the root's member")
}
