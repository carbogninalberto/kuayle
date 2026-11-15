package repository

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

type undoFixture struct {
	db                      *sqlx.DB
	repo                    *IssueRepository
	owner, outsider         uuid.UUID
	ws, team, privateTeam   uuid.UUID
	todo, done, privateTodo uuid.UUID
	label                   uuid.UUID
	ctx, outsiderCtx        context.Context
}

func newUndoFixture(t *testing.T) *undoFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	f := &undoFixture{db: db, repo: NewIssueRepository(db),
		owner: uuid.New(), outsider: uuid.New(), ws: uuid.New(), team: uuid.New(), privateTeam: uuid.New(),
		todo: uuid.New(), done: uuid.New(), privateTodo: uuid.New(), label: uuid.New()}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	for _, user := range []uuid.UUID{f.owner, f.outsider} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Undo','hash')`, user, user.String()+"@test.invalid")
		user := user
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	}
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Undo',$2,$3)`, f.ws, f.ws.String(), f.owner)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, f.ws) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, f.ws, f.owner, f.outsider)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Undo','UND')`, f.team, f.ws)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Private undo','PUN',TRUE)`, f.privateTeam, f.ws)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2),($3,$2)`, f.team, f.owner, f.privateTeam)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,is_default) VALUES($1,$2,'Todo','todo','unstarted',TRUE)`, f.todo, f.team)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Done','done','completed')`, f.done, f.team)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,is_default) VALUES($1,$2,'Todo','todo','unstarted',TRUE)`, f.privateTodo, f.privateTeam)
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'Bug','#f00')`, f.label, f.ws)
	f.ctx = domain.WithAccess(context.Background(), domain.Access{UserID: f.owner, WorkspaceID: f.ws})
	f.outsiderCtx = domain.WithAccess(context.Background(), domain.Access{UserID: f.outsider, WorkspaceID: f.ws})
	return f
}

func (f *undoFixture) createIssue(t *testing.T, team, status uuid.UUID) *domain.Issue {
	t.Helper()
	tx, err := f.repo.BeginTx(f.ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	number, err := f.repo.NextNumber(f.ctx, tx, team)
	require.NoError(t, err)
	key, err := f.repo.TeamKey(f.ctx, tx, team)
	require.NoError(t, err)
	issue := &domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, TeamID: team, Number: number,
		Identifier: key + "-" + itoa(number), Title: "Undo me", Status: "todo", StatusID: &status, CreatorID: f.owner, Triaged: true}
	require.NoError(t, f.repo.Create(f.ctx, tx, issue))
	require.NoError(t, tx.Commit())
	created, err := f.repo.GetByID(f.ctx, issue.ID)
	require.NoError(t, err)
	return created
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestIssueVersionAdvancesOnEveryEdit(t *testing.T) {
	f := newUndoFixture(t)
	issue := f.createIssue(t, f.team, f.todo)
	require.EqualValues(t, 1, issue.Version)

	issue.Priority = domain.PriorityHigh
	require.NoError(t, f.repo.Update(f.ctx, issue))
	require.EqualValues(t, 2, issue.Version, "UpdateContent returns the new version")

	require.NoError(t, f.repo.SetLabels(f.ctx, issue.ID, []uuid.UUID{f.label}))
	require.NoError(t, f.repo.SetAssignees(f.ctx, issue.ID, []uuid.UUID{f.owner}))
	reloaded, err := f.repo.GetByID(f.ctx, issue.ID)
	require.NoError(t, err)
	require.EqualValues(t, 4, reloaded.Version, "label and assignee edits advance the version")
}

func TestApplyUndoPutsBackPreviousValuesWhenUnchanged(t *testing.T) {
	f := newUndoFixture(t)
	issue := f.createIssue(t, f.team, f.todo)
	require.NoError(t, f.repo.SetLabels(f.ctx, issue.ID, []uuid.UUID{f.label}))
	require.NoError(t, f.repo.SetAssignees(f.ctx, issue.ID, []uuid.UUID{f.owner, f.outsider}))
	before, err := f.repo.GetByID(f.ctx, issue.ID)
	require.NoError(t, err)

	// The edit being undone: status, priority, labels and assignees change.
	before.StatusID, before.Status, before.Priority = &f.done, "done", domain.PriorityUrgent
	require.NoError(t, f.repo.Update(f.ctx, before))
	require.NoError(t, f.repo.SetLabels(f.ctx, issue.ID, nil))
	require.NoError(t, f.repo.SetAssignees(f.ctx, issue.ID, nil))
	after, err := f.repo.GetByID(f.ctx, issue.ID)
	require.NoError(t, err)

	priority := domain.PriorityNone
	labels, assignees := []uuid.UUID{f.label}, []uuid.UUID{f.outsider, f.owner}
	undone, err := f.repo.ApplyUndo(f.ctx, f.ws, []domain.IssueUndo{{IssueID: issue.ID, ExpectedVersion: after.Version,
		Previous: domain.IssueUndoPrevious{StatusID: &f.todo, Priority: &priority, LabelIDs: &labels, AssigneeIDs: &assignees}}})
	require.NoError(t, err)
	require.Len(t, undone, 1)
	require.Equal(t, f.todo, *undone[0].StatusID)
	require.Equal(t, domain.IssueStatus("todo"), undone[0].Status, "the legacy slug follows the status")
	require.Equal(t, domain.PriorityNone, undone[0].Priority)
	require.Equal(t, f.outsider, *undone[0].AssigneeID, "the first previous assignee is the primary one")
	require.Greater(t, undone[0].Version, after.Version)
	gotLabels, err := f.repo.GetLabels(f.ctx, issue.ID)
	require.NoError(t, err)
	require.Len(t, gotLabels, 1)
	gotAssignees, err := f.repo.GetAssignees(f.ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, assignees, gotAssignees, "assignees come back in their earlier order")
}

func TestApplyUndoAfterConflictingEditChangesNothing(t *testing.T) {
	f := newUndoFixture(t)
	issue := f.createIssue(t, f.team, f.todo)
	issue.Priority = domain.PriorityHigh
	require.NoError(t, f.repo.Update(f.ctx, issue))
	editedVersion := issue.Version

	// Someone else edits the issue before the undo arrives.
	issue.Priority = domain.PriorityLow
	require.NoError(t, f.repo.Update(f.ctx, issue))

	priority := domain.PriorityNone
	_, err := f.repo.ApplyUndo(f.ctx, f.ws, []domain.IssueUndo{{IssueID: issue.ID, ExpectedVersion: editedVersion,
		Previous: domain.IssueUndoPrevious{Priority: &priority}}})
	require.ErrorIs(t, err, ErrEditedMeanwhile)
	current, err := f.repo.GetByID(f.ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PriorityLow, current.Priority, "the later edit is kept")
	require.Equal(t, issue.Version, current.Version, "a refused undo does not write")
}

func TestBulkApplyUndoIsAllOrNothing(t *testing.T) {
	f := newUndoFixture(t)
	first, second := f.createIssue(t, f.team, f.todo), f.createIssue(t, f.team, f.todo)
	_, err := f.repo.BulkUpdate(f.ctx, f.ws, []uuid.UUID{first.ID, second.ID}, nil, ptrInt(1), nil, nil, nil, false)
	require.NoError(t, err)
	edited, err := f.repo.ListByIDs(f.ctx, f.ws, []uuid.UUID{first.ID, second.ID})
	require.NoError(t, err)
	require.Len(t, edited, 2)
	versions := map[uuid.UUID]int64{edited[0].ID: edited[0].Version, edited[1].ID: edited[1].Version}

	// The second issue changes again, so the bulk undo must refuse both.
	second.Priority = domain.PriorityLow
	second.Version = versions[second.ID]
	require.NoError(t, f.repo.Update(f.ctx, second))

	none := domain.PriorityNone
	items := []domain.IssueUndo{
		{IssueID: first.ID, ExpectedVersion: versions[first.ID], Previous: domain.IssueUndoPrevious{Priority: &none}},
		{IssueID: second.ID, ExpectedVersion: versions[second.ID], Previous: domain.IssueUndoPrevious{Priority: &none}},
	}
	_, err = f.repo.ApplyUndo(f.ctx, f.ws, items)
	require.ErrorIs(t, err, ErrEditedMeanwhile)
	unchanged, err := f.repo.GetByID(f.ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PriorityUrgent, unchanged.Priority, "the item without a conflict is not undone alone")

	// Without the conflict the whole bulk edit is undone in one step.
	items[1].ExpectedVersion = second.Version
	undone, err := f.repo.ApplyUndo(f.ctx, f.ws, items)
	require.NoError(t, err)
	require.Len(t, undone, 2)
	for _, issue := range undone {
		require.Equal(t, domain.PriorityNone, issue.Priority)
	}
}

func TestApplyUndoRefusesUnavailablePreviousValues(t *testing.T) {
	f := newUndoFixture(t)
	issue := f.createIssue(t, f.team, f.todo)
	// A status of another team cannot be put back on this issue.
	_, err := f.repo.ApplyUndo(f.ctx, f.ws, []domain.IssueUndo{{IssueID: issue.ID, ExpectedVersion: issue.Version,
		Previous: domain.IssueUndoPrevious{StatusID: &f.privateTodo}}})
	require.ErrorIs(t, err, ErrUndoUnavailable)
	current, err := f.repo.GetByID(f.ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, issue.Version, current.Version)
}

func TestUndoAndRestoreRespectPrivateTeams(t *testing.T) {
	f := newUndoFixture(t)
	private := f.createIssue(t, f.privateTeam, f.privateTodo)
	none := domain.PriorityNone
	_, err := f.repo.ApplyUndo(f.outsiderCtx, f.ws, []domain.IssueUndo{{IssueID: private.ID, ExpectedVersion: private.Version,
		Previous: domain.IssueUndoPrevious{Priority: &none}}})
	require.ErrorIs(t, err, ErrBulkIssueNotFound, "an outsider cannot undo on a private team")

	require.NoError(t, f.repo.Delete(f.ctx, private.ID))
	_, err = f.repo.Restore(f.outsiderCtx, f.ws, []uuid.UUID{private.ID}, time.Now().Add(-time.Hour))
	require.ErrorIs(t, err, ErrBulkIssueNotFound, "an outsider cannot restore on a private team")
	deleted, err := f.repo.GetDeletedByIdentifier(f.outsiderCtx, f.ws, private.Identifier)
	require.NoError(t, err)
	require.Nil(t, deleted)
}

func TestSoftDeletedIssueIsHiddenAndRestorableWithinGracePeriod(t *testing.T) {
	f := newUndoFixture(t)
	parent := f.createIssue(t, f.team, f.todo)
	issue := f.createIssue(t, f.team, f.todo)
	issue.ParentID = &parent.ID
	require.NoError(t, f.repo.Update(f.ctx, issue))

	require.NoError(t, f.repo.Delete(f.ctx, issue.ID))

	got, err := f.repo.GetByIdentifier(f.ctx, f.ws, issue.Identifier)
	require.NoError(t, err)
	require.Nil(t, got, "a deleted issue is not found by identifier")
	listed, total, err := f.repo.List(f.ctx, f.ws, dto.IssueFilterParams{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, parent.ID, listed[0].ID)
	searched, _, err := f.repo.List(f.ctx, f.ws, dto.IssueFilterParams{Search: "Undo"})
	require.NoError(t, err)
	require.Len(t, searched, 1)
	subs, err := f.repo.ListSubIssues(f.ctx, parent.ID)
	require.NoError(t, err)
	require.Empty(t, subs)
	total, _, err = f.repo.CountSubIssues(f.ctx, parent.ID)
	require.NoError(t, err)
	require.Zero(t, total)

	deleted, err := f.repo.GetDeletedByIdentifier(f.ctx, f.ws, issue.Identifier)
	require.NoError(t, err)
	require.NotNil(t, deleted)
	require.Equal(t, f.owner, *deleted.DeletedBy)

	// Numbers of deleted issues are never handed out again.
	next := f.createIssue(t, f.team, f.todo)
	require.Equal(t, issue.Number+1, next.Number)

	restored, err := f.repo.Restore(f.ctx, f.ws, []uuid.UUID{issue.ID}, time.Now().Add(-30*24*time.Hour))
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Equal(t, parent.ID, *restored[0].ParentID, "the restored issue keeps its parent")
	got, err = f.repo.GetByIdentifier(f.ctx, f.ws, issue.Identifier)
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestRestoreAfterGracePeriodOrPurgeIsRefused(t *testing.T) {
	f := newUndoFixture(t)
	expired, recent := f.createIssue(t, f.team, f.todo), f.createIssue(t, f.team, f.todo)
	_, err := f.repo.BulkDelete(f.ctx, f.ws, []uuid.UUID{expired.ID, recent.ID})
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE issue_records SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, expired.ID)
	require.NoError(t, err)
	cutoff := time.Now().Add(-30 * 24 * time.Hour)

	_, err = f.repo.Restore(f.ctx, f.ws, []uuid.UUID{expired.ID, recent.ID}, cutoff)
	require.ErrorIs(t, err, ErrRestoreExpired)
	deleted, err := f.repo.ListDeletedByIDs(f.ctx, f.ws, []uuid.UUID{expired.ID, recent.ID})
	require.NoError(t, err)
	require.Len(t, deleted, 2, "a refused bulk restore restores nothing")

	purged, err := f.repo.PurgeDeletedIssues(context.Background(), cutoff, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, purged, 1)
	deleted, err = f.repo.ListDeletedByIDs(f.ctx, f.ws, []uuid.UUID{expired.ID, recent.ID})
	require.NoError(t, err)
	require.Len(t, deleted, 1, "only the expired issue is purged")
	require.Equal(t, recent.ID, deleted[0].ID)

	_, err = f.repo.Restore(f.ctx, f.ws, []uuid.UUID{expired.ID}, cutoff)
	require.ErrorIs(t, err, ErrBulkIssueNotFound, "a purged issue is gone")
	restored, err := f.repo.Restore(f.ctx, f.ws, []uuid.UUID{recent.ID}, cutoff)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestTeamKeyRenameRewritesDeletedIssues(t *testing.T) {
	f := newUndoFixture(t)
	issue := f.createIssue(t, f.team, f.todo)
	require.NoError(t, f.repo.Delete(f.ctx, issue.ID))
	teams := NewTeamRepository(f.db)
	team, err := teams.GetByID(f.ctx, f.team)
	require.NoError(t, err)
	require.NoError(t, teams.UpdateWithKey(f.ctx, team, "UNX"))
	deleted, err := f.repo.GetDeletedByIdentifier(f.ctx, f.ws, "UNX-"+itoa(issue.Number))
	require.NoError(t, err)
	require.NotNil(t, deleted, "a restored issue carries the team's current key")
}

func TestDeletingAStatusMovesDeletedIssuesToTheDefault(t *testing.T) {
	f := newUndoFixture(t)
	issue := f.createIssue(t, f.team, f.done)
	require.NoError(t, f.repo.Delete(f.ctx, issue.ID))
	require.NoError(t, NewTeamStatusRepository(f.db).Delete(f.ctx, f.done))
	deleted, err := f.repo.GetDeletedByIdentifier(f.ctx, f.ws, issue.Identifier)
	require.NoError(t, err)
	require.Equal(t, f.todo, *deleted.StatusID)
}

func ptrInt(v int) *int { return &v }

// A child keeps its parent_id while the parent is in the trash, so the parent
// can come back with its sub-issues; the child must stay editable meanwhile.
func TestChildOfDeletedParentStaysEditable(t *testing.T) {
	f := newUndoFixture(t)
	parent := f.createIssue(t, f.team, f.todo)
	child := f.createIssue(t, f.team, f.todo)
	child.ParentID = &parent.ID
	require.NoError(t, f.repo.Update(f.ctx, child))
	require.NoError(t, f.repo.Delete(f.ctx, parent.ID))

	child.Priority = domain.PriorityHigh
	require.NoError(t, f.repo.Update(f.ctx, child), "editing a child of a deleted parent")
	none := domain.PriorityNone
	_, err := f.repo.ApplyUndo(f.ctx, f.ws, []domain.IssueUndo{{IssueID: child.ID, ExpectedVersion: child.Version,
		Previous: domain.IssueUndoPrevious{Priority: &none}}})
	require.NoError(t, err, "undoing an edit of a child of a deleted parent")
}
