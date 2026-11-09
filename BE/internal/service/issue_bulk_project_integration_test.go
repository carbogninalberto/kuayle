package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Bulk "Set project" moves issues like the single-issue update: references are
// validated before any write, labels of the project an issue leaves are
// dropped, and history records both.
func TestIssueServiceBulkUpdateProjectPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.Get(&n, query, args...))
		return n
	}

	user, workspace, otherWorkspace := uuid.New(), uuid.New(), uuid.New()
	team, privateTeam, status := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Bulk project','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	for _, w := range []uuid.UUID{workspace, otherWorkspace} {
		exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Bulk project',$2,$3)`, w, w.String(), user)
		w := w
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, w) })
	}
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Open','BLK')`, team, workspace)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','BSC',true)`, privateTeam, workspace)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)

	projectA, projectB, foreign, secret := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'A')`, projectA, workspace)
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'B')`, projectB, workspace)
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Foreign')`, foreign, otherWorkspace)
	exec(`INSERT INTO projects(id,workspace_id,team_id,name) VALUES($1,$2,$3,'Secret')`, secret, workspace, privateTeam)

	issueRepo := repository.NewIssueRepository(db)
	historyRepo := repository.NewIssueHistoryRepository(db)
	svc := NewIssueService(issueRepo, repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db), historyRepo, realtime.NewHub(), nil)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})

	first, second := uuid.New(), uuid.New()
	exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id,project_id) VALUES($1,$2,$3,$4,1,'BLK-1','One',$5,$6)`, first, workspace, team, status, user, projectA)
	exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,2,'BLK-2','Two',$5)`, second, workspace, team, status, user)
	global, inA := uuid.New(), uuid.New()
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'global','#112233')`, global, workspace)
	exec(`INSERT INTO labels(id,workspace_id,name,color,project_id) VALUES($1,$2,'a','#112233',$3)`, inA, workspace, projectA)
	require.NoError(t, issueRepo.SetLabels(ctx, first, []uuid.UUID{global, inA}))

	projectOf := func(id uuid.UUID) *uuid.UUID {
		issue, err := issueRepo.GetByID(ctx, id)
		require.NoError(t, err)
		return issue.ProjectID
	}
	ids := []string{first.String(), second.String()}

	// A project outside the workspace, or in a private team, moves nothing.
	for _, bad := range []uuid.UUID{foreign, secret} {
		raw := bad.String()
		_, err = svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &raw})
		require.Error(t, err)
		require.Equal(t, &projectA, projectOf(first), "a refused move leaves every issue as it was")
		require.Nil(t, projectOf(second))
	}

	raw := projectB.String()
	updated, err := svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &raw})
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Equal(t, &projectB, projectOf(first))
	require.Equal(t, &projectB, projectOf(second))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1`, first), "A's label is dropped, the workspace label stays")
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1 AND label_id=$2`, first, global))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='project'`, first))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='labels'`, first))
	var history []struct {
		Field string    `db:"field"`
		Old   string    `db:"old_value"`
		New   string    `db:"new_value"`
		User  uuid.UUID `db:"user_id"`
	}
	require.NoError(t, db.Select(&history, `SELECT field, old_value, new_value, user_id FROM issue_history WHERE issue_id=$1 ORDER BY field`, first))
	require.Len(t, history, 2)
	require.Equal(t, "labels", history[0].Field)
	require.Equal(t, "a, global", history[0].Old)
	require.Equal(t, "global", history[0].New)
	require.Equal(t, "project", history[1].Field)
	require.Equal(t, projectA.String(), history[1].Old)
	require.Equal(t, projectB.String(), history[1].New)
	require.Equal(t, user, history[1].User)
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='project' AND old_value='' AND new_value=$2`, second, projectB.String()))

	updated, err = svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &raw})
	require.NoError(t, err)
	require.Zero(t, updated, "moving to the project they are already in changes nothing")

	none := ""
	updated, err = svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &none})
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Nil(t, projectOf(first))
	require.Nil(t, projectOf(second))
}

// bulkProjectFixture is a workspace with one public team, one private team and
// a few projects, for the bulk "Set project" tests below.
type bulkProjectFixture struct {
	db                             *sqlx.DB
	svc                            *IssueService
	ctx                            context.Context
	user, workspace, team, private uuid.UUID
	status, privateStatus          uuid.UUID
	projectA, projectB, secret     uuid.UUID
	issueNumber                    int
}

func newBulkProjectFixture(t *testing.T) *bulkProjectFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	f := &bulkProjectFixture{db: db, user: uuid.New(), workspace: uuid.New(), team: uuid.New(), private: uuid.New(),
		status: uuid.New(), privateStatus: uuid.New(), projectA: uuid.New(), projectB: uuid.New(), secret: uuid.New()}
	key := strings.ToUpper(f.workspace.String()[:4])
	f.exec(t, `INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Bulk project','hash')`, f.user, f.user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, f.user) })
	f.exec(t, `INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Bulk project',$2,$3)`, f.workspace, f.workspace.String(), f.user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, f.workspace) })
	f.exec(t, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, f.workspace, f.user)
	f.exec(t, `INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Open',$3)`, f.team, f.workspace, "O"+key)
	f.exec(t, `INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret',$3,true)`, f.private, f.workspace, "S"+key)
	f.exec(t, `INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, f.status, f.team)
	f.exec(t, `INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, f.privateStatus, f.private)
	f.exec(t, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'A')`, f.projectA, f.workspace)
	f.exec(t, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'B')`, f.projectB, f.workspace)
	f.exec(t, `INSERT INTO projects(id,workspace_id,team_id,name) VALUES($1,$2,$3,'Secret')`, f.secret, f.workspace, f.private)
	f.svc = NewIssueService(repository.NewIssueRepository(db), repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db),
		repository.NewIssueHistoryRepository(db), realtime.NewHub(), nil)
	f.ctx = domain.WithAccess(context.Background(), domain.Access{UserID: f.user, WorkspaceID: f.workspace})
	return f
}

func (f *bulkProjectFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := f.db.Exec(query, args...)
	require.NoError(t, err)
}

func (f *bulkProjectFixture) issue(t *testing.T, team, status uuid.UUID, project *uuid.UUID) uuid.UUID {
	t.Helper()
	f.issueNumber++
	id := uuid.New()
	f.exec(t, `INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id,project_id,priority) VALUES($1,$2,$3,$4,$5,$6,'Issue',$7,$8,1)`,
		id, f.workspace, team, status, f.issueNumber, fmt.Sprintf("BLK-%d", f.issueNumber), f.user, project)
	return id
}

func (f *bulkProjectFixture) move(ids []uuid.UUID, project *uuid.UUID) (int, error) {
	raw := ""
	if project != nil {
		raw = project.String()
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	return f.svc.BulkUpdate(f.ctx, f.workspace, f.user, dto.BulkUpdateIssueRequest{IssueIDs: strs, ProjectID: &raw})
}

func (f *bulkProjectFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.Get(&n, query, args...))
	return n
}

// A bulk move only writes the project: an edit to another field that commits
// while the move waits for the row is kept, not reverted to a stale snapshot.
func TestIssueServiceBulkUpdateProjectKeepsConcurrentEditsPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	first, second := f.issue(t, f.team, f.status, &f.projectA), f.issue(t, f.team, f.status, nil)

	// Another editor holds the second issue's row with an uncommitted priority change.
	editor, err := f.db.Beginx()
	require.NoError(t, err)
	defer editor.Rollback()
	var editorPID int
	require.NoError(t, editor.Get(&editorPID, `SELECT pg_backend_pid()`))
	_, err = editor.Exec(`UPDATE issues SET priority=4, title='Edited' WHERE id=$1`, second)
	require.NoError(t, err)

	type result struct {
		updated int
		err     error
	}
	done := make(chan result, 1)
	go func() {
		updated, err := f.move([]uuid.UUID{first, second}, &f.projectB)
		done <- result{updated, err}
	}()
	// Wait until the move is blocked on the editor's row lock, then commit the edit.
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, editorPID) > 0
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, editor.Commit())

	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("bulk move did not finish")
	}
	require.NoError(t, res.err)
	require.Equal(t, 2, res.updated)
	var row struct {
		Priority  int       `db:"priority"`
		Title     string    `db:"title"`
		ProjectID uuid.UUID `db:"project_id"`
	}
	require.NoError(t, f.db.Get(&row, `SELECT priority, title, project_id FROM issues WHERE id=$1`, second))
	require.Equal(t, 4, row.Priority, "the concurrent priority change survives the move")
	require.Equal(t, "Edited", row.Title)
	require.Equal(t, f.projectB, row.ProjectID)
}

// Repeated IDs move and record each issue once.
func TestIssueServiceBulkUpdateProjectCountsDuplicatesOncePostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	first, second := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)

	updated, err := f.move([]uuid.UUID{first, first, second, first}, &f.projectA)
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='project'`, first))
	require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='project'`, second))
}

// The move is all-or-nothing: one issue the project cannot take (a public-team
// issue and a private-team project) refuses the whole batch.
func TestIssueServiceBulkUpdateProjectIsAtomicPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	inPrivate := f.issue(t, f.private, f.privateStatus, nil)
	inPublic := f.issue(t, f.team, f.status, &f.projectA)

	_, err := f.move([]uuid.UUID{inPrivate, inPublic}, &f.secret)
	require.Error(t, err)
	require.Zero(t, f.count(t, `SELECT COUNT(*) FROM issues WHERE id=$1 AND project_id IS NOT NULL`, inPrivate), "nothing moved")
	require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM issues WHERE id=$1 AND project_id=$2`, inPublic, f.projectA))
	require.Zero(t, f.count(t, `SELECT COUNT(*) FROM issue_history WHERE issue_id IN ($1,$2)`, inPrivate, inPublic))

	// The private-team issue alone can join its own team's project.
	updated, err := f.move([]uuid.UUID{inPrivate}, &f.secret)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
}
