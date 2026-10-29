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

// Migration 91 adds a pending machine size applied by the resize operation
// and the latest resource limit a machine container hit. Rollback keeps each
// machine's applied size and removes queued resizes.
func TestDevMachineResizeMigrationKeepsAppliedSizeAndRollsBack(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_resize_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	rows := insertFinalDevMachineFixture(t, db, fixture)
	var cpu, memory, disk int
	require.NoError(t, db.QueryRow(`SELECT cpu_millis, memory_mb, disk_gb FROM dev_machines WHERE id=$1`, rows.machineA).Scan(&cpu, &memory, &disk))

	require.NoError(t, m.Migrate(91))
	var pending, limit sql.NullString
	require.NoError(t, db.QueryRow(`SELECT pending_cpu_millis::text, resource_limit_kind FROM dev_machines WHERE id=$1`, rows.machineA).Scan(&pending, &limit))
	require.False(t, pending.Valid, "existing machines have no pending resize")
	require.False(t, limit.Valid, "existing machines have no recorded limit")
	_, err = db.Exec(`UPDATE dev_machines SET pending_cpu_millis=4000 WHERE id=$1`, rows.machineA)
	require.Error(t, err, "a pending size sets CPU, memory and disk together")
	_, err = db.Exec(`UPDATE dev_machines SET pending_cpu_millis=4000, pending_memory_mb=16384, pending_disk_gb=60 WHERE id=$1`, rows.machineA)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET resource_limit_kind='cpu', resource_limit_message='x', resource_limit_at=now() WHERE id=$1`, rows.machineA)
	require.Error(t, err, "only memory and disk limits are recorded")
	_, err = db.Exec(`UPDATE dev_machines SET resource_limit_kind='memory', resource_limit_at=now() WHERE id=$1`, rows.machineA)
	require.Error(t, err, "a recorded limit has its message")
	_, err = db.Exec(`UPDATE dev_machines SET resource_limit_kind='memory', resource_limit_message='Out of memory', resource_limit_at=now() WHERE id=$1`, rows.machineA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_operations(machine_id,workspace_id,action,status,generation,idempotency_key)
		VALUES($1,$2,'resize','pending',1,'resize:fixture')`, rows.machineA, fixture.workspaceA)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	version, _, err := m.Version()
	require.NoError(t, err)
	require.Less(t, version, uint(91))
	var afterCPU, afterMemory, afterDisk, resizes int
	require.NoError(t, db.QueryRow(`SELECT cpu_millis, memory_mb, disk_gb FROM dev_machines WHERE id=$1`, rows.machineA).Scan(&afterCPU, &afterMemory, &afterDisk))
	require.Equal(t, []int{cpu, memory, disk}, []int{afterCPU, afterMemory, afterDisk}, "rollback keeps the applied size")
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_operations WHERE machine_id=$1 AND action='resize'`, rows.machineA).Scan(&resizes))
	require.Zero(t, resizes, "rollback removes queued resizes")
	require.NoError(t, m.Migrate(91), "the migration can be applied again")
}
