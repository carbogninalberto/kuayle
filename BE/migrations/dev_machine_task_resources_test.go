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

// Migration 90 gives workspace policies task machine resources (4 vCPU and
// 8 GiB by default, existing workspaces included), moves the disk limit to
// 2 GiB steps (an odd limit is rounded down) and rolls back to the former
// columns.
func TestDevMachineTaskResourcesMigration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_task_resources_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		admin.Close()
	})
	testURL := databaseURLWithName(t, databaseURL, name)
	m, err := migrate.New("file://.", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(89))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,max_disk_gb) VALUES($1,TRUE,40)
		ON CONFLICT (workspace_id) DO UPDATE SET max_disk_gb=40`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,max_disk_gb) VALUES($1,TRUE,75)
		ON CONFLICT (workspace_id) DO UPDATE SET max_disk_gb=75`, fixture.workspaceB)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(90))
	resources := func(workspace uuid.UUID) []int {
		t.Helper()
		var cpu, memory, disk, maxDisk int
		require.NoError(t, db.QueryRow(`SELECT task_machine_cpu_millis,task_machine_memory_mb,task_machine_disk_gb,max_disk_gb
			FROM dev_machine_workspace_policies WHERE workspace_id=$1`, workspace).Scan(&cpu, &memory, &disk, &maxDisk))
		return []int{cpu, memory, disk, maxDisk}
	}
	require.Equal(t, []int{4000, 8192, 20, 40}, resources(fixture.workspaceA), "existing workspaces get the 8 GiB task machine")
	require.Equal(t, []int{4000, 8192, 20, 74}, resources(fixture.workspaceB), "an odd disk limit is rounded down to a 2 GiB step")

	for _, invalid := range []string{
		`UPDATE dev_machine_workspace_policies SET max_disk_gb=41 WHERE workspace_id=$1`,
		`UPDATE dev_machine_workspace_policies SET task_machine_disk_gb=21 WHERE workspace_id=$1`,
		`UPDATE dev_machine_workspace_policies SET task_machine_disk_gb=42 WHERE workspace_id=$1`,
		`UPDATE dev_machine_workspace_policies SET task_machine_cpu_millis=8000,task_machine_memory_mb=4096 WHERE workspace_id=$1`,
		`UPDATE dev_machine_workspace_policies SET task_machine_memory_mb=0 WHERE workspace_id=$1`,
	} {
		_, err = db.Exec(invalid, fixture.workspaceA)
		require.Error(t, err, invalid)
	}
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET max_disk_gb=2048,task_machine_cpu_millis=8000,task_machine_memory_mb=16384,task_machine_disk_gb=500 WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE table_name='dev_machine_workspace_policies' AND column_name LIKE 'task_machine_%'`).Scan(&columns))
	require.Zero(t, columns)
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET max_disk_gb=41 WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err, "rollback restores any limit from 20 to 2048")
	require.NoError(t, m.Migrate(90), "the migration can be applied again")
}
