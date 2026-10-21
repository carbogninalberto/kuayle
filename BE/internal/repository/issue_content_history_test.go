package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestIssueContentHistoryPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Reuse one physical connection to prove the transaction-local actor is cleared.
	db.SetMaxOpenConns(1)
	owner, ws, team, status, id := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'History editor','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM users WHERE id=$1`, owner); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'History',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'History','HIS',true)`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,1,'HIS-1','Original',$5)`, id, ws, team, status, owner)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	repo, history := NewIssueRepository(db), NewIssueHistoryRepository(db)
	read := func() []domain.IssueHistory {
		entries, e := history.ListByIssue(ctx, id)
		require.NoError(t, e)
		return entries
	}
	require.Empty(t, read(), "existing/created issues must not gain invented history")
	issue, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	desc := "<p>Complete description</p>"
	issue.Title = "Revised"
	issue.Description = &desc
	require.NoError(t, repo.UpdateContent(ctx, issue, true, true))
	entries := read()
	require.Len(t, entries, 2)
	fields := map[string]domain.IssueHistory{}
	for _, entry := range entries {
		fields[entry.Field] = entry
		require.Equal(t, owner, entry.UserID)
		require.False(t, entry.CreatedAt.IsZero())
	}
	require.Equal(t, "Original", *fields["title"].OldValue)
	require.Equal(t, "Revised", *fields["title"].NewValue)
	require.Nil(t, fields["description"].OldValue)
	require.Equal(t, desc, *fields["description"].NewValue)
	require.NoError(t, repo.UpdateContent(ctx, issue, true, true))
	require.Len(t, read(), 2, "no-op saves must not create revisions")
	// An invalid actor fails the history insert and must roll back the issue too.
	issue.Title = "Must roll back"
	require.Error(t, repo.UpdateContent(WithIssueHistoryActor(ctx, uuid.New()), issue, true, false))
	persisted, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Revised", persisted.Title)
	require.Len(t, read(), 2)
	// Writes outside the HTTP service are captured too, without inventing an author.
	_, err = db.Exec(`UPDATE issues SET title='Integration edit' WHERE id=$1`, id)
	require.NoError(t, err)
	entries = read()
	require.Len(t, entries, 3)
	require.Equal(t, uuid.Nil, entries[0].UserID)
	require.Equal(t, "Revised", *entries[0].OldValue)
	// Unauthenticated and non-member reads cannot expose private content.
	for _, denied := range []context.Context{context.Background(), domain.WithAccess(context.Background(), domain.Access{UserID: uuid.New(), WorkspaceID: ws})} {
		hidden, e := history.ListByIssue(denied, id)
		require.NoError(t, e)
		require.Empty(t, hidden)
		require.Error(t, repo.Update(denied, persisted))
	}
	require.Len(t, read(), 3)
	// Two writers serialize on the issue; each entry uses the committed predecessor.
	db.SetMaxOpenConns(4)
	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE issues SET title='First concurrent edit' WHERE id=$1`, id)
	require.NoError(t, err)
	var pid int
	require.NoError(t, tx.Get(&pid, `SELECT pg_backend_pid()`))
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	writer, err := db.Connx(deadline)
	require.NoError(t, err)
	defer writer.Close()
	var writerPID int
	require.NoError(t, writer.QueryRowxContext(deadline, `SELECT pg_backend_pid()`).Scan(&writerPID))
	done := make(chan error, 1)
	go func() {
		_, e := writer.ExecContext(deadline, `UPDATE issues SET title='Second concurrent edit' WHERE id=$1`, id)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		e := db.Get(&blocked, `SELECT $1=ANY(pg_blocking_pids($2))`, pid, writerPID)
		return e == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
	entries = read()
	require.Len(t, entries, 5)
	require.Equal(t, "First concurrent edit", *entries[0].OldValue)
	require.Equal(t, "Second concurrent edit", *entries[0].NewValue)
	require.Equal(t, "Integration edit", *entries[1].OldValue)
	// Clearing preserves the prior content; repeated empty/null saves add nothing.
	_, err = db.Exec(`UPDATE issues SET description='' WHERE id=$1`, id)
	require.NoError(t, err)
	entries = read()
	require.Len(t, entries, 6)
	require.Equal(t, desc, *entries[0].OldValue)
	require.Equal(t, "", *entries[0].NewValue)
	_, err = db.Exec(`UPDATE issues SET description=NULL WHERE id=$1`, id)
	require.NoError(t, err)
	require.Len(t, read(), 6)
}
