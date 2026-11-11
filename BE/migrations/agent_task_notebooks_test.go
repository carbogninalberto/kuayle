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

// Migration 150 adds the task notebook, its history and the per-attempt
// notebook and read-only worktree columns, and the table of recorded prompts. Existing attempts
// need no notebook update and have no recorded prompt. Rolling back drops
// the notebooks.
func TestAgentTaskNotebooksMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_task_notebooks_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(149))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	columns := func() int {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='dev_machine_agent_runs'
 AND column_name IN ('notebook_required','notebook_updates','notebook_problem','notebook_asked_at','notebook_continuation','worktree_read_only')`).Scan(&count))
		return count
	}
	tables := func() int {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('agent_task_notebooks','agent_task_notebook_changes','dev_machine_agent_run_prompts')`).Scan(&count))
		return count
	}
	require.Zero(t, columns())
	require.Zero(t, tables())

	require.NoError(t, m.Migrate(150))
	require.Equal(t, 6, columns())
	require.Equal(t, 3, tables())
	var required, readOnly, nullable string
	require.NoError(t, db.QueryRow(`SELECT column_default FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name='notebook_required'`).Scan(&required))
	require.NoError(t, db.QueryRow(`SELECT column_default FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name='worktree_read_only'`).Scan(&readOnly))
	require.NoError(t, db.QueryRow(`SELECT is_nullable FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name='notebook_continuation'`).Scan(&nullable))
	require.Equal(t, "false", required, "attempts from before the notebook need no update")
	require.Equal(t, "false", readOnly)
	require.Equal(t, "YES", nullable)

	require.NoError(t, m.Migrate(149))
	require.Zero(t, columns())
	require.Zero(t, tables())
	require.NoError(t, m.Migrate(150), "the migration applies again after a rollback")
}
