package migrations_test

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Existing workspaces keep the default bot (no row), stored values are
// bounded, a linked and an uploaded picture are exclusive, settings go with
// their workspace, and rolling back removes only the settings.
func TestGitHubBotSettingsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_github_bot_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		admin.Close()
	})
	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(116))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(117))

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM workspace_github_bot_settings`).Scan(&count))
	require.Zero(t, count, "existing workspaces keep the default bot")
	for _, invalid := range []string{
		`INSERT INTO workspace_github_bot_settings(workspace_id,display_name) VALUES($1,'')`,
		`INSERT INTO workspace_github_bot_settings(workspace_id,avatar_url) VALUES($1,'http://example.com/a.png')`,
		`INSERT INTO workspace_github_bot_settings(workspace_id,avatar_file) VALUES($1,'../etc/passwd')`,
		`INSERT INTO workspace_github_bot_settings(workspace_id,avatar_url,avatar_file) VALUES($1,'https://example.com/a.png','` + uuid.NewString() + `.png')`,
		`INSERT INTO workspace_github_bot_settings(workspace_id,abilities) VALUES($1,'[]')`,
		`INSERT INTO workspace_github_bot_settings(workspace_id,mention_harness) VALUES($1,'bash')`,
	} {
		_, err = db.Exec(invalid, fixture.workspaceA)
		require.Error(t, err, invalid)
	}
	_, err = db.Exec(`INSERT INTO workspace_github_bot_settings(workspace_id,display_name,avatar_file,abilities,mention_harness,updated_by) VALUES($1,'Acme Reviewer',$2,'{"mention_reviews":true}','codex',$3)`, fixture.workspaceA, uuid.NewString()+".png", fixture.userA)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM workspaces WHERE id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM workspace_github_bot_settings`).Scan(&count))
	require.Zero(t, count, "settings are deleted with their workspace")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('public.workspace_github_bot_settings') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, m.Steps(1))
}
