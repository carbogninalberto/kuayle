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

func TestMachineAuditSurvivesParentDeletion(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_audit_delete_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		_ = admin.Close()
	})
	testURL := databaseURLWithName(t, databaseURL, name)
	m, err := migrate.New("file://.", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(48))
	insert := func(workspace, user uuid.UUID) (uuid.UUID, uuid.UUID, int64) {
		machine, service := uuid.New(), uuid.New()
		key := strings.ReplaceAll(machine.String(), "-", "")
		_, err := db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at)
   VALUES($1,$2,$3,$4,$4,'destroyed','destroyed','small',2000,4096,20,480,NOW())`, machine, workspace, user, key)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_services(id,workspace_id,machine_id,service_type,service_key,container_name,image_ref,internal_host,internal_port,status)
   VALUES($1,$2,$3,'ide','ide',$4,'test-image',$4,8080,'stopped')`, service, workspace, machine, key)
		require.NoError(t, err)
		var logID int64
		err = db.QueryRow(`INSERT INTO dev_machine_access_logs(workspace_id,machine_id,service_id,user_id,decision,reason,method,path,response_status)
   VALUES($1,$2,$3,$4,'allowed','test access','GET','/',200) RETURNING id`, workspace, machine, service, user).Scan(&logID)
		require.NoError(t, err)
		return machine, service, logID
	}
	a, serviceA, logA := insert(fixture.workspaceA, fixture.userA)
	b, serviceB, logB := insert(fixture.workspaceB, fixture.userB)
	err = m.Up()
	if err != migrate.ErrNoChange {
		require.NoError(t, err)
	}
	// Keep composite ownership validation and null-dependency checks intact.
	_, err = db.Exec(`INSERT INTO dev_machine_access_logs(workspace_id,machine_id,service_id,decision,method,path) VALUES($1,$2,$3,'denied','GET','/')`, fixture.workspaceB, a, serviceA)
	require.Equal(t, "23503", postgresCode(err))
	_, err = db.Exec(`INSERT INTO dev_machine_access_logs(service_id,decision,method,path) VALUES($1,'denied','GET','/')`, serviceA)
	require.Equal(t, "23514", postgresCode(err))
	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`DELETE FROM dev_machines WHERE id=$1`, a)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	var restoredMachine, restoredService string
	require.NoError(t, db.QueryRow(`SELECT machine_id::text,service_id::text FROM dev_machine_access_logs WHERE id=$1`, logA).Scan(&restoredMachine, &restoredService))
	require.Equal(t, a.String(), restoredMachine)
	require.Equal(t, serviceA.String(), restoredService)
	_, err = db.Exec(`DELETE FROM dev_machines WHERE id=$1`, a)
	require.NoError(t, err, "purge must detach both dependent audit references atomically")
	var workspace, machine, service sql.NullString
	var reason string
	read := func(id int64) {
		require.NoError(t, db.QueryRow(`SELECT workspace_id::text,machine_id::text,service_id::text,reason FROM dev_machine_access_logs WHERE id=$1`, id).Scan(&workspace, &machine, &service, &reason))
	}
	read(logA)
	require.Equal(t, fixture.workspaceA.String(), workspace.String)
	require.False(t, machine.Valid)
	require.False(t, service.Valid)
	require.Equal(t, "test access", reason)
	read(logB)
	require.Equal(t, b.String(), machine.String)
	require.Equal(t, serviceB.String(), service.String)
	_, err = db.Exec(`DELETE FROM dev_machine_services WHERE id=$1`, serviceB)
	require.NoError(t, err)
	read(logB)
	require.Equal(t, b.String(), machine.String)
	require.False(t, service.Valid)
	_, _, logB2 := insert(fixture.workspaceB, fixture.userB)
	_, err = db.Exec(`DELETE FROM workspaces WHERE id=$1`, fixture.workspaceB)
	require.NoError(t, err)
	for _, id := range []int64{logB, logB2} {
		read(id)
		require.False(t, workspace.Valid)
		require.False(t, machine.Valid)
		require.False(t, service.Valid)
		require.Equal(t, "test access", reason)
	}
	read(logA)
	require.Equal(t, fixture.workspaceA.String(), workspace.String)
	require.NoError(t, m.Steps(-1))
	requireMigrationVersion(t, m, 48)
	c, _, logC := insert(fixture.workspaceA, fixture.userA)
	_, err = db.Exec(`DELETE FROM dev_machines WHERE id=$1`, c)
	require.Equal(t, "23514", postgresCode(err))
	require.NoError(t, m.Steps(1))
	requireMigrationVersion(t, m, 49)
	_, err = db.Exec(`DELETE FROM dev_machines WHERE id=$1`, c)
	require.NoError(t, err)
	read(logC)
	require.False(t, machine.Valid)
	require.False(t, service.Valid)
}
