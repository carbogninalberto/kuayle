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

// Migration 156 gives each machine its own agent task limit between 1 and 10;
// NULL follows the workspace policy. Existing machines keep following it, and
// rollback drops every override.
func TestMachineTaskLimitMigrationIsOptionalAndRollsBack(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_machine_task_limit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		admin.Close()
	})
	target := databaseURLWithName(t, databaseURL, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(155))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	machine := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machines
		(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,
		 machine_size,cpu_millis,memory_mb,disk_gb)
		VALUES ($1,$2,$3,'machinelimit0001','Limit','running','running',1,'small',1000,2048,20)`,
		machine, fixture.workspaceA, fixture.userA)
	require.NoError(t, err)
	set := func(value any) error {
		_, err := db.Exec(`UPDATE dev_machines SET max_tasks=$2 WHERE id=$1`, machine, value)
		return err
	}
	maxTasks := func() sql.NullInt64 {
		t.Helper()
		var value sql.NullInt64
		require.NoError(t, db.QueryRow(`SELECT max_tasks FROM dev_machines WHERE id=$1`, machine).Scan(&value))
		return value
	}

	require.NoError(t, m.Migrate(156))
	require.False(t, maxTasks().Valid, "an existing machine follows the workspace policy")

	for _, valid := range []int{1, 4, 10} {
		require.NoError(t, set(valid), "%d is a supported limit", valid)
		require.Equal(t, int64(valid), maxTasks().Int64)
	}
	for _, invalid := range []int{0, 11, -1} {
		require.Error(t, set(invalid), "%d is refused", invalid)
	}
	require.NoError(t, set(nil), "an override can be cleared back to the policy")
	require.False(t, maxTasks().Valid)

	require.NoError(t, m.Steps(-1))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE table_name='dev_machines' AND column_name='max_tasks'`).Scan(&columns))
	require.Zero(t, columns, "rollback drops each machine's override")

	require.NoError(t, m.Migrate(156), "the migration applies again after a rollback")
	require.NoError(t, set(2))
	require.Equal(t, int64(2), maxTasks().Int64)
}
