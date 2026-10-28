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

// Migration 89 adds the opt-in Docker daemon: a workspace policy switch (off
// by default), the docker service type and app ports served by the daemon.
// Rollback removes daemon service rows and port targets, keeping the ports.
func TestDevMachineDockerMigrationIsOptInAndRollsBack(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_docker_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	// 85 is the last migration this one depends on; streams merged in
	// between (86-88) do not touch these tables.
	require.NoError(t, m.Migrate(85))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	rows := insertFinalDevMachineFixture(t, db, fixture)
	_, err = db.Exec(`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,5173,'user',$3)`, fixture.workspaceA, rows.machineA, fixture.userA)
	require.NoError(t, err)
	insertDocker := `INSERT INTO dev_machine_services(workspace_id,machine_id,service_type,service_key,container_name,image_ref,internal_host,internal_port)
		VALUES($1,$2,'docker','docker','kuayle-a-docker','fixture','a-docker',2376)`
	_, err = db.Exec(insertDocker, fixture.workspaceA, rows.machineA)
	require.Error(t, err, "the docker service type does not exist before 89")

	require.NoError(t, m.Migrate(89))
	var allowDocker bool
	require.NoError(t, db.QueryRow(`SELECT allow_docker FROM dev_machine_workspace_policies WHERE workspace_id=$1`, fixture.workspaceA).Scan(&allowDocker))
	require.False(t, allowDocker, "existing workspaces stay without a daemon")
	var target, dockerPorts string
	require.NoError(t, db.QueryRow(`SELECT target FROM dev_machine_ports WHERE machine_id=$1 AND port=5173`, rows.machineA).Scan(&target))
	require.Equal(t, "developer", target)
	require.NoError(t, db.QueryRow(`SELECT docker_listening_ports::text FROM dev_machines WHERE id=$1`, rows.machineA).Scan(&dockerPorts))
	require.Equal(t, "{}", dockerPorts)
	_, err = db.Exec(insertDocker, fixture.workspaceA, rows.machineA)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_ports SET target='docker' WHERE machine_id=$1`, rows.machineA)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_ports SET target='host' WHERE machine_id=$1`, rows.machineA)
	require.Error(t, err, "a port is served by the developer or the docker service only")
	for port := 3000; port < 3020; port++ {
		_, err = db.Exec(`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,$3,'agent',$4)`, fixture.workspaceA, rows.machineA, port, fixture.userA)
		require.NoError(t, err, "the schema does not limit the number of ports")
	}

	require.NoError(t, m.Steps(-1))
	version, _, err := m.Version()
	require.NoError(t, err)
	require.Less(t, version, uint(89))
	var services, ports int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_services WHERE machine_id=$1 AND service_key='docker'`, rows.machineA).Scan(&services))
	require.Zero(t, services, "rollback removes daemon service rows")
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_ports WHERE machine_id=$1`, rows.machineA).Scan(&ports))
	require.Equal(t, 21, ports, "ports are kept")
	require.NoError(t, m.Migrate(89), "the migration can be applied again")
}
