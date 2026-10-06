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

// Run against the same migrated PostgreSQL used by the CI database job.
// Every wait is bounded and transactions roll back before fixture cleanup.
func TestPrivatePrivacyGuardConcurrency(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, ws, team, status := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Concurrency','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM users WHERE id=$1`, owner); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Concurrency',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Concurrency','CON')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range ids {
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,$5,$6,'Concurrency',$7)`, id, ws, team, status, i+1, "CON-"+id.String()[:8], owner)
		require.NoError(t, err)
	}
	actor := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	t.Run("independent writes do not wait behind a transaction touching another row", func(t *testing.T) {
		first, e := db.BeginTxx(actor, nil)
		require.NoError(t, e)
		defer func() { _ = first.Rollback() }()
		_, e = first.Exec(`UPDATE issues SET title='first transaction' WHERE id=$1`, ids[0])
		require.NoError(t, e)
		deadline, cancel := context.WithTimeout(actor, 750*time.Millisecond)
		defer cancel()
		_, e = db.ExecContext(deadline, `UPDATE issues SET title='independent edit' WHERE id=$1`, ids[1])
		require.NoError(t, e, "a different row must not wait on the first transaction's privacy guard")
		_, e = first.Exec(`UPDATE issues SET title='bulk continuation' WHERE id=$1`, ids[1])
		require.NoError(t, e)
	})
	t.Run("publication excludes privacy transitions but permits content writes", func(t *testing.T) {
		guard, e := beginPublicWorkspaceOperation(actor, db, ws)
		require.NoError(t, e)
		defer func() { _ = guard.Rollback() }()
		deadline, cancel := context.WithTimeout(actor, 750*time.Millisecond)
		defer cancel()
		_, e = db.ExecContext(deadline, `UPDATE issues SET title='during publication' WHERE id=$1`, ids[0])
		require.NoError(t, e, "outbound network latency must not block unrelated content edits")
		var guardPID int
		require.NoError(t, guard.Get(&guardPID, `SELECT pg_backend_pid()`))
		transitionCtx, stop := context.WithTimeout(actor, 5*time.Second)
		defer stop()
		result := make(chan error, 1)
		go func() {
			_, err := NewTeamRepository(db).SetVisibility(transitionCtx, team, true, false, true)
			result <- err
		}()
		require.Eventually(t, func() bool {
			var waiting bool
			err := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, guardPID)
			return err == nil && waiting
		}, 2*time.Second, 10*time.Millisecond, "visibility must wait for the active delivery")
		_, e = db.ExecContext(deadline, `UPDATE issues SET title='bulk while visibility is queued' WHERE workspace_id=$1`, ws)
		require.NoError(t, e, "queued visibility must not acquire the graph barrier before publication completes")
		require.NoError(t, guard.Rollback())
		require.NoError(t, <-result)
		_, e = beginPublicWorkspaceOperation(actor, db, ws)
		require.ErrorIs(t, e, ErrPrivateWorkspaceOperation)

	})
	t.Run("queued member edit cannot use the pre-transition visibility snapshot", func(t *testing.T) {
		require.Zero(t, queuedPrivatizationEdit(t, db, actor, ws, team, ids[0]), "authorization must observe the committed private classification")
	})

	t.Run("late private asset cannot race a public historical reference", func(t *testing.T) {
		public, publicStatus, publicIssue := uuid.New(), uuid.New(), uuid.New()
		_, e := db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Public','PUB')`, public, ws)
		require.NoError(t, e)
		_, e = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, publicStatus, public)
		require.NoError(t, e)
		_, e = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,1,'PUB-1','Public',$5)`, publicIssue, ws, public, publicStatus, owner)
		require.NoError(t, e)
		for _, source := range []string{"comments", "issue_history"} {
			t.Run(source, func(t *testing.T) {
				asset := uuid.New()
				writer, err := db.BeginTxx(actor, nil)
				require.NoError(t, err)
				defer func() { _ = writer.Rollback() }()
				if source == "comments" {
					_, err = writer.Exec(`INSERT INTO comments(issue_id,user_id,body) VALUES($1,$2,$3)`, publicIssue, owner, "/assets/"+asset.String())
				} else {
					_, err = writer.Exec(`INSERT INTO issue_history(issue_id,user_id,field,old_value,new_value) VALUES($1,$2,'description',$3,'new')`, publicIssue, owner, "/assets/"+asset.String())
				}
				require.NoError(t, err)
				var writerPID int
				require.NoError(t, writer.Get(&writerPID, `SELECT pg_backend_pid()`))
				deadline, cancel := context.WithTimeout(actor, 5*time.Second)
				defer cancel()
				result := make(chan error, 1)
				go func() {
					_, err := db.ExecContext(deadline, `INSERT INTO assets(id,workspace_id,team_id,storage_key,filename,content_type,size,uploaded_by) VALUES($1,$2,$3,$4,'late.txt','text/plain',1,$5)`, asset, ws, team, asset.String(), owner)
					result <- err
				}()
				require.Eventually(t, func() bool {
					var waiting bool
					err := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, writerPID)
					return err == nil && waiting
				}, 2*time.Second, 10*time.Millisecond)
				require.NoError(t, writer.Commit())
				require.ErrorContains(t, <-result, "crosses a private-team boundary")
				var count int
				require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM assets WHERE id=$1`, asset))
				require.Zero(t, count)
			})
		}
	})

}

// queuedPrivatizationEdit makes the team public, privatizes it in an open
// transaction, queues an ordinary member's guarded edit behind that transaction
// and returns the rows the member changed after the classification committed.
func queuedPrivatizationEdit(t *testing.T, db *sqlx.DB, actor context.Context, ws, team, issue uuid.UUID) int64 {
	t.Helper()
	member := uuid.New()
	_, e := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, member, member.String()+"@test.invalid")
	require.NoError(t, e)
	defer func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, member) }()
	_, e = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, member)
	require.NoError(t, e)
	_, e = db.Exec(`UPDATE teams SET is_private=FALSE WHERE id=$1`, team)
	require.NoError(t, e)
	transition, e := db.BeginTxx(actor, nil)
	require.NoError(t, e)
	defer func() { _ = transition.Rollback() }()
	_, e = transition.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
	require.NoError(t, e)
	var transitionPID int
	require.NoError(t, transition.Get(&transitionPID, `SELECT pg_backend_pid()`))
	memberCtx, cancel := context.WithTimeout(domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: ws}), 5*time.Second)
	defer cancel()
	result := make(chan int64, 1)
	errors := make(chan error, 1)
	go func() {
		r, err := db.ExecContext(memberCtx, `UPDATE issues SET title='must not persist' WHERE id=$1 AND `+teamVisible(memberCtx, "issues.team_id"), issue)
		if err != nil {
			errors <- err
			return
		}
		n, err := r.RowsAffected()
		if err != nil {
			errors <- err
			return
		}
		result <- n
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, transitionPID)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, transition.Commit())
	select {
	case n := <-result:
		return n
	case e := <-errors:
		require.NoError(t, e)
	case <-memberCtx.Done():
		t.Fatal("queued member edit did not finish")
	}
	return 0
}
