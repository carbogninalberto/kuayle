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

// Migration 177 adds run_migration to the approval action list, keeps every
// action an earlier migration allowed, and adds the named migration actions
// and the migration mark of command runs; rolling back removes only those.
func TestAssistantMigrationActionsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_migration_actions_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	actions := func() string {
		t.Helper()
		var definition string
		require.NoError(t, db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='conversation_approvals_action_check'`).Scan(&definition))
		return definition
	}

	require.NoError(t, m.Migrate(176))
	before := actions()
	require.Contains(t, before, "'run_command_write'")

	require.NoError(t, m.Migrate(177))
	after := actions()
	require.Contains(t, after, "'run_migration'")
	for _, action := range []string{"message_task", "start_temporary_machine", "run_command", "run_command_write"} {
		require.Contains(t, after, "'"+action+"'", "every earlier action is kept")
	}
	fixture := insertBaseMigrationFixture(t, db)
	var machine uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,created_by_user_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,'migrationactions0000','Migrations','running','running','small',1000,2048,20) RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&machine))
	_, err = db.Exec(`INSERT INTO dev_machine_migration_actions(workspace_id,machine_id,name,description,command,encrypted_secret)
  VALUES($1,$2,'migrate-db','Run the database migrations','migrate -url "$KUAYLE_MIGRATION_SECRET" up','sealed')`, fixture.workspaceA, machine)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM dev_machine_migration_actions`).Scan(&count))
	require.Equal(t, 1, count)
	// The per-machine name is unique ignoring case.
	_, err = db.Exec(`INSERT INTO dev_machine_migration_actions(workspace_id,machine_id,name,description,command,encrypted_secret)
  VALUES($1,$2,'Migrate-DB','duplicate','true','sealed')`, fixture.workspaceA, machine)
	require.Error(t, err, "duplicate action names are refused")
	// A migration run names its action and never writes files.
	_, err = db.Exec(`INSERT INTO assistant_command_runs(workspace_id,machine_id,machine_name,cpu_millis,memory_mb,disk_gb,command,migration,action_name,working_directory,status)
  VALUES($1,$2,'machine',1000,2048,20,'migrate up',true,'migrate-db','/workspace','queued')`, fixture.workspaceA, machine)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE assistant_command_runs SET write=true WHERE migration`)
	require.Error(t, err, "a migration run never writes files")

	require.NoError(t, m.Migrate(176))
	require.Equal(t, before, actions(), "rolling back restores the earlier list")
	var gone bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('public.dev_machine_migration_actions') IS NULL`).Scan(&gone))
	require.True(t, gone, "rolling back removes the migration actions")
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='assistant_command_runs' AND column_name IN ('migration','action_name')`).Scan(&columns))
	require.Zero(t, columns, "rolling back removes the migration mark of command runs")
}
