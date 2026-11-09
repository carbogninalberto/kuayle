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

	// A rename and a visibility change on the same team must not deadlock:
	// both take the privacy barrier before the team row.
	for i := 0; i < 15; i++ {
		keys := []string{"OPA", "OPB"}
		done := make(chan error, 2)
		go func() { done <- teams.UpdateWithKey(admin, load(ops), keys[i%2]) }()
		go func() { _, err := teams.SetVisibility(admin, ops, i%2 == 0, true, true); done <- err }()
		require.NoError(t, <-done)
		require.NoError(t, <-done)
	}

	// A number too large for the column is simply not found.
	huge, err := issues.GetByIdentifier(admin, ws, "ENG-99999999999")
	require.NoError(t, err)
	require.Nil(t, huge)

	// A rename and a delete of the same team must not deadlock.
	for i := 0; i < 15; i++ {
		doomed, doomedStatus := uuid.New(), uuid.New()
		_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Doomed','DOOM')`, doomed, ws)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($2,$1,'Todo','todo','unstarted',0)`, doomed, doomedStatus)
		require.NoError(t, err)
		for n := 1; n <= 20; n++ {
			_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,sort_order,status_id) VALUES(gen_random_uuid(),$1,$2,$3,$4,'x',$5,0,$6)`, ws, doomed, n, "DOOM-"+strconv.Itoa(n), owner, doomedStatus)
			require.NoError(t, err)
		}
		done := make(chan error, 2)
		go func() { done <- teams.UpdateWithKey(admin, load(doomed), "DOMB") }()
		go func() { done <- teams.Delete(admin, doomed) }()
		for j := 0; j < 2; j++ {
			if err := <-done; err != nil {
				require.NotContains(t, err.Error(), "deadlock")
			}
		}
		_, err = db.Exec(`DELETE FROM teams WHERE id=$1`, doomed)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM team_key_aliases WHERE team_id=$1`, doomed)
		require.NoError(t, err)
	}

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

// GetByIdentifier finds the current identifier first and falls back to a
// retired key only on a miss; the fallback stays inside the workspace and
// behind team visibility.
func TestGetByIdentifierAliasFallbackPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	owner, outsider := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Alias test','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		id := id
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	// Two workspaces whose teams share the same keys.
	type fixture struct{ ws, team, priv, issue, privIssue uuid.UUID }
	make1 := func() fixture {
		f := fixture{ws: uuid.New(), team: uuid.New(), priv: uuid.New(), issue: uuid.New(), privIssue: uuid.New()}
		_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Alias test',$2,$3)`, f.ws, f.ws.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, f.ws) })
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, f.ws, owner, outsider)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Eng','ENG')`, f.team, f.ws)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Priv','PRV',TRUE)`, f.priv, f.ws)
		require.NoError(t, err)
		for team, issue := range map[uuid.UUID]uuid.UUID{f.team: f.issue, f.priv: f.privIssue} {
			status := uuid.New()
			_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Todo','todo','unstarted',0)`, status, team)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,sort_order,status_id) SELECT $1,$2,$3,7,key||'-7','Issue',$4,0,$5 FROM teams WHERE id=$3`, issue, f.ws, team, owner, status)
			require.NoError(t, err)
		}
		return f
	}
	a, b := make1(), make1()
	teams, issues := NewTeamRepository(db), NewIssueRepository(db)
	asOwner := func(ws uuid.UUID) context.Context {
		return domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: ws})
	}
	rename := func(ws, id uuid.UUID, key string) {
		team, err := teams.GetByID(asOwner(ws), id)
		require.NoError(t, err)
		require.NoError(t, teams.UpdateWithKey(asOwner(ws), team, key))
	}
	find := func(c context.Context, ws uuid.UUID, identifier string) *domain.Issue {
		got, err := issues.GetByIdentifier(c, ws, identifier)
		require.NoError(t, err)
		return got
	}

	rename(a.ws, a.team, "PLT")
	rename(a.ws, a.priv, "SEC")

	// Exact and alias matches both resolve to the issue under its new identifier.
	for _, identifier := range []string{"PLT-7", "ENG-7"} {
		got := find(asOwner(a.ws), a.ws, identifier)
		require.NotNil(t, got, identifier)
		require.Equal(t, a.issue, got.ID)
		require.Equal(t, "PLT-7", got.Identifier)
	}
	require.Nil(t, find(asOwner(a.ws), a.ws, "ENG-8"), "an alias with an unknown number")
	require.Nil(t, find(asOwner(a.ws), a.ws, "ENG"), "not an identifier")

	// Workspace B was not renamed: ENG-7 is its own issue, and PLT-7 is unknown.
	got := find(asOwner(b.ws), b.ws, "ENG-7")
	require.NotNil(t, got)
	require.Equal(t, b.issue, got.ID)
	require.Nil(t, find(asOwner(b.ws), b.ws, "PLT-7"), "another workspace's alias does not apply")

	// A private team's issue stays hidden from a non-member by either identifier.
	asOutsider := domain.WithAccess(ctx, domain.Access{UserID: outsider, WorkspaceID: a.ws})
	require.Nil(t, find(asOutsider, a.ws, "SEC-7"))
	require.Nil(t, find(asOutsider, a.ws, "PRV-7"))
	got = find(asOwner(a.ws), a.ws, "PRV-7")
	require.NotNil(t, got)
	require.Equal(t, a.privIssue, got.ID)

	// Searching the list by a retired identifier finds the renamed issue, in
	// this workspace only and never in a private team the caller cannot see.
	search := func(c context.Context, ws uuid.UUID, term string) []uuid.UUID {
		listed, _, err := issues.List(c, ws, dto.IssueFilterParams{Search: term})
		require.NoError(t, err)
		ids := []uuid.UUID{}
		for _, issue := range listed {
			ids = append(ids, issue.ID)
		}
		return ids
	}
	require.Equal(t, []uuid.UUID{a.issue}, search(asOwner(a.ws), a.ws, "eng-7"))
	require.Equal(t, []uuid.UUID{a.privIssue}, search(asOwner(a.ws), a.ws, "PRV-7"))
	require.Empty(t, search(asOutsider, a.ws, "PRV-7"))
	require.Empty(t, search(asOwner(b.ws), b.ws, "PLT-7"))
}
