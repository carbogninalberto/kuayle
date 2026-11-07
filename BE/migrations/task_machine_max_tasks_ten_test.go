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

// Migration 117 lets up to ten tasks share a machine and makes ten the
// default for new workspaces, keeping saved limits; rolling back caps limits
// at the former maximum of 4.
func TestTaskMachineMaxTasksTenMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_task_machine_ten_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(116))
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE) ON CONFLICT (workspace_id) DO NOTHING`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,task_machine_max_tasks) VALUES($1,TRUE,3)
		ON CONFLICT (workspace_id) DO UPDATE SET task_machine_max_tasks=3`, fixture.workspaceB)
	require.NoError(t, err)
	limit := func(workspace uuid.UUID) int {
		t.Helper()
		var value int
		require.NoError(t, db.QueryRow(`SELECT task_machine_max_tasks FROM dev_machine_workspace_policies WHERE workspace_id=$1`, workspace).Scan(&value))
		return value
	}
	set := func(value int) error {
		_, err := db.Exec(`UPDATE dev_machine_workspace_policies SET task_machine_max_tasks=$2 WHERE workspace_id=$1`, fixture.workspaceA, value)
		return err
	}
	columnDefault := func() string {
		t.Helper()
		var value string
		require.NoError(t, db.QueryRow(`SELECT column_default FROM information_schema.columns
			WHERE table_name='dev_machine_workspace_policies' AND column_name='task_machine_max_tasks'`).Scan(&value))
		return value
	}
	checks := func() int {
		t.Helper()
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM pg_constraint
			WHERE conrelid='dev_machine_workspace_policies'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%task_machine_max_tasks%'`).Scan(&count))
		return count
	}

	require.NoError(t, m.Migrate(117))
	require.Equal(t, 2, limit(fixture.workspaceA), "saved limits are kept")
	require.Equal(t, 3, limit(fixture.workspaceB))
	require.Equal(t, "10", columnDefault(), "new workspaces share a machine between up to ten tasks")
	require.Equal(t, 1, checks(), "the former check is replaced")
	require.NoError(t, set(10))
	for _, invalid := range []int{0, 11} {
		require.Error(t, set(invalid), "%d is refused", invalid)
	}

	require.NoError(t, m.Steps(-1))
	require.Equal(t, 4, limit(fixture.workspaceA), "a limit above the former maximum becomes 4")
	require.Equal(t, 3, limit(fixture.workspaceB))
	require.Equal(t, "2", columnDefault())
	require.Equal(t, 1, checks())
	require.Error(t, set(5), "the former check is restored")

	require.NoError(t, m.Migrate(117), "the migration can be applied again")
	require.Equal(t, "10", columnDefault())
	require.NoError(t, set(10))
}
