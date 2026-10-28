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

// Migration 79 raises machine limits to at least 20 per user and 20 per
// workspace, for existing policies and as defaults, and its rollback restores
// the old defaults without lowering any policy.
func TestDevMachinePolicyLimitsMigrationRaisesLimits(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_policy_limits_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(78))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	limits := func(workspace uuid.UUID) (int, int) {
		t.Helper()
		var concurrent, perUser int
		require.NoError(t, db.QueryRow(`SELECT max_concurrent_machines,max_machines_per_user FROM dev_machine_workspace_policies WHERE workspace_id=$1`, workspace).Scan(&concurrent, &perUser))
		return concurrent, perUser
	}
	// Workspace A keeps the old defaults; B has a custom high workspace cap.
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE)
		ON CONFLICT (workspace_id) DO UPDATE SET max_concurrent_machines=5,max_machines_per_user=2`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,max_concurrent_machines,max_machines_per_user) VALUES($1,TRUE,60,4)
		ON CONFLICT (workspace_id) DO UPDATE SET max_concurrent_machines=60,max_machines_per_user=4`, fixture.workspaceB)
	require.NoError(t, err)
	concurrent, perUser := limits(fixture.workspaceA)
	require.Equal(t, []int{5, 2}, []int{concurrent, perUser})

	require.NoError(t, m.Migrate(79))
	concurrent, perUser = limits(fixture.workspaceA)
	require.Equal(t, []int{20, 20}, []int{concurrent, perUser})
	concurrent, perUser = limits(fixture.workspaceB)
	require.Equal(t, []int{60, 20}, []int{concurrent, perUser}, "higher limits are kept")
	// New policies default to 20/20; admins can still lower them.
	_, err = db.Exec(`DELETE FROM dev_machine_workspace_policies WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE)`, fixture.workspaceA)
	require.NoError(t, err)
	concurrent, perUser = limits(fixture.workspaceA)
	require.Equal(t, []int{20, 20}, []int{concurrent, perUser})
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET max_machines_per_user=3 WHERE workspace_id=$1`, fixture.workspaceB)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	requireMigrationVersion(t, m, 78)
	concurrent, perUser = limits(fixture.workspaceB)
	require.Equal(t, []int{60, 3}, []int{concurrent, perUser}, "rollback never changes a policy")
	_, err = db.Exec(`DELETE FROM dev_machine_workspace_policies WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE)`, fixture.workspaceA)
	require.NoError(t, err)
	concurrent, perUser = limits(fixture.workspaceA)
	require.Equal(t, []int{5, 2}, []int{concurrent, perUser}, "rollback restores the old defaults")

	require.NoError(t, m.Steps(1))
	concurrent, perUser = limits(fixture.workspaceA)
	require.Equal(t, []int{20, 20}, []int{concurrent, perUser})
}
