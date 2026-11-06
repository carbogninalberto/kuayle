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

// Migration 114 adds the assistant's research settings beside existing
// workspaces; rolling back removes them with any stored search key.
func TestAssistantResearchSettingsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_research_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(113))
	require.NoError(t, m.Migrate(114))

	_, err = db.Exec(`INSERT INTO assistant_research_settings(workspace_id) VALUES($1)`, fixture.workspaceA)
	require.NoError(t, err, "an existing workspace gets the defaults")
	var provider, model string
	var parallel int
	var key sql.NullString
	require.NoError(t, db.QueryRow(`SELECT search_provider, search_api_key_encrypted, subagent_model, subagent_max_parallel FROM assistant_research_settings WHERE workspace_id=$1`, fixture.workspaceA).Scan(&provider, &key, &model, &parallel))
	require.Equal(t, "brave", provider)
	require.False(t, key.Valid)
	require.Empty(t, model)
	require.Equal(t, 3, parallel)

	for _, bad := range []string{
		`INSERT INTO assistant_research_settings(workspace_id, search_provider) VALUES($1, 'google')`,
		`INSERT INTO assistant_research_settings(workspace_id, subagent_max_parallel) VALUES($1, 9)`,
		`INSERT INTO assistant_research_settings(workspace_id, subagent_max_parallel) VALUES($1, 0)`,
		`INSERT INTO assistant_research_settings(workspace_id, search_api_key_encrypted) VALUES($1, '')`,
	} {
		_, err := db.Exec(bad, fixture.workspaceB)
		require.Error(t, err, bad)
	}
	_, err = db.Exec(`DELETE FROM workspaces WHERE id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM assistant_research_settings`).Scan(&count))
	require.Zero(t, count, "settings go with their workspace")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('assistant_research_settings') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
}

// Migration 116 lets a workspace choose SearXNG; rolling back moves those
// workspaces to Brave and keeps any stored key.
func TestAssistantResearchSearXNGMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_searxng_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(115))
	_, err = db.Exec(`INSERT INTO assistant_research_settings(workspace_id, search_provider) VALUES($1, 'searxng')`, fixture.workspaceA)
	require.Error(t, err, "before 116 SearXNG is not a provider")
	_, err = db.Exec(`INSERT INTO assistant_research_settings(workspace_id, search_provider, search_api_key_encrypted) VALUES($1, 'tavily', 'encrypted')`, fixture.workspaceA)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(116))
	_, err = db.Exec(`UPDATE assistant_research_settings SET search_provider='searxng' WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err, "an existing row can choose SearXNG")
	_, err = db.Exec(`INSERT INTO assistant_research_settings(workspace_id, search_provider) VALUES($1, 'google')`, fixture.workspaceB)
	require.Error(t, err, "other providers are still refused")

	require.NoError(t, m.Steps(-1))
	var provider string
	var key sql.NullString
	require.NoError(t, db.QueryRow(`SELECT search_provider, search_api_key_encrypted FROM assistant_research_settings WHERE workspace_id=$1`, fixture.workspaceA).Scan(&provider, &key))
	require.Equal(t, "brave", provider)
	require.Equal(t, "encrypted", key.String)
	_, err = db.Exec(`UPDATE assistant_research_settings SET search_provider='searxng' WHERE workspace_id=$1`, fixture.workspaceA)
	require.Error(t, err)
	require.NoError(t, m.Migrate(116), "the migration can be applied again")
}
