package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var previousFullAccessScopes = []string{
	"issues:read", "comments:read", "projects:read", "cycles:read",
	"labels:read", "teams:read", "members:read", "workspaces:read",
	"templates:read", "views:read", "wiki:read", "analytics:read",
	"notifications:read", "account:read", "assets:read", "agent_tasks:read",
	"workflows:read", "dev_machine:read", "issue:create", "issue:update",
	"issue:delete_own", "project:manage", "label:manage", "team:manage",
	"member:invite", "workspace:manage", "workspace:transfer", "wiki:write",
	"dev_machine:create", "dev_machine:manage", "dev_machine:admin",
}

var newTokenScopes = []string{
	"cycle:manage", "view:manage", "issue:delete", "account:write", "notifications:write",
}

// Migration 153 extends only active tokens that held the complete previous
// full-access set; a narrower or revoked token is untouched, and the down
// migration removes exactly the added scopes.
func TestPATFullAccessScopesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_pat_scopes_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		admin.Close()
	})
	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(152))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	userID := uuid.New()
	_, err = db.Exec(`INSERT INTO users (id,email,name,password_hash) VALUES ($1,$2,'pat migration','test')`, userID, userID.String()+"@example.test")
	require.NoError(t, err)
	insert := func(name string, scopes []string, revoked bool) uuid.UUID {
		id := uuid.New()
		var revokedAt sql.NullTime
		if revoked {
			revokedAt.Valid = true
			require.NoError(t, db.QueryRow(`SELECT NOW()`).Scan(&revokedAt.Time))
		}
		_, err := db.Exec(`INSERT INTO personal_access_tokens (id,user_id,name,token_hash,token_prefix,scopes,revoked_at)
			VALUES ($1,$2,$3,$4,'kuayle_pat_test',$5,$6)`, id, userID, name, uuid.NewString(), scopes, revokedAt)
		require.NoError(t, err)
		return id
	}
	full := insert("full", previousFullAccessScopes, false)
	narrow := insert("narrow", []string{"issues:read", "issue:create"}, false)
	revoked := insert("revoked", previousFullAccessScopes, true)

	require.NoError(t, m.Migrate(153))

	scopesOf := func(id uuid.UUID) []string {
		var scopes []string
		require.NoError(t, db.QueryRow(`SELECT scopes FROM personal_access_tokens WHERE id=$1`, id).Scan(pq.Array(&scopes)))
		return scopes
	}
	require.ElementsMatch(t, append(append([]string{}, previousFullAccessScopes...), newTokenScopes...), scopesOf(full))
	require.ElementsMatch(t, []string{"issues:read", "issue:create"}, scopesOf(narrow))
	require.ElementsMatch(t, previousFullAccessScopes, scopesOf(revoked),
		"the migration only extends active tokens")

	// Running the migration again is idempotent for the added scopes.
	require.NoError(t, m.Migrate(152))
	require.NoError(t, m.Migrate(153))
	require.ElementsMatch(t, append(append([]string{}, previousFullAccessScopes...), newTokenScopes...), scopesOf(full))

	require.NoError(t, m.Migrate(152))
	require.ElementsMatch(t, previousFullAccessScopes, scopesOf(full))
	require.ElementsMatch(t, []string{"issues:read", "issue:create"}, scopesOf(narrow))
}
