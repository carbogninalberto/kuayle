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

// Migration 113 lets two tasks share a machine by default (existing
// workspaces included), keeps a user's own agent run alone on its machine and
// rolls back to one active run per machine.
func TestSharedTaskMachinesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_shared_task_machines_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(112))
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE) ON CONFLICT (workspace_id) DO NOTHING`, fixture.workspaceA)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(113))
	var limit int
	require.NoError(t, db.QueryRow(`SELECT task_machine_max_tasks FROM dev_machine_workspace_policies WHERE workspace_id=$1`, fixture.workspaceA).Scan(&limit))
	require.Equal(t, 2, limit, "existing workspaces share a machine between two tasks")
	for _, invalid := range []int{0, 5} {
		_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET task_machine_max_tasks=$2 WHERE workspace_id=$1`, fixture.workspaceA, invalid)
		require.Error(t, err)
	}
	var machine uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,'sharedtaskmigration','Shared','running','running','small',1000,2048,20) RETURNING id`, fixture.workspaceA).Scan(&machine))
	manualRun := func() error {
		_, err := db.Exec(`INSERT INTO dev_machine_agent_runs(workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds) VALUES($1,$2,'codex','interactive','Manual','["codex"]',60)`, fixture.workspaceA, machine)
		return err
	}
	require.NoError(t, manualRun())
	require.Error(t, manualRun(), "a user's own agent run keeps its machine to itself")
	index := func(name string) bool {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists))
		return exists
	}
	require.False(t, index("idx_dev_machine_agent_runs_one_active"))
	require.True(t, index("idx_dev_machine_agent_runs_machine_active"))

	require.NoError(t, m.Steps(-1))
	require.True(t, index("idx_dev_machine_agent_runs_one_active"))
	require.False(t, index("idx_dev_machine_agent_runs_one_active_manual"))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='dev_machine_workspace_policies' AND column_name='task_machine_max_tasks'`).Scan(&columns))
	require.Zero(t, columns)
	require.NoError(t, m.Migrate(113), "the migration can be applied again")
}
