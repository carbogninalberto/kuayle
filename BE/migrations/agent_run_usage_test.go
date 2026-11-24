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

// Runs recorded before the usage columns did not report usage (NULL), a
// count or a cost is never negative, and rolling back removes only the
// columns.
func TestAgentRunUsageMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_run_usage_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(202))
	var machine uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,routing_key,name,machine_size,cpu_millis,memory_mb,disk_gb) VALUES($1,'usagemigration','Usage','small',1000,2048,20) RETURNING id`, fixture.workspaceA).Scan(&machine))
	run := uuid.New()
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,'codex','autonomous','Implement','["codex"]',60,'succeeded')`, run, fixture.workspaceA, machine)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(204))
	var reported bool
	require.NoError(t, db.QueryRow(`SELECT input_tokens IS NOT NULL OR output_tokens IS NOT NULL OR cache_read_tokens IS NOT NULL OR cache_write_tokens IS NOT NULL OR cost_usd IS NOT NULL FROM dev_machine_agent_runs WHERE id=$1`, run).Scan(&reported))
	require.False(t, reported, "an earlier run reported nothing")
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET input_tokens=315,output_tokens=122,cache_read_tokens=24448,cache_write_tokens=0,cost_usd=0.012345678 WHERE id=$1`, run)
	require.NoError(t, err)
	var cost string
	require.NoError(t, db.QueryRow(`SELECT cost_usd::text FROM dev_machine_agent_runs WHERE id=$1`, run).Scan(&cost))
	require.Equal(t, "0.012345678", cost, "the harness's estimate is kept as reported")
	for _, column := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd"} {
		_, err = db.Exec(`UPDATE dev_machine_agent_runs SET `+column+`=-1 WHERE id=$1`, run)
		require.Error(t, err, column)
	}

	require.NoError(t, m.Steps(-1))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='dev_machine_agent_runs' AND column_name IN ('input_tokens','output_tokens','cache_read_tokens','cache_write_tokens','cost_usd')`).Scan(&columns))
	require.Zero(t, columns)
	var kept int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_agent_runs WHERE id=$1`, run).Scan(&kept))
	require.Equal(t, 1, kept, "the run itself stays")
	require.NoError(t, m.Steps(1))
}
