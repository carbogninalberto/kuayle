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

// Migration 162 adds the backup settings singleton and the run history on top
// of main's 161 (OIDC SSO), so databases already at 161 still receive it;
// rolling back removes both.
func TestBackupsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_backups_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(161))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	var before bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('backup_runs') IS NOT NULL`).Scan(&before))
	require.False(t, before, "161 does not create the backup tables")
	require.NoError(t, m.Migrate(162), "a database at 161 upgrades to the backups migration")

	var enabled bool
	var kind string
	var retention int
	require.NoError(t, db.QueryRow(`SELECT enabled, schedule_kind, retention_value FROM backup_settings WHERE id=1`).Scan(&enabled, &kind, &retention))
	require.False(t, enabled, "backups start disabled")
	require.Equal(t, "daily", kind)
	require.Equal(t, 7, retention)

	for _, bad := range []string{
		`INSERT INTO backup_settings(id) VALUES(2)`,
		`UPDATE backup_settings SET schedule_time='24:00'`,
		`UPDATE backup_settings SET retention_value=0`,
		`UPDATE backup_settings SET lease_token=gen_random_uuid()`,
		`INSERT INTO backup_runs(kind, status) VALUES('hourly', 'running')`,
		`INSERT INTO backup_runs(kind, status, upload_status) VALUES('manual', 'running', 'lost')`,
	} {
		_, err := db.Exec(bad)
		require.Error(t, err, bad)
	}
	_, err = db.Exec(`INSERT INTO backup_runs(kind, status, destination) VALUES('pre_upgrade', 'succeeded', 'updater:/srv/backup')`)
	require.NoError(t, err, "the updater can record its dump")
	var endpoint, bucket string
	require.NoError(t, db.QueryRow(`SELECT s3_endpoint, s3_bucket FROM backup_runs WHERE kind='pre_upgrade'`).Scan(&endpoint, &bucket))
	require.Empty(t, endpoint+bucket, "runs without a bucket copy record no location")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('backup_runs') IS NOT NULL OR to_regclass('backup_settings') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, m.Steps(1), "the migration applies again after a rollback")
}
