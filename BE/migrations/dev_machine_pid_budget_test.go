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

func TestDevMachinePIDBudgetMigration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_machine_pids_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(189))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)

	insertMachine := func(key string) int {
		t.Helper()
		var pids int
		require.NoError(t, db.QueryRow(`INSERT INTO dev_machines
			(workspace_id,routing_key,name,machine_size,cpu_millis,memory_mb,disk_gb)
			VALUES ($1,$2,$2,'small',2000,4096,20) RETURNING pids_limit`, fixture.workspaceA, key).Scan(&pids))
		return pids
	}
	require.Equal(t, 512, insertMachine("initialdefault"))
	insertMachine("explicitlimit")
	_, err = db.Exec(`UPDATE dev_machines SET pids_limit=2048 WHERE routing_key='explicitlimit'`)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(190))
	limits := func() []int {
		t.Helper()
		var pids []int
		rows, err := db.Query(`SELECT pids_limit FROM dev_machines WHERE routing_key IN ('explicitlimit','initialdefault') ORDER BY routing_key`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var limit int
			require.NoError(t, rows.Scan(&limit))
			pids = append(pids, limit)
		}
		require.NoError(t, rows.Err())
		return pids
	}
	require.Equal(t, []int{2048, 4096}, limits(), "only the old default increases")
	require.Equal(t, 4096, insertMachine("updateddefault"))

	require.NoError(t, m.Steps(-1))
	require.Equal(t, []int{2048, 4096}, limits(), "rollback preserves the stored limits")
	require.Equal(t, 512, insertMachine("rollbackdefault"))
}
