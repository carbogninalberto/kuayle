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

// Custom tools add their tables, the run_tool operation, the run_custom_tool
// approval and digits in tool-call names; rolling back keeps every other
// approval action and conversation record, and refuses while a run is active.
func TestCustomToolMigrationsRoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_custom_tool_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(100))
	constraint := func(name string) string {
		var definition string
		require.NoError(t, db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1`, name).Scan(&definition))
		return definition
	}
	actions := constraint("conversation_approvals_action_check")
	for _, action := range []string{"delegate_issue", "merge_pull_request", "request_agent_review", "follow_up_task", "accept_agent_work", "start_preview", "expose_port", "run_check", "run_custom_tool"} {
		require.Contains(t, actions, action)
	}
	var tool uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO assistant_custom_tools(workspace_id,name,description,parameters,script,interpreter,run_mode,status,current_version,author_kind)
 VALUES($1,'run_e2e','Run','{"type":"object"}','echo','sh','approval','active',1,'user') RETURNING id`, fixture.workspaceA).Scan(&tool))
	_, err = db.Exec(`INSERT INTO assistant_custom_tools(workspace_id,name,description,parameters,script,interpreter,run_mode,status,current_version,author_kind)
 VALUES($1,'auto_tool','Run','{"type":"object"}','echo','sh','auto','active',1,'user')`, fixture.workspaceA)
	require.Error(t, err, "auto runs need read_only")
	var machine uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at) VALUES($1,'customtoolmigration','Tools','running','running','small',1000,2048,20,60,now()+interval '1 hour') RETURNING id`, fixture.workspaceA).Scan(&machine))
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,action,status,generation,idempotency_key) VALUES($1,$2,'run_tool','pending',1,'tool-run:x')`, fixture.workspaceA, machine)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1), "an active run refuses rollback")
	require.NoError(t, m.Force(100))
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE idempotency_key='tool-run:x'`)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	require.NotContains(t, constraint("conversation_approvals_action_check"), "run_custom_tool")
	require.Contains(t, constraint("conversation_approvals_action_check"), "run_check")
	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('assistant_custom_tools') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, m.Migrate(100))
}
