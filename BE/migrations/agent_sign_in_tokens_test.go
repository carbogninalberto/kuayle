package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A captured sign-in survives the migration as kind credentials; a Claude Code
// token can sit next to it, Codex cannot have one, and rolling back removes
// only the tokens.
func TestAgentSignInTokenMigrationKeepsCapturedSignIns(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_sign_in_tokens_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(78))
	_, err = db.Exec(`INSERT INTO agent_sign_ins(workspace_id,user_id,provider,encrypted_credential) VALUES($1,$2,'claude-code','sealed-file')`, fixture.workspaceA, fixture.userA)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(81))

	var kind string
	var expires sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT kind,expires_at FROM agent_sign_ins WHERE workspace_id=$1 AND user_id=$2`, fixture.workspaceA, fixture.userA).Scan(&kind, &expires))
	require.Equal(t, "credentials", kind)
	require.False(t, expires.Valid)
	_, err = db.Exec(`INSERT INTO agent_sign_ins(workspace_id,user_id,provider,kind,encrypted_credential,expires_at) VALUES($1,$2,'claude-code','token','sealed-token',now()+interval '1 year')`, fixture.workspaceA, fixture.userA)
	require.NoError(t, err, "a token sits next to the captured file")
	_, err = db.Exec(`INSERT INTO agent_sign_ins(workspace_id,user_id,provider,kind,encrypted_credential) VALUES($1,$2,'codex','token','sealed')`, fixture.workspaceA, fixture.userA)
	require.Error(t, err, "only Claude Code has a token")
	_, err = db.Exec(`INSERT INTO agent_sign_ins(workspace_id,user_id,provider,kind,encrypted_credential) VALUES($1,$2,'claude-code','password','sealed')`, fixture.workspaceA, fixture.userA)
	require.Error(t, err)

	require.NoError(t, m.Steps(-1))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_sign_ins WHERE workspace_id=$1`, fixture.workspaceA).Scan(&count))
	require.Equal(t, 1, count, "rolling back keeps only the captured sign-in")
	require.NoError(t, m.Steps(1))
}
