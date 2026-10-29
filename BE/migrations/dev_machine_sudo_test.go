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

// Migration 93 adds the opt-in sudo switch to the workspace policy and the
// per-machine record of it. Existing workspaces and machines stay without
// sudo; rollback drops both columns.
func TestDevMachineSudoMigrationIsOptInAndRollsBack(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_sudo_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	// 89 is the last migration this one depends on; streams merged in
	// between (90-92) do not touch these columns.
	require.NoError(t, m.Migrate(89))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	rows := insertFinalDevMachineFixture(t, db, fixture)

	require.NoError(t, m.Migrate(93))
	var allowSudo, sudoEnabled bool
	require.NoError(t, db.QueryRow(`SELECT allow_sudo FROM dev_machine_workspace_policies WHERE workspace_id=$1`, fixture.workspaceA).Scan(&allowSudo))
	require.False(t, allowSudo, "existing workspaces stay without sudo")
	require.NoError(t, db.QueryRow(`SELECT sudo_enabled FROM dev_machines WHERE id=$1`, rows.machineA).Scan(&sudoEnabled))
	require.False(t, sudoEnabled, "existing machines stay without sudo")
	_, err = db.Exec(`UPDATE dev_machines SET sudo_enabled=true WHERE id=$1`, rows.machineA)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	version, _, err := m.Version()
	require.NoError(t, err)
	require.Less(t, version, uint(93))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE (table_name='dev_machines' AND column_name='sudo_enabled')
		   OR (table_name='dev_machine_workspace_policies' AND column_name='allow_sudo')`).Scan(&columns))
	require.Zero(t, columns, "rollback drops both columns")
	require.NoError(t, m.Migrate(93), "the migration can be applied again")
}
