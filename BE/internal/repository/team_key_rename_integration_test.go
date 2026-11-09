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
	"github.com/stretchr/testify/require"
)

// Real SQL for renaming a team key: stored identifiers are rewritten, the old
// key stays resolvable, and keys cannot collide with teams or retired keys.
func TestTeamKeyRenamePostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Rename test','hash')`, owner, owner.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Rename test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)

	eng, ops, priv := uuid.New(), uuid.New(), uuid.New()
	for id, key := range map[uuid.UUID]string{eng: "ENG", ops: "OPS"} {
		_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,$3,$3)`, id, ws, key)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'PRV','PRV',TRUE)`, priv, ws)
	require.NoError(t, err)
	status := uuid.New()
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Todo','todo','unstarted',0)`, status, eng)
	require.NoError(t, err)
	var engIssue uuid.UUID
	for n := 1; n <= 2; n++ {
		engIssue = uuid.New()
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,sort_order,status_id) VALUES($1,$2,$3,$4,$5,'Issue',$6,0,$7)`, engIssue, ws, eng, n, "ENG-"+string(rune('0'+n)), owner, status)
		require.NoError(t, err)
	}
	privStatus, privIssue := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Todo','todo','unstarted',0)`, privStatus, priv)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,sort_order,status_id) VALUES($1,$2,$3,1,'PRV-1','Private',$4,0,$5)`, privIssue, ws, priv, owner, privStatus)
	require.NoError(t, err)

	teams, issues := NewTeamRepository(db), NewIssueRepository(db)
	admin := domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: ws})
	load := func(id uuid.UUID) *domain.Team {
		team, err := teams.GetByID(admin, id)
		require.NoError(t, err)
		require.NotNil(t, team)
		return team
	}

	// Rename ENG to PLT: identifiers are rewritten, the old key still resolves.
	team := load(eng)
	team.Name = "Platform"
	require.NoError(t, teams.UpdateWithKey(admin, team, "PLT"))
	require.Equal(t, "PLT", team.Key)
	require.Equal(t, "Platform", load(eng).Name)
	for _, identifier := range []string{"PLT-2", "ENG-2"} {
		got, err := issues.GetByIdentifier(admin, ws, identifier)
		require.NoError(t, err)
		require.NotNil(t, got, identifier)
		require.Equal(t, engIssue, got.ID)
		require.Equal(t, "PLT-2", got.Identifier)
	}
	missing, err := issues.GetByIdentifier(admin, ws, "ENG-99")
	require.NoError(t, err)
	require.Nil(t, missing)

	// Keys held by another team or retired by one cannot be taken.
	other := load(ops)
	require.ErrorIs(t, teams.UpdateWithKey(admin, other, "PLT"), ErrTeamKeyTaken)
	require.ErrorIs(t, teams.UpdateWithKey(admin, other, "ENG"), ErrTeamKeyTaken)
	require.ErrorIs(t, teams.Create(admin, &domain.Team{ID: uuid.New(), WorkspaceID: ws, Name: "Again", Key: "ENG"}), ErrTeamKeyTaken)
	require.Equal(t, "OPS", load(ops).Key)

	// Renaming back releases the team's own alias and keeps PLT resolvable.
	require.NoError(t, teams.UpdateWithKey(admin, load(eng), "ENG"))
	got, err := issues.GetByIdentifier(admin, ws, "PLT-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "ENG-1", got.Identifier)
	var aliases int
	require.NoError(t, db.Get(&aliases, `SELECT count(*) FROM team_key_aliases WHERE workspace_id=$1 AND old_key='ENG'`, ws))
	require.Zero(t, aliases)

	// An issue created while a rename is waiting keeps its number and ends up
	// under the new key: creation re-reads the key under the lock NextNumber holds.
	require.NoError(t, teams.UpdateWithKey(admin, load(eng), "ENG"))
	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	number, err := issues.NextNumber(ctx, tx, eng)
	require.NoError(t, err)
	renamed := make(chan error, 1)
	go func() { renamed <- teams.UpdateWithKey(admin, load(eng), "NEW") }()
	select {
	case err := <-renamed:
		t.Fatalf("rename did not wait for the issue being created: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	key, err := issues.TeamKey(ctx, tx, eng)
	require.NoError(t, err)
	require.Equal(t, "ENG", key)
	racing := uuid.New()
	_, err = tx.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,sort_order,status_id) VALUES($1,$2,$3,$4,$5,'Racing',$6,0,$7)`, racing, ws, eng, number, key+"-"+strconv.Itoa(number), owner, status)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-renamed)
	var identifier string
	require.NoError(t, db.Get(&identifier, `SELECT identifier_text FROM issues WHERE id=$1`, racing))
	require.Equal(t, "NEW-"+strconv.Itoa(number), identifier)
	require.NoError(t, teams.UpdateWithKey(admin, load(eng), "ENG"))

	// An alias never exposes a private team's issues to a non-member.
	require.NoError(t, teams.UpdateWithKey(admin, load(priv), "SEC"))
	outsider := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Outsider','hash')`, outsider, outsider.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, outsider) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, outsider)
	require.NoError(t, err)
	hidden, err := issues.GetByIdentifier(domain.WithAccess(ctx, domain.Access{UserID: outsider, WorkspaceID: ws}), ws, "PRV-1")
	require.NoError(t, err)
	require.Nil(t, hidden)
	seen, err := issues.GetByIdentifier(admin, ws, "PRV-1")
	require.NoError(t, err)
	require.NotNil(t, seen)
}
