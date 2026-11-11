package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

var memoryTestKey = cryptoutil.DeriveKey("workspace-memory-test")

// enableLessons configures lesson redaction for one test, as the API and the
// machine manager do at startup.
func enableLessons(t *testing.T) {
	t.Helper()
	ConfigureLessonRedaction(memoryTestKey)
	t.Cleanup(func() { lessonRedaction.configured, lessonRedaction.key = false, nil })
}

// memoryFixture is a task in review on fixture/repo, owned by the workspace's
// owner, with an admin, two members, a guest and the owner of another
// workspace.
type memoryFixture struct {
	db                              *sqlx.DB
	tasks                           *AgentTaskRepository
	memory                          *WorkspaceMemoryRepository
	ctx                             context.Context
	task                            domain.AgentTask
	ws, owner, admin, member, guest uuid.UUID
	colleague                       uuid.UUID
	otherWorkspace, otherOwner      uuid.UUID
}

func newMemoryFixture(t *testing.T) memoryFixture {
	t.Helper()
	db, tasks, ctx, task := reviewableTask(t)
	enableLessons(t)
	f := memoryFixture{db: db, tasks: tasks, memory: NewWorkspaceMemoryRepository(db), ctx: ctx, task: task, ws: task.WorkspaceID, owner: task.CreatorID, otherWorkspace: uuid.New()}
	f.admin = addWorkflowMember(t, db, f.ws, "admin")
	f.member = addWorkflowMember(t, db, f.ws, "member")
	f.colleague = addWorkflowMember(t, db, f.ws, "member")
	f.guest = addWorkflowMember(t, db, f.ws, "guest")
	f.otherOwner = uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other owner','hash')`, f.otherOwner, f.otherOwner.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, f.otherWorkspace, f.otherWorkspace.String(), f.otherOwner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, f.otherWorkspace, f.otherOwner)
	require.NoError(t, err)
	return f
}

// prompt is what a task of the fixture's team would receive.
func (f memoryFixture) prompt(t *testing.T) []string {
	t.Helper()
	texts, err := f.memory.ApprovedTexts(context.Background(), f.ws, f.task.TeamID, "Fixture/Repo")
	require.NoError(t, err)
	return texts
}

func (f memoryFixture) lessons(t *testing.T, where string, args ...any) []domain.Lesson {
	t.Helper()
	lessons := []domain.Lesson{}
	require.NoError(t, f.db.Select(&lessons, `SELECT id,workspace_id,repository_full_name,team_id,state,text,proposed_text,source_event,proposer,source_task_id,proposed_by,decided_by,decided_at,position,created_at,updated_at FROM workspace_lessons WHERE `+where+` ORDER BY created_at,id`, args...))
	return lessons
}

// sees lists what an actor reads of the repository, by lesson ID.
func (f memoryFixture) sees(t *testing.T, actor uuid.UUID) map[uuid.UUID]domain.Lesson {
	t.Helper()
	lessons, err := f.memory.List(context.Background(), f.ws, actor, "fixture/repo", "")
	require.NoError(t, err)
	seen := map[uuid.UUID]domain.Lesson{}
	for _, lesson := range lessons {
		seen[lesson.ID] = lesson
	}
	return seen
}

func TestLessonsReachAgentsOnlyThroughAnAdminsDecision(t *testing.T) {
	f := newMemoryFixture(t)
	ctx := context.Background()

	// A member proposes; guests, outsiders and unknown repositories are refused.
	proposal, err := f.memory.Add(ctx, f.ws, f.member, "Fixture/Repo", "  Run `make check`\nbefore pushing. ")
	require.NoError(t, err)
	require.Equal(t, domain.LessonProposed, proposal.State)
	require.Equal(t, "fixture/repo", proposal.Repository, "the connected repository's own name is kept")
	require.Equal(t, "Run `make check` before pushing.", proposal.Text)
	require.Equal(t, domain.LessonEventManual, proposal.SourceEvent)
	require.Equal(t, domain.LessonProposerPerson, proposal.Proposer)
	require.Equal(t, f.member, *proposal.ProposedBy)
	require.Nil(t, proposal.DecidedBy)
	_, err = f.memory.Add(ctx, f.ws, f.guest, "fixture/repo", "Guest lesson")
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.Add(ctx, f.ws, f.otherOwner, "fixture/repo", "Outsider lesson")
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.Add(ctx, f.otherWorkspace, f.otherOwner, "fixture/repo", "Not their repository")
	require.ErrorIs(t, err, ErrLessonRepository)
	_, err = f.memory.Add(ctx, f.ws, f.member, "other/repo", "Unknown repository")
	require.ErrorIs(t, err, ErrLessonRepository)
	_, err = f.memory.Add(ctx, f.ws, f.member, "fixture/repo", strings.Repeat("a", domain.LessonMaxRunes+1))
	require.ErrorIs(t, err, domain.ErrLessonTooLong)
	_, err = f.memory.Add(ctx, f.ws, f.member, "fixture/repo", " \n ​")
	require.ErrorIs(t, err, domain.ErrLessonEmpty)
	_, err = f.memory.Add(ctx, f.ws, f.admin, "fixture/repo", "run `make check` before  pushing.")
	require.ErrorIs(t, err, ErrLessonDuplicate)
	require.Empty(t, f.prompt(t), "a proposal never reaches an agent")

	// Only owners and admins decide; reading needs a non-guest member.
	_, err = f.memory.Approve(ctx, f.ws, f.member, proposal.ID, nil)
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.Drop(ctx, f.ws, f.guest, proposal.ID)
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.Edit(ctx, f.ws, f.member, proposal.ID, &proposal.Text, false)
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.List(ctx, f.ws, f.guest, "", "")
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.List(ctx, f.ws, f.otherOwner, "", "")
	require.ErrorIs(t, err, ErrLessonDenied)
	_, err = f.memory.Events(ctx, f.ws, f.member, proposal.ID)
	require.ErrorIs(t, err, ErrLessonDenied)
	// Another workspace's owner never reaches the lesson through their own.
	_, err = f.memory.Approve(ctx, f.otherWorkspace, f.otherOwner, proposal.ID, nil)
	require.ErrorIs(t, err, ErrLessonNotFound)
	_, err = f.memory.Events(ctx, f.otherWorkspace, f.otherOwner, proposal.ID)
	require.ErrorIs(t, err, ErrLessonNotFound)
	others, err := f.memory.List(ctx, f.otherWorkspace, f.otherOwner, "", "")
	require.NoError(t, err)
	require.Empty(t, others)
	require.Empty(t, f.prompt(t))

	// A waiting proposal is read by its author and by those who decide.
	mine, err := f.memory.List(ctx, f.ws, f.member, "FIXTURE/repo", domain.LessonProposed)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Empty(t, f.sees(t, f.colleague), "another member does not read someone's proposal")
	require.Contains(t, f.sees(t, f.admin), proposal.ID)

	// Approving with edited text: agents see what the person left.
	tooLong := strings.Repeat("b", domain.LessonMaxRunes+1)
	_, err = f.memory.Approve(ctx, f.ws, f.admin, proposal.ID, &tooLong)
	require.ErrorIs(t, err, domain.ErrLessonTooLong)
	edited := "Run `make check` before every push."
	approved, err := f.memory.Approve(ctx, f.ws, f.admin, proposal.ID, &edited)
	require.NoError(t, err)
	require.Equal(t, domain.LessonApproved, approved.State)
	require.Equal(t, edited, approved.Text)
	require.Equal(t, "Run `make check` before pushing.", approved.ProposedText, "what was proposed is kept as written")
	require.Equal(t, f.admin, *approved.DecidedBy)
	require.NotNil(t, approved.DecidedAt)
	require.Equal(t, 1, approved.Position)
	require.Equal(t, []string{edited}, f.prompt(t))
	_, err = f.memory.Approve(ctx, f.ws, f.admin, proposal.ID, nil)
	require.ErrorIs(t, err, ErrLessonState)
	_, err = f.memory.Drop(ctx, f.ws, f.admin, proposal.ID)
	require.ErrorIs(t, err, ErrLessonState, "an approved lesson is retired, not dropped")

	// Every member reads the approved lesson; the original stays with the
	// owners and admins.
	for _, reader := range []uuid.UUID{f.member, f.colleague} {
		lesson, visible := f.sees(t, reader)[proposal.ID]
		require.True(t, visible)
		require.Equal(t, edited, lesson.Text)
		require.Empty(t, lesson.ProposedText, "members never read the text before it was edited")
	}
	require.Equal(t, "Run `make check` before pushing.", f.sees(t, f.owner)[proposal.ID].ProposedText)

	// An approved lesson can be edited; an owner's own lesson is approved at once.
	again := "Run `make check` and `make lint` before every push."
	changed, err := f.memory.Edit(ctx, f.ws, f.owner, proposal.ID, &again, false)
	require.NoError(t, err)
	require.Equal(t, again, changed.Text)
	_, err = f.memory.Edit(ctx, f.ws, f.owner, proposal.ID, &tooLong, false)
	require.ErrorIs(t, err, domain.ErrLessonTooLong)
	second, err := f.memory.Add(ctx, f.ws, f.owner, "fixture/repo", "Renumber migrations after merging main.")
	require.NoError(t, err)
	require.Equal(t, domain.LessonApproved, second.State)
	require.Equal(t, 2, second.Position)
	require.Equal(t, f.owner, *second.DecidedBy)
	require.Equal(t, []string{again, second.Text}, f.prompt(t))

	// Reordering names every approved lesson once.
	_, err = f.memory.Reorder(ctx, f.ws, f.member, "fixture/repo", []uuid.UUID{second.ID, proposal.ID})
	require.ErrorIs(t, err, ErrLessonDenied)
	for _, ids := range [][]uuid.UUID{{second.ID}, {second.ID, second.ID}, {second.ID, proposal.ID, uuid.New()}, {}} {
		_, err = f.memory.Reorder(ctx, f.ws, f.admin, "fixture/repo", ids)
		require.ErrorIs(t, err, ErrLessonOrder)
	}
	ordered, err := f.memory.Reorder(ctx, f.ws, f.admin, "Fixture/Repo", []uuid.UUID{second.ID, proposal.ID})
	require.NoError(t, err)
	require.Len(t, ordered, 2)
	require.Equal(t, second.ID, ordered[0].ID)
	require.Equal(t, []string{second.Text, again}, f.prompt(t))

	// Retired and dropped lessons never reach an agent again.
	retired, err := f.memory.Retire(ctx, f.ws, f.admin, proposal.ID)
	require.NoError(t, err)
	require.Equal(t, domain.LessonRetired, retired.State)
	require.Equal(t, []string{second.Text}, f.prompt(t))
	require.NotContains(t, f.sees(t, f.colleague), proposal.ID, "members read approved lessons only")
	_, err = f.memory.Retire(ctx, f.ws, f.admin, proposal.ID)
	require.ErrorIs(t, err, ErrLessonState)
	_, err = f.memory.Edit(ctx, f.ws, f.admin, proposal.ID, &edited, false)
	require.ErrorIs(t, err, ErrLessonState)
	_, err = f.memory.Approve(ctx, f.ws, f.admin, proposal.ID, nil)
	require.ErrorIs(t, err, ErrLessonState)
	unwanted, err := f.memory.Add(ctx, f.ws, f.member, "fixture/repo", "Always force-push.")
	require.NoError(t, err)
	dropped, err := f.memory.Drop(ctx, f.ws, f.owner, unwanted.ID)
	require.NoError(t, err)
	require.Equal(t, domain.LessonDropped, dropped.State)
	require.Equal(t, f.owner, *dropped.DecidedBy)
	_, err = f.memory.Approve(ctx, f.ws, f.owner, unwanted.ID, nil)
	require.ErrorIs(t, err, ErrLessonState)
	_, err = f.memory.Add(ctx, f.ws, f.member, "fixture/repo", "Always force-push.")
	require.ErrorIs(t, err, ErrLessonDuplicate, "a dropped lesson is not proposed again")
	require.Equal(t, []string{second.Text}, f.prompt(t))

	// Every decision is audited, newest first, with who made it.
	events, err := f.memory.Events(ctx, f.ws, f.admin, proposal.ID)
	require.NoError(t, err)
	actions, actors := []string{}, []uuid.UUID{}
	for _, event := range events {
		actions = append(actions, event.Action)
		actors = append(actors, *event.ActorID)
	}
	require.Equal(t, []string{"retired", "reordered", "edited", "approved", "proposed"}, actions)
	require.Equal(t, []uuid.UUID{f.admin, f.admin, f.owner, f.admin, f.member}, actors)
	require.JSONEq(t, fmt.Sprintf(`{"from":%q,"to":%q}`, edited, again), string(events[2].Detail))
	events, err = f.memory.Events(ctx, f.ws, f.owner, unwanted.ID)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "dropped", events[0].Action)
	events, err = f.memory.Events(ctx, f.ws, f.owner, second.ID)
	require.NoError(t, err)
	require.Equal(t, "reordered", events[0].Action)
	require.Equal(t, "added", events[len(events)-1].Action)
}

func TestLessonsAreBoundedPerRepositoryAndMember(t *testing.T) {
	f := newMemoryFixture(t)
	ctx := context.Background()
	waiting, err := f.memory.Add(ctx, f.ws, f.member, "fixture/repo", "Waiting for a decision")
	require.NoError(t, err)
	for i := range domain.LessonsApprovedPerRepository {
		_, err = f.memory.Add(ctx, f.ws, f.owner, "fixture/repo", fmt.Sprintf("Lesson %d", i))
		require.NoError(t, err)
	}
	_, err = f.memory.Add(ctx, f.ws, f.owner, "fixture/repo", "One too many")
	require.ErrorIs(t, err, ErrLessonLimit)
	_, err = f.memory.Approve(ctx, f.ws, f.admin, waiting.ID, nil)
	require.ErrorIs(t, err, ErrLessonLimit)
	require.Len(t, f.prompt(t), domain.LessonsApprovedPerRepository)
	listed, err := f.memory.List(ctx, f.ws, f.member, "fixture/repo", domain.LessonApproved)
	require.NoError(t, err)
	require.Equal(t, "Lesson 0", listed[0].Text, "approved lessons are listed in prompt order")
	// Retiring one makes room.
	_, err = f.memory.Retire(ctx, f.ws, f.admin, listed[0].ID)
	require.NoError(t, err)
	approved, err := f.memory.Approve(ctx, f.ws, f.admin, waiting.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.LessonsApprovedPerRepository+1, approved.Position, "a newly approved lesson comes last")

	// One member cannot fill the queue: a few waiting proposals each.
	for i := range domain.LessonsPendingPerMember {
		_, err = f.memory.Add(ctx, f.ws, f.member, "fixture/repo", fmt.Sprintf("My proposal %d", i))
		require.NoError(t, err)
	}
	_, err = f.memory.Add(ctx, f.ws, f.member, "fixture/repo", "One proposal too many")
	require.ErrorIs(t, err, ErrLessonMemberLimit)
	_, err = f.memory.Add(ctx, f.ws, f.colleague, "fixture/repo", "A colleague still proposes")
	require.NoError(t, err)

	// Proposals waiting on a repository are bounded too.
	_, err = f.db.Exec(`INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,text,proposed_text,source_event,proposer,dedupe_key)
 SELECT $1,'fixture/repo','fixture/repo','Pending '||n,'Pending '||n,'review','external',lpad(n::text,64,'0') FROM generate_series(1,$2::int) n`, f.ws, domain.LessonsPendingPerRepository)
	require.NoError(t, err)
	_, err = f.memory.Add(ctx, f.ws, f.colleague, "fixture/repo", "Too many are waiting")
	require.ErrorIs(t, err, ErrLessonPendingLimit)
	recorded, err := NewAgentMessageRepository(f.db).RecordTaskOutbox(ctx, f.task.ID, []domain.AgentOutboxFile{outbox("full.json", `{"to":"memory","lesson":"Too many are waiting"}`)})
	require.NoError(t, err)
	require.Equal(t, 1, recorded, "the refusal is recorded")
	require.Empty(t, f.lessons(t, `source_task_id=$1`, f.task.ID))
	var reason string
	require.NoError(t, f.db.Get(&reason, `SELECT reject_reason FROM agent_messages WHERE from_task_id=$1 AND to_ref='MEMORY'`, f.task.ID))
	require.Equal(t, lessonPendingLimit, reason)
}

func TestOnlyReviewsThatRequestedChangesProposeFromTaskEvents(t *testing.T) {
	f := newMemoryFixture(t)
	background := context.Background()

	// CI results propose nothing: a check's name is no lesson.
	for id, outcome := range map[string]string{"1": "success", "2": "failure", "3": "timed_out"} {
		_, err := f.tasks.RecordSignal(background, f.ws, "fixture/repo", 12, domain.AgentTaskSignalInput{Kind: "ci", ExternalID: id, Outcome: outcome, Name: "Tests", Summary: "3 of 9 tests failed", HeadSHA: publishedCommit})
		require.NoError(t, err)
	}
	// Neither does what a person tells the agent or writes on the issue: their
	// messages stay between them and their task.
	_, err := f.tasks.QueueLiveMessage(f.ctx, f.ws, f.task.ID, domain.AgentLiveSenderUser, "Use the v2 route, the client told me in private")
	require.NoError(t, err)
	_, err = f.tasks.RecordCommentSignal(background, f.ws, f.task.IssueID, uuid.New(), f.owner, "Please rename the flag")
	require.NoError(t, err)
	require.Empty(t, f.lessons(t, `workspace_id=$1`, f.ws))

	// A GitHub change request proposes when its author is trusted, once, and
	// is marked as text from outside the workspace.
	review := func(id string, trusted bool) {
		t.Helper()
		_, err := f.tasks.RecordSignal(background, f.ws, "fixture/repo", 12, domain.AgentTaskSignalInput{Kind: "review", ExternalID: id, Outcome: "changes_requested", Name: "reviewer", Summary: "Please keep handlers‮ thin", Trusted: trusted})
		require.NoError(t, err)
	}
	review("5", false)
	require.Empty(t, f.lessons(t, `source_event='review'`), "an untrusted reviewer proposes nothing")
	review("6", true)
	review("7", true)
	fromReview := f.lessons(t, `source_event='review'`)
	require.Len(t, fromReview, 1)
	proposal := fromReview[0]
	require.Equal(t, "Please keep handlers thin", proposal.Text, "invisible characters are removed")
	require.Equal(t, domain.LessonProposerExternal, proposal.Proposer)
	require.Equal(t, domain.LessonProposed, proposal.State)
	require.Equal(t, f.task.ID, *proposal.SourceTaskID)
	require.Equal(t, f.task.TeamID, *proposal.TeamID, "a task's lesson keeps the task's team")
	require.Nil(t, proposal.ProposedBy)
	require.Equal(t, "fixture/repo", proposal.Repository)
	var audited int
	require.NoError(t, f.db.Get(&audited, `SELECT count(*) FROM workspace_lesson_events WHERE lesson_id=$1 AND action='proposed'`, proposal.ID))
	require.Equal(t, 1, audited)

	// Until it is approved only the task's creator and those who decide read it.
	require.Contains(t, f.sees(t, f.owner), proposal.ID, "the task's creator, here also the owner")
	require.Contains(t, f.sees(t, f.admin), proposal.ID)
	require.Empty(t, f.sees(t, f.member), "other members do not read a task's proposal")
	_, err = f.db.Exec(`UPDATE agent_tasks SET creator_id=$2 WHERE id=$1`, f.task.ID, f.member)
	require.NoError(t, err)
	require.Contains(t, f.sees(t, f.member), proposal.ID, "a member reads the proposals of their own task")
	require.Empty(t, f.sees(t, f.colleague))
	require.Empty(t, f.prompt(t), "nothing reaches agents without approval")

	// Dropped: it stays hidden from members and is not proposed again.
	_, err = f.memory.Drop(background, f.ws, f.admin, proposal.ID)
	require.NoError(t, err)
	require.Empty(t, f.sees(t, f.member))
	_, err = f.db.Exec(`UPDATE agent_tasks SET creator_id=$2 WHERE id=$1`, f.task.ID, f.owner)
	require.NoError(t, err)
	review("8", true)
	require.Len(t, f.lessons(t, `source_event='review'`), 1)
	require.Empty(t, f.prompt(t))
}

func TestAgentsProposeLessonsThroughTheOutbox(t *testing.T) {
	f := newMemoryFixture(t)
	ctx := context.Background()
	messages := NewAgentMessageRepository(f.db)
	record := func(task uuid.UUID, files ...domain.AgentOutboxFile) int {
		t.Helper()
		recorded, err := messages.RecordTaskOutbox(ctx, task, files)
		require.NoError(t, err)
		return recorded
	}
	refusals := func(task uuid.UUID) []string {
		t.Helper()
		reasons := []string{}
		require.NoError(t, f.db.Select(&reasons, `SELECT reject_reason FROM agent_messages WHERE from_task_id=$1 AND to_ref='MEMORY' AND state='rejected' ORDER BY created_at,id`, task))
		return reasons
	}

	require.Equal(t, 1, record(f.task.ID, outbox("1.json", `{"to":"memory","lesson":"Renumber migrations\nafter merging​ main."}`)))
	proposed := f.lessons(t, `source_task_id=$1`, f.task.ID)
	require.Len(t, proposed, 1)
	require.Equal(t, "Renumber migrations after merging main.", proposed[0].Text)
	require.Equal(t, domain.LessonProposed, proposed[0].State)
	require.Equal(t, domain.LessonEventTask, proposed[0].SourceEvent)
	require.Equal(t, domain.LessonProposerAgent, proposed[0].Proposer, "the text is marked as written by an agent")
	require.Nil(t, proposed[0].ProposedBy)
	require.Nil(t, proposed[0].DecidedBy)
	require.Equal(t, "fixture/repo", proposed[0].Repository)
	require.Empty(t, f.prompt(t), "an agent's lesson reaches no agent until a person approves it")
	require.Empty(t, f.sees(t, f.member), "nor any member other than the task's creator")
	require.Contains(t, f.sees(t, f.owner), proposed[0].ID)
	var messagesRecorded int
	require.NoError(t, f.db.Get(&messagesRecorded, `SELECT count(*) FROM agent_messages WHERE from_task_id=$1`, f.task.ID))
	require.Zero(t, messagesRecorded, "a lesson is a proposal, not a message to another agent")

	// The same file, or the same lesson in another file, is proposed once.
	require.Zero(t, record(f.task.ID, outbox("1.json", `{"to":"memory","lesson":"Renumber migrations\nafter merging​ main."}`)))
	require.Zero(t, record(f.task.ID, outbox("2.json", `{"to":"MEMORY","lesson":"renumber migrations after merging  main."}`)))
	require.Len(t, f.lessons(t, `source_task_id=$1`, f.task.ID), 1)

	// Oversized and malformed lessons are refused and recorded as refusals.
	require.Equal(t, 3, record(f.task.ID,
		outbox("3.json", `{"to":"memory","lesson":"`+strings.Repeat("a", domain.LessonMaxRunes+1)+`"}`),
		outbox("4.json", `{"to":"memory","body":"wrong field"}`),
		outbox("5.json", `{"to":"memory","lesson":"x","channel":"internal"}`)))
	require.Equal(t, 1, record(f.task.ID, domain.AgentOutboxFile{Worktree: "/workspace/tasks/rec-1", Name: "6.json", Data: []byte(`{"to":"memory","lesson":"cut`), Oversize: true}))
	require.Len(t, f.lessons(t, `source_task_id=$1`, f.task.ID), 1)
	reasons := refusals(f.task.ID)
	require.Len(t, reasons, 3)
	require.Equal(t, domain.ErrLessonTooLong.Error(), reasons[0])
	var tooLarge int
	require.NoError(t, f.db.Get(&tooLarge, `SELECT count(*) FROM agent_messages WHERE from_task_id=$1 AND reject_reason='message file is too large'`, f.task.ID))
	require.Equal(t, 1, tooLarge)

	// A task proposes a few lessons; more are refused.
	files := []domain.AgentOutboxFile{}
	for i := range domain.LessonProposalsPerTaskEvent + 1 {
		files = append(files, outbox(fmt.Sprintf("more-%d.json", i), fmt.Sprintf(`{"to":"memory","lesson":"Lesson number %d"}`, i)))
	}
	require.Equal(t, len(files), record(f.task.ID, files...))
	require.Len(t, f.lessons(t, `source_task_id=$1`, f.task.ID), domain.LessonProposalsPerTaskEvent)
	require.Len(t, refusals(f.task.ID), 5, "the lessons over the task's limit are refused")
	require.Equal(t, lessonTaskLimit, refusals(f.task.ID)[4])

	// A review task reviews; it does not write the memory.
	review, _, err := f.tasks.CreateReview(f.ctx, f.ws, *f.task.ConversationID, f.task.ID, reviewRequest("claude-code"))
	require.NoError(t, err)
	require.Equal(t, 1, record(review.ID, outbox("review.json", `{"to":"memory","lesson":"From a review task"}`)))
	require.Equal(t, []string{"the sending task can no longer send messages"}, refusals(review.ID))
	require.Empty(t, f.lessons(t, `text='From a review task'`))

	// A finished task proposes nothing more.
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, f.task.ID)
	require.NoError(t, err)
	_, err = f.db.Exec(`DELETE FROM workspace_lessons WHERE source_task_id=$1`, f.task.ID)
	require.NoError(t, err)
	require.Equal(t, 1, record(f.task.ID, outbox("late.json", `{"to":"memory","lesson":"Too late"}`)))
	require.Empty(t, f.lessons(t, `source_task_id=$1`, f.task.ID))
	require.Empty(t, f.prompt(t))
}

// A proposal that cannot be recorded never breaks the outbox it came in.
func TestAFailingLessonProposalDoesNotLoseTheOutbox(t *testing.T) {
	f := newMemoryFixture(t)
	ctx := context.Background()
	// Nothing configured how to redact lessons, so storing one fails.
	lessonRedaction.configured = false
	recorded, err := NewAgentMessageRepository(f.db).RecordTaskOutbox(ctx, f.task.ID, []domain.AgentOutboxFile{
		outbox("1.json", `{"to":"memory","lesson":"Cannot be stored now"}`),
		outbox("2.json", `{"to":"REC-9","body":"The rest of the batch"}`),
		outbox("3.json", `{"to":"orchestrator","body":"Still delivered"}`),
	})
	require.NoError(t, err, "the batch is recorded")
	require.Equal(t, 3, recorded)
	require.Empty(t, f.lessons(t, `workspace_id=$1`, f.ws), "unredacted text is never stored as a lesson")
	var reason string
	require.NoError(t, f.db.Get(&reason, `SELECT reject_reason FROM agent_messages WHERE from_task_id=$1 AND to_ref='MEMORY'`, f.task.ID))
	require.Equal(t, lessonUnavailable, reason)
	var others int
	require.NoError(t, f.db.Get(&others, `SELECT (SELECT count(*) FROM agent_messages WHERE from_task_id=$1 AND to_ref='REC-9')+(SELECT count(*) FROM agent_live_messages WHERE task_id=$1 AND direction='from_agent')`, f.task.ID))
	require.Equal(t, 2, others, "the other files of the batch are kept")
	_, err = f.memory.Add(ctx, f.ws, f.owner, "fixture/repo", "By hand")
	require.ErrorIs(t, err, ErrLessonRedaction)

	// The same holds for a review's change request.
	_, err = f.tasks.RecordSignal(ctx, f.ws, "fixture/repo", 12, domain.AgentTaskSignalInput{Kind: "review", ExternalID: "1", Outcome: "changes_requested", Name: "reviewer", Summary: "Keep handlers thin", Trusted: true})
	require.NoError(t, err)
	signals, err := f.tasks.Signals(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Len(t, signals, 1, "the signal is recorded without its lesson")
	require.Empty(t, f.lessons(t, `workspace_id=$1`, f.ws))
}

func TestLessonTextIsRedactedBeforeItIsStored(t *testing.T) {
	f := newMemoryFixture(t)
	ctx := context.Background()
	// The task's machine holds a secret; run output is redacted with it.
	var machine uuid.UUID
	require.NoError(t, f.db.Get(&machine, `SELECT machine_id FROM agent_tasks WHERE id=$1`, f.task.ID))
	sealed, err := cryptoutil.Encrypt("hunter2-deploy-token", memoryTestKey)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_env_vars(machine_id,target_service,name,encrypted_value) VALUES($1,'agent','DEPLOY_TOKEN',$2)`, machine, sealed)
	require.NoError(t, err)
	token := "ghp_" + strings.Repeat("a", 36)
	stored := func() string {
		t.Helper()
		var all string
		require.NoError(t, f.db.Get(&all, `SELECT COALESCE((SELECT string_agg(text||' '||proposed_text,' ') FROM workspace_lessons WHERE workspace_id=$1),'')||' '||COALESCE((SELECT string_agg(detail::text,' ') FROM workspace_lesson_events WHERE workspace_id=$1),'')`, f.ws))
		return all
	}

	// An agent's lesson: the machine's secret and a token format are removed
	// from the lesson, the original and the audit trail.
	recorded, err := NewAgentMessageRepository(f.db).RecordTaskOutbox(ctx, f.task.ID, []domain.AgentOutboxFile{outbox("1.json", `{"to":"memory","lesson":"Deploy with hunter2-deploy-token and push as `+token+`."}`)})
	require.NoError(t, err)
	require.Equal(t, 1, recorded)
	fromAgent := f.lessons(t, `source_task_id=$1`, f.task.ID)
	require.Len(t, fromAgent, 1)
	require.Equal(t, "Deploy with [REDACTED] and push as [REDACTED].", fromAgent[0].Text)
	require.Equal(t, fromAgent[0].Text, fromAgent[0].ProposedText)

	// A person's edit of that lesson is redacted with the same secrets, and a
	// lesson written by hand with the credential formats.
	edit := "Deploy with hunter2-deploy-token, never by hand."
	approved, err := f.memory.Approve(ctx, f.ws, f.admin, fromAgent[0].ID, &edit)
	require.NoError(t, err)
	require.Equal(t, "Deploy with [REDACTED], never by hand.", approved.Text)
	again := "Deploy with the hunter2-deploy-token only."
	changed, err := f.memory.Edit(ctx, f.ws, f.admin, fromAgent[0].ID, &again, false)
	require.NoError(t, err)
	require.Equal(t, "Deploy with the [REDACTED] only.", changed.Text)
	byHand, err := f.memory.Add(ctx, f.ws, f.member, "fixture/repo", "Clone from https://bot:s3cr3tpass@github.com/fixture/repo.git with "+token)
	require.NoError(t, err)
	require.Equal(t, "Clone from https://bot:[REDACTED]@github.com/fixture/repo.git with [REDACTED]", byHand.Text)

	all := stored()
	for _, secret := range []string{"hunter2", token, "s3cr3tpass"} {
		require.NotContains(t, all, secret)
	}
	require.Contains(t, all, "[REDACTED]")
	require.Equal(t, []string{"Deploy with the [REDACTED] only."}, f.prompt(t))
}

func TestPrivateTeamLessonsStayWithTheTeam(t *testing.T) {
	f := newMessageFixture(t)
	enableLessons(t)
	ctx := context.Background()
	memory := NewWorkspaceMemoryRepository(f.db)
	var privateTeam uuid.UUID
	require.NoError(t, f.db.Get(&privateTeam, `SELECT id FROM teams WHERE workspace_id=$1 AND key='PRV'`, f.workspace))
	// A team that is public now and becomes private later.
	later := uuid.New()
	_, err := f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Later','LAT')`, later, f.workspace)
	require.NoError(t, err)
	insert := func(team *uuid.UUID, state, text string, position int) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		require.NoError(t, f.db.Get(&id, `INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,team_id,state,text,proposed_text,source_event,proposer,decided_at,position,dedupe_key)
 VALUES($1,'acme/app','acme/app',$2,$3::text,$4,$4,'review','agent',CASE WHEN $3::text<>'proposed' THEN now() END,$5,$6) RETURNING id`, f.workspace, team, state, text, position, domain.LessonDedupeKey("review", text)))
		return id
	}
	shared := insert(nil, domain.LessonApproved, "Workspace lesson", 1)
	secret := insert(&privateTeam, domain.LessonApproved, "Private team lesson", 2)
	public := insert(&later, domain.LessonApproved, "Public team lesson", 3)
	pending := insert(&privateTeam, domain.LessonProposed, "Private team proposal", 0)

	ids := func(actor uuid.UUID) []uuid.UUID {
		t.Helper()
		lessons, err := memory.List(ctx, f.workspace, actor, "acme/app", "")
		require.NoError(t, err)
		out := []uuid.UUID{}
		for _, lesson := range lessons {
			out = append(out, lesson.ID)
		}
		return out
	}
	texts := func(team uuid.UUID) []string {
		t.Helper()
		texts, err := memory.ApprovedTexts(ctx, f.workspace, team, "acme/app")
		require.NoError(t, err)
		return texts
	}
	require.ElementsMatch(t, []uuid.UUID{shared, public}, ids(f.member), "a member outside the private team does not see its lessons")
	require.ElementsMatch(t, []uuid.UUID{shared, secret, public, pending}, ids(f.user))

	// Only the private team's tasks receive its lesson; a public team's lesson
	// reaches every task on the repository.
	require.Equal(t, []string{"Workspace lesson", "Public team lesson"}, texts(f.team))
	require.Equal(t, []string{"Workspace lesson", "Private team lesson", "Public team lesson"}, texts(privateTeam))
	other, err := memory.ApprovedTexts(ctx, uuid.New(), privateTeam, "acme/app")
	require.NoError(t, err)
	require.Empty(t, other, "another workspace receives nothing")

	// A team made private later takes its lessons with it, at once.
	_, err = f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, later)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{shared}, ids(f.member))
	require.Equal(t, []string{"Workspace lesson"}, texts(f.team))
	require.Equal(t, []string{"Workspace lesson", "Public team lesson"}, texts(later))

	// An owner may share a team's lesson with the whole workspace; that is audited.
	_, err = memory.Edit(ctx, f.workspace, f.member, secret, nil, true)
	require.ErrorIs(t, err, ErrLessonDenied)
	sharedNow, err := memory.Edit(ctx, f.workspace, f.user, secret, nil, true)
	require.NoError(t, err)
	require.Nil(t, sharedNow.TeamID)
	require.ElementsMatch(t, []uuid.UUID{shared, secret}, ids(f.member))
	require.Equal(t, []string{"Workspace lesson", "Private team lesson"}, texts(f.team))
	events, err := memory.Events(ctx, f.workspace, f.user, secret)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "shared", events[0].Action)

	// A task without a repository checkout cannot propose a lesson.
	recorded, err := f.messages.RecordTaskOutbox(ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("memory.json", `{"to":"memory","lesson":"No checkout yet"}`)})
	require.NoError(t, err)
	require.Equal(t, 1, recorded)
	var reason string
	require.NoError(t, f.db.Get(&reason, `SELECT reject_reason FROM agent_messages WHERE from_task_id=$1 AND to_ref='MEMORY'`, f.sender.ID))
	require.Equal(t, lessonNoRepository, reason)
}
