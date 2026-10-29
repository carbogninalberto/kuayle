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

func TestManualCheckMigrationRetainsResultsAndRefusesDowngrade(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_manual_check_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(97))
	approvalActions := func() string {
		var definition string
		require.NoError(t, db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='conversation_approvals_action_check'`).Scan(&definition))
		return definition
	}
	require.Contains(t, approvalActions(), "run_check")
	require.NoError(t, m.Steps(-1))
	require.NotContains(t, approvalActions(), "run_check")
	require.Contains(t, approvalActions(), "expose_port")
	require.NoError(t, m.Steps(1))
	var machine uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at) VALUES($1,'manualcheckmigration','Manual check','running','running','small',1000,2048,20,60,now()+interval '1 hour') RETURNING id`, fixture.workspaceA).Scan(&machine))
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'run_check','pending',1,'migration-check')`, fixture.workspaceA, machine)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1))
	require.NoError(t, m.Force(97))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_operations WHERE action='run_check'`).Scan(&count))
	require.Equal(t, 1, count)
}
