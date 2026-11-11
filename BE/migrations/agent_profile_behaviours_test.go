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

// Migration 143 gives every agent profile a behaviour and turns its role
// into a label. Reviewer and security reviewer profiles review, the others
// change code, so the workflows that use them run as before; the old role
// stays as the label. Rolling back restores the fixed roles.
func TestAgentProfileBehavioursMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_profile_behaviours_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(142))

	want := map[string]struct{ behaviour, label string }{
		"implementer":       {"changes_code", "Implementer"},
		"reviewer":          {"reviews", "Reviewer"},
		"tester":            {"changes_code", "Tester"},
		"docs_writer":       {"changes_code", "Docs writer"},
		"security_reviewer": {"reviews", "Security reviewer"},
	}
	ids := map[string]uuid.UUID{}
	for role := range want {
		var id uuid.UUID
		require.NoError(t, db.QueryRow(`INSERT INTO agent_profiles(workspace_id,name,role,harness,instructions) VALUES($1,$2,$3,'codex','Keep it small.') RETURNING id`, fixture.workspaceA, "The "+role, role).Scan(&id))
		ids[role] = id
	}
	// A workflow version that uses the reviewer keeps its graph untouched.
	graph := `{"nodes":[{"id":"review","type":"agent","label":"Review","profile_id":"` + ids["reviewer"].String() + `","position":{"x":0,"y":0}}],"edges":[]}`
	var workflow, version uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO workflows(workspace_id,name,latest_version) VALUES($1,'Review loop',1) RETURNING id`, fixture.workspaceA).Scan(&workflow))
	require.NoError(t, db.QueryRow(`INSERT INTO workflow_versions(workflow_id,workspace_id,version,graph) VALUES($1,$2,1,$3) RETURNING id`, workflow, fixture.workspaceA, graph).Scan(&version))

	require.NoError(t, m.Migrate(143))
	for role, expected := range want {
		var behaviour, label, instructions string
		require.NoError(t, db.QueryRow(`SELECT behaviour,role,instructions FROM agent_profiles WHERE id=$1`, ids[role]).Scan(&behaviour, &label, &instructions))
		require.Equal(t, expected.behaviour, behaviour, role)
		require.Equal(t, expected.label, label, role)
		require.Equal(t, "Keep it small.", instructions)
	}
	var kept bool
	require.NoError(t, db.QueryRow(`SELECT graph=$2::jsonb FROM workflow_versions WHERE id=$1`, version, graph).Scan(&kept))
	require.True(t, kept)

	// Any label is a role now, with any of the three behaviours.
	var planner uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO agent_profiles(workspace_id,name,role,behaviour,harness) VALUES($1,'Architect','Staff engineer · platform','plans','opencode') RETURNING id`, fixture.workspaceA).Scan(&planner))
	_, err = db.Exec(`INSERT INTO agent_profiles(workspace_id,name,harness) VALUES($1,'Unlabelled','codex')`, fixture.workspaceA)
	require.NoError(t, err, "a role is optional and a profile changes code unless it says otherwise")
	for _, bad := range []string{
		`INSERT INTO agent_profiles(workspace_id,name,role,behaviour,harness) VALUES($1,'Bad','x','manages','codex')`,
		`INSERT INTO agent_profiles(workspace_id,name,role,harness) VALUES($1,'Bad',repeat('r',41),'codex')`,
		`INSERT INTO agent_profiles(workspace_id,name,role,harness) VALUES($1,'Bad',' padded','codex')`,
	} {
		_, err = db.Exec(bad, fixture.workspaceA)
		require.Error(t, err, bad)
	}
	for table, column := range map[string]string{"agent_task_workflow_steps": "plan", "dev_machine_agent_runs": "worktree_changed"} {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2)`, table, column).Scan(&exists))
		require.True(t, exists, table)
	}

	// Rolling back restores the fixed roles; a planner becomes an implementer.
	require.NoError(t, m.Steps(-1))
	for role := range want {
		var restored string
		require.NoError(t, db.QueryRow(`SELECT role FROM agent_profiles WHERE id=$1`, ids[role]).Scan(&restored))
		require.Equal(t, role, restored)
	}
	var restored string
	require.NoError(t, db.QueryRow(`SELECT role FROM agent_profiles WHERE id=$1`, planner).Scan(&restored))
	require.Equal(t, "implementer", restored)
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_profiles' AND column_name='behaviour')`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, m.Migrate(143), "the migration re-applies cleanly")
}
