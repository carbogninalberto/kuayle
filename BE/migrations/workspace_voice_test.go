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

// Workspaces start with voice off, can store only known models, lose the
// choice with the workspace, and rolling back removes only the choice.
func TestWorkspaceVoiceMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_voice_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(82))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(83))

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM workspace_voice_settings`).Scan(&count))
	require.Zero(t, count, "existing workspaces keep voice off")
	_, err = db.Exec(`INSERT INTO workspace_voice_settings(workspace_id,model,updated_by) VALUES($1,'whisper-huge',$2)`, fixture.workspaceA, fixture.userA)
	require.Error(t, err, "only pinned models can be stored")
	_, err = db.Exec(`INSERT INTO workspace_voice_settings(workspace_id,updated_by) VALUES($1,$2)`, fixture.workspaceA, fixture.userA)
	require.NoError(t, err)
	var model string
	require.NoError(t, db.QueryRow(`SELECT model FROM workspace_voice_settings WHERE workspace_id=$1`, fixture.workspaceA).Scan(&model))
	require.Equal(t, "off", model)
	_, err = db.Exec(`UPDATE workspace_voice_settings SET model='vosk-small-it-0.22' WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM workspaces WHERE id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM workspace_voice_settings`).Scan(&count))
	require.Zero(t, count, "the choice is deleted with its workspace")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('public.workspace_voice_settings') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, m.Steps(1))
}
