package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Migration 101 removes machine lifetimes. Existing machines keep running
// whatever their old expiry, machine credentials stop expiring, a teardown the
// old reaper queued but never started becomes a stop, and rollback restores
// the columns with a fresh lifetime.
func TestDevMachinesPersistentMigrationKeepsExistingMachines(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_persistent_machines_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(100))
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,idle_pause_minutes,max_runtime_minutes) VALUES($1,TRUE,30,60)`, fixture.workspaceA)
	require.NoError(t, err)

	insert := func(key, status, desired, expires string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,created_by_user_id,routing_key,name,status,desired_status,
			machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at)
			VALUES($1,$2,$3,$3,$4,$5,'small',1000,2048,20,60,now()+$6::interval) RETURNING id`,
			fixture.workspaceA, fixture.userA, key, status, desired, expires).Scan(&id))
		return id
	}
	// Past its old expiry but not reaped yet, and well within it.
	overdue := insert("persistentoverdue", "running", "running", "-1 hour")
	current := insert("persistentcurrent", "running", "running", "1 hour")
	// Marked expired with its teardown still pending: rescued.
	rescued := insert("persistentrescued", "expired", "destroyed", "-1 minute")
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'teardown','pending',2,'ttl-teardown:2')`, fixture.workspaceA, rescued)
	require.NoError(t, err)
	// A teardown already leased runs to completion.
	leased := insert("persistentleased", "expired", "destroyed", "-1 minute")
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key,lease_owner,lease_expires_at) VALUES($1,$2,'teardown','leased',2,'ttl-teardown:2','manager',now()+interval '1 minute')`, fixture.workspaceA, leased)
	require.NoError(t, err)
	// A user's own teardown is untouched.
	manual := insert("persistentmanual", "running", "destroyed", "1 hour")
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'teardown','pending',2,'teardown:user')`, fixture.workspaceA, manual)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_tokens(machine_id,token_hash,scopes,expires_at) VALUES($1,$2,'["events:write"]',now()+interval '1 hour')`, current, strings.Repeat("a", 64))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_env_vars(machine_id,target_service,name,encrypted_value,expires_at) VALUES
		($1,'collector','KUAYLE_MACHINE_TOKEN','opaque',now()+interval '1 hour'),
		($1,'agent','USER_SECRET','opaque',now()+interval '1 hour')`, current)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(101))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE (table_name='dev_machines' AND column_name IN ('expires_at','max_runtime_minutes','keep_running'))
		   OR (table_name='dev_machine_workspace_policies' AND column_name IN ('max_runtime_minutes','idle_pause_minutes'))`).Scan(&columns))
	require.Zero(t, columns, "the lifetime and idle columns are dropped")
	state := func(id uuid.UUID) string {
		t.Helper()
		var status, desired string
		require.NoError(t, db.QueryRow(`SELECT status,desired_status FROM dev_machines WHERE id=$1`, id).Scan(&status, &desired))
		return status + "/" + desired
	}
	require.Equal(t, "running/running", state(overdue), "a machine past its old expiry keeps running")
	require.Equal(t, "running/running", state(current))
	require.Equal(t, "expired/stopped", state(rescued), "the manager stops it instead of tearing it down")
	require.Equal(t, "expired/destroyed", state(leased))
	require.Equal(t, "running/destroyed", state(manual))
	opStatus := func(id uuid.UUID) string {
		t.Helper()
		var status string
		require.NoError(t, db.QueryRow(`SELECT status FROM dev_machine_operations WHERE machine_id=$1 AND action='teardown'`, id).Scan(&status))
		return status
	}
	require.Equal(t, "cancelled", opStatus(rescued))
	require.Equal(t, "leased", opStatus(leased))
	require.Equal(t, "pending", opStatus(manual))
	var tokenExpiry, collectorExpiry, userExpiry sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM dev_machine_tokens WHERE machine_id=$1`, current).Scan(&tokenExpiry))
	require.False(t, tokenExpiry.Valid, "the collector token no longer expires")
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM dev_machine_env_vars WHERE machine_id=$1 AND name='KUAYLE_MACHINE_TOKEN'`, current).Scan(&collectorExpiry))
	require.False(t, collectorExpiry.Valid)
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM dev_machine_env_vars WHERE machine_id=$1 AND name='USER_SECRET'`, current).Scan(&userExpiry))
	require.True(t, userExpiry.Valid, "a user's own secret keeps its time to live")
	// New machines are created without a lifetime.
	_, err = db.Exec(`INSERT INTO dev_machines(workspace_id,created_by_user_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb)
		VALUES($1,$2,'persistentcreated','created','queued','running','small',1000,2048,20)`, fixture.workspaceA, fixture.userA)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	var expiresAt sql.NullTime
	var runtime int
	require.NoError(t, db.QueryRow(`SELECT expires_at,max_runtime_minutes FROM dev_machines WHERE id=$1`, overdue).Scan(&expiresAt, &runtime))
	require.True(t, expiresAt.Valid && expiresAt.Time.After(time.Now().Add(time.Hour)), "rollback gives machines a fresh lifetime")
	require.Equal(t, 480, runtime)
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM dev_machine_tokens WHERE machine_id=$1`, current).Scan(&tokenExpiry))
	require.True(t, tokenExpiry.Valid, "rollback restores the token expiry")
	var idle int
	require.NoError(t, db.QueryRow(`SELECT idle_pause_minutes FROM dev_machine_workspace_policies WHERE workspace_id=$1`, fixture.workspaceA).Scan(&idle))
	require.Equal(t, 240, idle)
	require.NoError(t, m.Migrate(101), "the migration can be applied again")
}
