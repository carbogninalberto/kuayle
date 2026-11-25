package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// roleMemoryFixture is a pipeline task (writer: implementer, plus a
// reviewer) and a task of the same creator that runs no pipeline, both on
// fixture/repo, with lessons enabled.
type roleMemoryFixture struct {
	*memberFixture
	memory *WorkspaceMemoryRepository
	repoID uuid.UUID
}

func newRoleMemoryFixture(t *testing.T) *roleMemoryFixture {
	t.Helper()
	f := &roleMemoryFixture{memberFixture: newMemberFixture(t)}
	enableLessons(t)
	f.memory = NewWorkspaceMemoryRepository(f.db)
	installation := uuid.New()
	f.repoID = uuid.New()
	_, err := f.db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/repo','main')`, f.repoID, installation, f.task.WorkspaceID)
	require.NoError(t, err)
	f.checkout(f.task)
	f.checkout(f.plain)
	return f
}

// checkout gives a task its worktree of fixture/repo on the machine.
func (f *roleMemoryFixture) checkout(task domain.AgentTask) {
	f.t.Helper()
	_, err := f.db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key)
 VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/rec-1',$6,'ready',$7)`, task.CheckoutID, task.WorkspaceID, f.machine, task.IssueID, f.repoID, "/workspace/tasks/"+task.ID.String(), task.ID.String())
	require.NoError(f.t, err)
}

// propose records one MEMORY outbox file of a task and returns the lesson
// it proposed.
func (f *roleMemoryFixture) propose(task uuid.UUID, lesson string) domain.Lesson {
	f.t.Helper()
	recorded, err := NewAgentMessageRepository(f.db).RecordTaskOutbox(context.Background(), task, []domain.AgentOutboxFile{outbox(uuid.NewString()+".json", `{"to":"memory","lesson":"`+lesson+`"}`)})
	require.NoError(f.t, err)
	require.Equal(f.t, 1, recorded)
	var proposed domain.Lesson
	require.NoError(f.t, f.db.Get(&proposed, `SELECT `+lessonColumns+` FROM workspace_lessons l WHERE l.source_task_id=$1 AND l.text=$2`, task, lesson))
	return proposed
}

// prompt is what a task of the fixture's team playing role (or none) reads.
func (f *roleMemoryFixture) prompt(role string) []string {
	f.t.Helper()
	texts, err := f.memory.ApprovedTexts(context.Background(), f.task.WorkspaceID, f.task.TeamID, "Fixture/Repo", role)
	require.NoError(f.t, err)
	return texts
}

// A MEMORY line from a member of a pipeline task proposes a lesson for the
// role it plays: the root task's for the writer's role, a member task's for
// its own. A task that runs no pipeline proposes for every task, as before.
// Each is still only a proposal until an admin approves it.
func TestAPipelineMembersLessonIsForItsRole(t *testing.T) {
	f := newRoleMemoryFixture(t)
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	reviewer := f.memberTask(member)
	f.checkout(reviewer)

	fromWriter := f.propose(f.task.ID, "Run make check before pushing.")
	fromReviewer := f.propose(reviewer.ID, "Check that every migration has a down file.")
	fromPlain := f.propose(f.plain.ID, "The API listens on port 8080.")
	require.Equal(t, "implementer", *fromWriter.Role)
	require.Equal(t, "reviewer", *fromReviewer.Role)
	require.Nil(t, fromPlain.Role, "a task without a pipeline proposes for every task")
	for _, lesson := range []domain.Lesson{fromWriter, fromReviewer, fromPlain} {
		require.Equal(t, domain.LessonProposed, lesson.State, "still a proposal for an admin")
		require.Equal(t, domain.LessonProposerAgent, lesson.Proposer)
	}
	require.Equal(t, reviewer.ID, *fromReviewer.SourceTaskID)
	for _, role := range []string{"", "implementer", "reviewer"} {
		require.Empty(t, f.prompt(role), "nothing reaches an agent before approval")
	}

	// The same text is a lesson of each role it is proposed for, and is not
	// proposed again for a role an approved lesson already reaches.
	again := f.propose(reviewer.ID, "Run make check before pushing.")
	require.Equal(t, "reviewer", *again.Role)
	_, err = f.memory.Approve(context.Background(), f.task.WorkspaceID, f.task.CreatorID, fromPlain.ID, nil)
	require.NoError(t, err)
	recorded, err := NewAgentMessageRepository(f.db).RecordTaskOutbox(context.Background(), reviewer.ID, []domain.AgentOutboxFile{outbox("known.json", `{"to":"memory","lesson":"The API listens on port 8080."}`)})
	require.NoError(t, err)
	require.Zero(t, recorded, "an approved lesson for every task already reaches the reviewer")

	// The admins see the role of each proposal.
	listed, err := f.memory.List(context.Background(), f.task.WorkspaceID, f.task.CreatorID, "fixture/repo", domain.LessonProposed)
	require.NoError(t, err)
	roles := []string{}
	for _, lesson := range listed {
		require.NotNil(t, lesson.Role)
		roles = append(roles, *lesson.Role+": "+lesson.Text)
	}
	require.ElementsMatch(t, []string{"implementer: Run make check before pushing.", "reviewer: Check that every migration has a down file.", "reviewer: Run make check before pushing."}, roles)
}

// An approved lesson for a role reaches the tasks that play it, with the
// lessons for every task; other roles and tasks without a pipeline read
// only the lessons for every task, as before. Another workspace's lessons
// never reach a role here.
func TestARolesLessonsReachOnlyThatRole(t *testing.T) {
	f := newRoleMemoryFixture(t)
	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, "reviewer", false)
	require.NoError(t, err)
	reviewer := f.memberTask(member)
	f.checkout(reviewer)
	approve := func(lesson domain.Lesson) {
		t.Helper()
		_, err := f.memory.Approve(context.Background(), f.task.WorkspaceID, f.task.CreatorID, lesson.ID, nil)
		require.NoError(t, err)
	}
	approve(f.propose(f.plain.ID, "The API listens on port 8080."))
	approve(f.propose(reviewer.ID, "Check that every migration has a down file."))
	approve(f.propose(f.task.ID, "Run make check before pushing."))

	// Another workspace's lesson for the same repository and role.
	other, otherOwner := uuid.New(), uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, otherOwner, otherOwner.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, other, other.String(), otherOwner)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,role,state,text,proposed_text,source_event,proposer,proposed_by,decided_by,decided_at,position,dedupe_key)
 VALUES($1,'fixture/repo','fixture/repo','reviewer','approved','Foreign lesson','Foreign lesson','manual','person',$2,$2,now(),1,$3)`, other, otherOwner, domain.LessonDedupeKey(domain.LessonEventManual, "Foreign lesson"))
	require.NoError(t, err)

	require.Equal(t, []string{"The API listens on port 8080."}, f.prompt(""), "a task without a role, as before")
	require.Equal(t, []string{"The API listens on port 8080.", "Check that every migration has a down file."}, f.prompt("reviewer"))
	require.Equal(t, []string{"The API listens on port 8080.", "Run make check before pushing."}, f.prompt("implementer"))
	require.Equal(t, []string{"The API listens on port 8080."}, f.prompt("security"), "another role reads none of them")

	// The role a task plays: the writer's for the root task, the member's
	// for its task, none for any other task or another workspace's view.
	for task, want := range map[uuid.UUID]string{f.task.ID: "implementer", reviewer.ID: "reviewer", f.plain.ID: ""} {
		var row domain.AgentTask
		require.NoError(t, f.db.Get(&row, `SELECT * FROM agent_tasks WHERE id=$1`, task))
		role, err := f.memory.TaskRole(context.Background(), row)
		require.NoError(t, err)
		require.Equal(t, want, role)
		row.WorkspaceID = other
		role, err = f.memory.TaskRole(context.Background(), row)
		require.NoError(t, err)
		require.Empty(t, role, "a task is looked up in its own workspace only")
	}
	role, err := f.memory.TaskRole(context.Background(), domain.AgentTask{ID: uuid.New(), WorkspaceID: f.task.WorkspaceID, Kind: domain.AgentTaskKindReview})
	require.NoError(t, err)
	require.Empty(t, role, "a review that is no member's plays no role")
}

// With several writers (KUA-289) the root task plays the writer holding
// its write lock: that writer's turn reads the lessons for its role, and a
// MEMORY line of the turn proposes for it, wherever the lock moves.
func TestTheRootTaskPlaysTheWriterHoldingTheWriteLock(t *testing.T) {
	f := newRoleMemoryFixture(t)
	_, err := f.repo.Members(f.ctx, f.task.ID)
	require.NoError(t, err)
	implementer := f.reload("implementer")
	hold := func(role string) {
		t.Helper()
		tx, err := f.db.Beginx()
		require.NoError(t, err)
		defer tx.Rollback()
		var root domain.AgentTask
		require.NoError(t, tx.Get(&root, `SELECT * FROM agent_tasks WHERE id=$1`, f.task.ID))
		require.NoError(t, holdWriteLock(f.ctx, tx, &root, domain.PipelineMember{Role: role, ProfileID: implementer.ProfileID, Writer: true}))
		require.NoError(t, tx.Commit())
	}
	role := func() string {
		t.Helper()
		var root domain.AgentTask
		require.NoError(t, f.db.Get(&root, `SELECT * FROM agent_tasks WHERE id=$1`, f.task.ID))
		role, err := f.memory.TaskRole(context.Background(), root)
		require.NoError(t, err)
		return role
	}

	hold("styler")
	require.Equal(t, "styler", role())
	require.Equal(t, "styler", *f.propose(f.task.ID, "Use the design tokens.").Role)
	hold("implementer")
	require.Equal(t, "implementer", role(), "the lock moved back")
	require.Equal(t, "implementer", *f.propose(f.task.ID, "Run make check before pushing.").Role)
}
