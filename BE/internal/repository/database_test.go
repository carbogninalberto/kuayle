package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A database or role default of REPEATABLE READ reuses the statement snapshot
// taken before a classification lock wait. Open must restore READ COMMITTED.
func TestOpenPinsReadCommittedForQueuedAuthorization(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	// pgx sends unknown DSN keys as startup parameters, emulating
	// ALTER DATABASE/ROLE ... SET default_transaction_isolation.
	repeatable := url + separator + "default_transaction_isolation=repeatable%20read"

	control, err := sqlx.Connect("pgx", repeatable)
	require.NoError(t, err)
	t.Cleanup(func() { _ = control.Close() })
	var isolation string
	require.NoError(t, control.Get(&isolation, `SELECT current_setting('default_transaction_isolation')`))
	require.Equal(t, "repeatable read", isolation)

	db, err := Open(repeatable)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Get(&isolation, `SELECT current_setting('default_transaction_isolation')`))
	require.Equal(t, "read committed", isolation)

	owner, ws, team, status, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Isolation','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM users WHERE id=$1`, owner); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Isolation',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Isolation','ISO')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,1,'ISO-1','Isolation',$5)`, issue, ws, team, status, owner)
	require.NoError(t, err)
	actor := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})

	// The control proves this fixture reproduces the stale-snapshot write.
	require.EqualValues(t, 1, queuedPrivatizationEdit(t, control, actor, ws, team, issue), "control connection should reproduce the repeatable-read race")
	require.Zero(t, queuedPrivatizationEdit(t, db, actor, ws, team, issue), "Open must deny the queued edit after privatization")
}
