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

// Migration 118 adds configurable workflows. Existing tasks keep the
// Standard flow (no pinned version), versions are immutable, a workflow
// never crosses workspaces, and rolling back refuses while a task still runs
// a configurable workflow.
func TestWorkflowsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_workflows_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(117))

	var issue, team, conversation, existing uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Workflows',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex') RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&existing))

	require.NoError(t, m.Migrate(118))

	// An existing task runs the Standard flow: nothing is pinned.
	var pinned sql.NullString
	var visits string
	var extensions int
	require.NoError(t, db.QueryRow(`SELECT workflow_version_id, workflow_visits::text, workflow_budget_extensions FROM agent_tasks WHERE id=$1`, existing).Scan(&pinned, &visits, &extensions))
	require.False(t, pinned.Valid)
	require.Equal(t, "{}", visits)
	require.Zero(t, extensions)

	var workflow, version uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO workflows(workspace_id,name,latest_version) VALUES($1,'Review loop',1) RETURNING id`, fixture.workspaceA).Scan(&workflow))
	require.NoError(t, db.QueryRow(`INSERT INTO workflow_versions(workflow_id,workspace_id,version,graph) VALUES($1,$2,1,'{"nodes":[],"edges":[]}') RETURNING id`, workflow, fixture.workspaceA).Scan(&version))

	for _, bad := range []struct {
		query string
		args  []any
	}{
		// Versions are immutable.
		{`UPDATE workflow_versions SET graph='{}' WHERE id=$1`, []any{version}},
		// A version or default never points at another workspace's workflow.
		{`INSERT INTO workflow_versions(workflow_id,workspace_id,version,graph) VALUES($1,$2,2,'{}')`, []any{workflow, fixture.workspaceB}},
		{`INSERT INTO workflow_defaults(workspace_id,scope,scope_id,workflow_id) VALUES($1,'workspace',$1,$2)`, []any{fixture.workspaceB, workflow}},
		// A workspace default is scoped to its own workspace.
		{`INSERT INTO workflow_defaults(workspace_id,scope,scope_id) VALUES($1,'workspace',$2)`, []any{fixture.workspaceA, fixture.workspaceB}},
		// A pinned version needs a cursor.
		{`UPDATE agent_tasks SET workflow_version_id=$2 WHERE id=$1`, []any{existing, version}},
		{`INSERT INTO agent_profiles(workspace_id,name,role,harness) VALUES($1,'Bad','manager','codex')`, []any{fixture.workspaceA}},
	} {
		_, err := db.Exec(bad.query, bad.args...)
		require.Error(t, err, bad.query)
	}
	_, err = db.Exec(`INSERT INTO workflow_defaults(workspace_id,scope,scope_id,workflow_id) VALUES($1,'workspace',$1,$2)`, fixture.workspaceA, workflow)
	require.NoError(t, err)

	// Rolling back refuses while a task runs a configurable workflow ...
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_version_id=$2,workflow_node='implement',workflow_status='running' WHERE id=$1`, existing, version)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('workflows') IS NOT NULL`).Scan(&exists))
	require.True(t, exists, "a refused rollback keeps the workflow tables")

	// ... and removes everything once none does.
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_version_id=NULL,workflow_node=NULL,workflow_status=NULL WHERE id=$1`, existing)
	require.NoError(t, err)
	require.NoError(t, m.Force(118))
	require.NoError(t, m.Steps(-1))
	for _, table := range []string{"agent_profiles", "workflows", "workflow_versions", "workflow_defaults", "workflow_audit_events", "agent_task_workflow_steps"} {
		require.NoError(t, db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists))
		require.False(t, exists, table)
	}
	for _, name := range []string{"workflow_version_id", "workflow_budget_extensions", "workflow_budget_from"} {
		var column bool
		require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_tasks' AND column_name=$1)`, name).Scan(&column))
		require.False(t, column, name)
	}
}
