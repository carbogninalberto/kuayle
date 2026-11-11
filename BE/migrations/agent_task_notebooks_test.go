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

// Migration 147 adds the task notebook, its history and the per-attempt
// notebook, read-only worktree and prompt-source columns. Existing attempts
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
	require.NoError(t, m.Migrate(144))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	columns := func() int {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='dev_machine_agent_runs'
 AND column_name IN ('notebook_required','notebook_updates','notebook_problem','worktree_read_only','prompt_sources')`).Scan(&count))
		return count
	}
	tables := func() int {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('agent_task_notebooks','agent_task_notebook_changes')`).Scan(&count))
		return count
	}
	require.Zero(t, columns())
	require.Zero(t, tables())

	require.NoError(t, m.Migrate(147))
	require.Equal(t, 5, columns())
	require.Equal(t, 2, tables())
	var required, readOnly, nullable string
	require.NoError(t, db.QueryRow(`SELECT column_default FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name='notebook_required'`).Scan(&required))
	require.NoError(t, db.QueryRow(`SELECT column_default FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name='worktree_read_only'`).Scan(&readOnly))
	require.NoError(t, db.QueryRow(`SELECT is_nullable FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name='prompt_sources'`).Scan(&nullable))
	require.Equal(t, "false", required, "attempts from before the notebook need no update")
	require.Equal(t, "false", readOnly)
	require.Equal(t, "YES", nullable)

	require.NoError(t, m.Migrate(144))
	require.Zero(t, columns())
	require.Zero(t, tables())
	require.NoError(t, m.Migrate(147), "the migration applies again after a rollback")
}
