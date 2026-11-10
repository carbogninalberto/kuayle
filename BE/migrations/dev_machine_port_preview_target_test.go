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

// Migration 136 lets an app port be served by a task's preview container.
// Existing ports keep their target, so a running instance routes as before;
// rolling back sends preview ports to the developer container again.
func TestDevMachinePortPreviewTargetKeepsExistingPortsAndRollsBack(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_preview_target_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(85))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	rows := insertFinalDevMachineFixture(t, db, fixture)
	require.NoError(t, m.Migrate(135))
	insert := `INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id,target) VALUES($1,$2,$3,$4,$5,$6)`
	_, err = db.Exec(insert, fixture.workspaceA, rows.machineA, 4100, "task", fixture.userA, "developer")
	require.NoError(t, err)
	_, err = db.Exec(insert, fixture.workspaceA, rows.machineA, 8025, "user", fixture.userA, "docker")
	require.NoError(t, err)
	_, err = db.Exec(insert, fixture.workspaceA, rows.machineA, 4101, "task", fixture.userA, "preview")
	require.Error(t, err, "before 136 a port is served by the developer or the docker service only")
	target := func(port int) string {
		t.Helper()
		var value string
		require.NoError(t, db.QueryRow(`SELECT target FROM dev_machine_ports WHERE machine_id=$1 AND port=$2`, rows.machineA, port).Scan(&value))
		return value
	}

	require.NoError(t, m.Migrate(136))
	require.Equal(t, "developer", target(4100), "a preview started before the upgrade keeps running where it is")
	require.Equal(t, "docker", target(8025))
	_, err = db.Exec(insert, fixture.workspaceA, rows.machineA, 4101, "task", fixture.userA, "preview")
	require.NoError(t, err)
	_, err = db.Exec(insert, fixture.workspaceA, rows.machineA, 4102, "task", fixture.userA, "host")
	require.Error(t, err, "other targets stay refused")

	require.NoError(t, m.Steps(-1))
	require.Equal(t, "developer", target(4101), "rollback routes preview ports to the developer container")
	require.Equal(t, "developer", target(4100))
	require.Equal(t, "docker", target(8025))
	_, err = db.Exec(`UPDATE dev_machine_ports SET target='preview' WHERE machine_id=$1 AND port=4101`, rows.machineA)
	require.Error(t, err, "the earlier constraint is back")
	require.NoError(t, m.Migrate(136), "the migration can be applied again")
}
