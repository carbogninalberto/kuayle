package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A write that lost the race against a concurrent one reports it instead of
// repeating the event: a second delete and a triage whose issue was already
// triaged by someone else write nothing.
func TestIssueDeleteAndTriageReportTheEarlierWriterPostgres(t *testing.T) {
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

	member := uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Concurrent write','hash')`, member, member.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, member) })
	workspace := uuid.New()
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Concurrent write',$2,$3)`, workspace, workspace.String(), member)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, member)
	team := uuid.New()
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Conc','CON')`, team, workspace)
	todo, cancelled := uuid.New(), uuid.New()
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, todo, team)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Cancelled','cancelled','cancelled')`, cancelled, team)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: workspace})
	issues := NewIssueRepository(db)
	insertIssue := func(number int, identifier, title string, triaged bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id,triaged) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			id, workspace, team, todo, number, identifier, title, member, triaged)
		return id
	}

	// Delete reports what it deleted: the second writer finds nothing left.
	deleted := insertIssue(1, "CON-1", "Delete me", true)
	require.NoError(t, issues.Delete(ctx, deleted))
	require.ErrorIs(t, issues.Delete(ctx, deleted), sql.ErrNoRows)
	var remaining int
	require.NoError(t, db.Get(&remaining, `SELECT COUNT(*) FROM issues WHERE id=$1`, deleted))
	require.Zero(t, remaining)

	// A triage whose issue was triaged behind its back writes nothing.
	raced := insertIssue(2, "CON-2", "Raced triage", false)
	stale, err := issues.GetByID(ctx, raced)
	require.NoError(t, err)
	require.False(t, stale.Triaged)
	exec(`UPDATE issues SET triaged=TRUE WHERE id=$1`, raced)
	stale.Triaged = true
	stale.Status = domain.IssueStatusCancelled
	stale.StatusID = &cancelled
	require.ErrorIs(t, issues.UpdateUntriaged(ctx, stale), ErrIssueAlreadyTriaged)
	var stored struct {
		Status  string `db:"status"`
		Triaged bool   `db:"triaged"`
	}
	require.NoError(t, db.Get(&stored, `SELECT status, triaged FROM issues WHERE id=$1`, raced))
	require.Equal(t, "backlog", stored.Status, "the winner's status is kept")
	require.True(t, stored.Triaged)

	// A triage that wins writes the decision and its lifecycle timestamp.
	fresh := insertIssue(3, "CON-3", "Fresh triage", false)
	loaded, err := issues.GetByID(ctx, fresh)
	require.NoError(t, err)
	require.Nil(t, loaded.TriagedAt)
	loaded.Triaged = true
	loaded.Status = domain.IssueStatusCancelled
	loaded.StatusID = &cancelled
	require.NoError(t, issues.UpdateUntriaged(ctx, loaded))
	require.NotNil(t, loaded.TriagedAt)
	require.Equal(t, domain.IssueStatusCancelled, loaded.Status)
	var storedFresh struct {
		Status    string     `db:"status"`
		TriagedAt *time.Time `db:"triaged_at"`
	}
	require.NoError(t, db.Get(&storedFresh, `SELECT status, triaged_at FROM issues WHERE id=$1`, fresh))
	require.Equal(t, "cancelled", storedFresh.Status)
	require.NotNil(t, storedFresh.TriagedAt)
	require.Equal(t, loaded.TriagedAt.UTC().Truncate(time.Microsecond), storedFresh.TriagedAt.UTC().Truncate(time.Microsecond))

	// A bulk delete returns the rows as they were when deleted.
	first := insertIssue(4, "CON-4", "Bulk one", true)
	second := insertIssue(5, "CON-5", "Bulk two", true)
	deletedRows, err := issues.BulkDeleteReturning(ctx, workspace, []uuid.UUID{first, second})
	require.NoError(t, err)
	require.Len(t, deletedRows, 2)
	require.ElementsMatch(t, []string{"Bulk one", "Bulk two"}, []string{deletedRows[0].Title, deletedRows[1].Title})
	require.ElementsMatch(t, []uuid.UUID{first, second}, []uuid.UUID{deletedRows[0].ID, deletedRows[1].ID})
	var afterBulk int
	require.NoError(t, db.Get(&afterBulk, `SELECT COUNT(*) FROM issues WHERE id=ANY(ARRAY[$1,$2]::uuid[])`, first, second))
	require.Zero(t, afterBulk)
}
