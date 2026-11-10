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

// Existing sign-ins survive the migration; an OpenCode key can be saved as a
// token without an expiry, OpenCode has no captured credentials, Codex still
// has no token, and rolling back removes only the OpenCode keys.
func TestOpenCodeAPIKeyMigrationKeepsExistingSignIns(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_opencode_key_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(138))
	insert := func(provider, kind string) error {
		_, err := db.Exec(`INSERT INTO agent_sign_ins(workspace_id,user_id,provider,kind,encrypted_credential) VALUES($1,$2,$3,$4,'sealed')`,
			fixture.workspaceA, fixture.userA, provider, kind)
		return err
	}
	require.NoError(t, insert("claude-code", "credentials"))
	require.NoError(t, insert("claude-code", "token"))
	require.NoError(t, insert("codex", "credentials"))
	require.Error(t, insert("opencode", "token"), "before the migration only Claude Code has a token")
	require.NoError(t, m.Migrate(139))

	require.NoError(t, insert("opencode", "token"))
	var expires sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM agent_sign_ins WHERE workspace_id=$1 AND provider='opencode'`, fixture.workspaceA).Scan(&expires))
	require.False(t, expires.Valid, "an API key has no expiry")
	require.Error(t, insert("opencode", "credentials"), "OpenCode has no credentials file")
	require.Error(t, insert("codex", "token"), "Codex still has no token")
	require.Error(t, insert("goose", "token"))

	require.NoError(t, m.Steps(-1))
	var providers string
	require.NoError(t, db.QueryRow(`SELECT string_agg(provider || '/' || kind, ',' ORDER BY provider, kind) FROM agent_sign_ins WHERE workspace_id=$1`, fixture.workspaceA).Scan(&providers))
	require.Equal(t, "claude-code/credentials,claude-code/token,codex/credentials", providers, "rolling back removes only the OpenCode key")
	require.NoError(t, m.Steps(1))
}
