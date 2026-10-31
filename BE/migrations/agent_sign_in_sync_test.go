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

// Migration 103 adds the sync_agent_sign_ins operation; rolling back keeps the
// inert enum value and refuses while a sync is still queued.
func TestAgentSignInSyncMigrationRoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_sign_in_sync_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(103))
	var machine uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,created_by_user_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,$2,'signinsyncmigration','Sync','running','running','small',1000,2048,20) RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&machine))
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'sync_agent_sign_ins','pending',1,'agent-sign-ins:x')`, fixture.workspaceA, machine)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1), "a queued sync refuses rollback")
	require.NoError(t, m.Force(103))
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE idempotency_key='agent-sign-ins:x'`)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	require.NoError(t, m.Migrate(103))
}
