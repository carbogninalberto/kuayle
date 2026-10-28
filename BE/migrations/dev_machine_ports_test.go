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

// Migration 85 turns the one preview port per machine into app ports: the
// existing preview becomes a user port, a machine may expose several, and a
// preview command no longer needs a port. Rollback keeps the lowest port.
func TestDevMachinePortsMigrationKeepsPreviewsAndRollsBackToTheLowestPort(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_ports_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(84))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	rows := insertFinalDevMachineFixture(t, db, fixture)
	_, err = db.Exec(`INSERT INTO dev_machine_previews(machine_id,workspace_id,port,created_by_user_id) VALUES($1,$2,5173,$3)`, rows.machineA, fixture.workspaceA, fixture.userA)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(85))
	var port int
	var source string
	require.NoError(t, db.QueryRow(`SELECT port,source FROM dev_machine_ports WHERE machine_id=$1`, rows.machineA).Scan(&port, &source))
	require.Equal(t, 5173, port)
	require.Equal(t, "user", source)
	_, err = db.Exec(`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id,label) VALUES($1,$2,3000,'agent',$3,'web app')`, fixture.workspaceA, rows.machineA, fixture.userA)
	require.NoError(t, err, "a machine exposes several ports")
	assertUniqueViolation(t, db, `INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,3000,'user',$3)`, fixture.workspaceA, rows.machineA, fixture.userA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,4000,'user',$3)`, fixture.workspaceB, rows.machineA, fixture.userA)
	for _, bad := range []string{
		`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,80,'user',$3)`,
		`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id) VALUES($1,$2,4001,'robot',$3)`,
		`INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id,label) VALUES($1,$2,4002,'user',$3,'<script>')`,
	} {
		_, err = db.Exec(bad, fixture.workspaceA, rows.machineA, fixture.userA)
		require.Error(t, err, bad)
	}
	var listening []byte
	require.NoError(t, db.QueryRow(`SELECT listening_ports::text FROM dev_machines WHERE id=$1`, rows.machineA).Scan(&listening))
	require.Equal(t, "{}", string(listening))
	_, err = db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,issue_id,preview_command) VALUES($1,$2,'["npm","run","dev"]')
		ON CONFLICT DO NOTHING`, fixture.workspaceB, fixture.issueB)
	require.NoError(t, err, "a preview command may omit the preferred port")
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET preview_command='["npm","run","dev"]' WHERE workspace_id=$1`, fixture.workspaceB)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET preview_command=NULL,preview_port=5173 WHERE workspace_id=$1`, fixture.workspaceA)
	require.Error(t, err, "a port without a command is refused")

	require.NoError(t, m.Steps(-1))
	requireMigrationVersion(t, m, 84)
	require.NoError(t, db.QueryRow(`SELECT port FROM dev_machine_previews WHERE machine_id=$1`, rows.machineA).Scan(&port))
	require.Equal(t, 3000, port, "rollback keeps the lowest port")
	var commands int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_scope_settings WHERE preview_command IS NOT NULL`).Scan(&commands))
	require.Zero(t, commands, "a command without a port cannot be represented before 85")
	require.NoError(t, m.Migrate(85), "the migration can be applied again")
}
