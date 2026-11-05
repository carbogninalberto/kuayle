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

// Existing servers keep their effort, the new levels become valid, and rolling
// back maps them to the nearest level the previous release accepts.
func TestAgentModelDefaultsMigrationAddsEffortLevelsAndRollsBack(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_agent_model_defaults_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(108))
	_, err = db.Exec(`INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,encrypted_api_key,default_model,thinking,is_default) VALUES($1,'Local','https://models.example.test/v1','k','m','low',true)`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_model_servers SET thinking='medium'`)
	require.Error(t, err, "the previous release has no medium effort")

	require.NoError(t, m.Migrate(109))
	var thinking string
	require.NoError(t, db.QueryRow(`SELECT thinking FROM agent_model_servers`).Scan(&thinking))
	require.Equal(t, "low", thinking)
	_, err = db.Exec(`UPDATE agent_model_servers SET thinking='xhigh'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_model_defaults(workspace_id,provider,model,thinking) VALUES($1,'claude-code','claude-opus-5-5','medium')`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_model_defaults(workspace_id,provider,model) VALUES($1,'claude-code','other')`, fixture.workspaceA)
	require.Error(t, err, "one default per agent and workspace")

	require.NoError(t, m.Steps(-1))
	require.NoError(t, db.QueryRow(`SELECT thinking FROM agent_model_servers`).Scan(&thinking))
	require.Equal(t, "max", thinking)
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('agent_model_defaults') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, m.Steps(1))
}
