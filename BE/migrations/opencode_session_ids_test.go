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

// OpenCode's session IDs (ses_…) contain an underscore. Migration 203 lets
// an attempt and its task's agent session store them, still refusing IDs
// that could be read as a flag; rolling back forgets only those IDs, so the
// older check holds again and OpenCode tasks start a new session.
func TestOpenCodeSessionIDMigrationAllowsUnderscores(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_opencode_session_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(82))
	var issue, team, conversation, codexTask, codexRun, openCodeTask, openCodeRun, machine uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Sessions',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	insertTask := `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state,generation) VALUES($1,$2,$3,$4,$5,$6,'Implement',$7,'awaiting_review',1) RETURNING id,run_id`
	require.NoError(t, db.QueryRow(insertTask, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New(), "codex").Scan(&codexTask, &codexRun))
	require.NoError(t, db.QueryRow(insertTask, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New(), "opencode").Scan(&openCodeTask, &openCodeRun))
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at) VALUES($1,'opencodesessions','Sessions','running','running','small',1000,2048,20,60,now()+interval '1 hour') RETURNING id`, fixture.workspaceA).Scan(&machine))
	insertRun := `INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at) VALUES($1,$2,$3,$4,$5,'autonomous','Implement','["agent"]',60,'succeeded',now())`
	_, err = db.Exec(insertRun, codexRun, fixture.workspaceA, machine, codexTask, "codex")
	require.NoError(t, err)
	_, err = db.Exec(insertRun, openCodeRun, fixture.workspaceA, machine, openCodeTask, "opencode")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(202))

	const codexThread, openCodeSession = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", "ses_df21986abffefn2FxDuH6uZGLA"
	setRun := func(run uuid.UUID, id string) error {
		_, err := db.Exec(`UPDATE dev_machine_agent_runs SET native_session_id=$2 WHERE id=$1`, run, id)
		return err
	}
	setSession := func(task uuid.UUID, id string) error {
		_, err := db.Exec(`UPDATE agent_sessions SET native_session_id=$2 WHERE task_id=$1`, task, id)
		return err
	}
	require.NoError(t, setRun(codexRun, codexThread))
	require.NoError(t, setSession(codexTask, codexThread))
	require.Error(t, setRun(openCodeRun, openCodeSession), "before the migration an underscore is refused")
	require.Error(t, setSession(openCodeTask, openCodeSession))

	require.NoError(t, m.Migrate(203))
	require.NoError(t, setRun(openCodeRun, openCodeSession))
	require.NoError(t, setSession(openCodeTask, openCodeSession))
	for _, unsafe := range []string{"_ses", "-ses", "--last", "ses_x; rm -rf /", "ses x", strings.Repeat("s", 129)} {
		require.Error(t, setRun(openCodeRun, unsafe), unsafe)
		require.Error(t, setSession(openCodeTask, unsafe), unsafe)
	}
	require.NoError(t, setRun(openCodeRun, "ses_"+strings.Repeat("s", 124)), "128 characters is the limit")

	require.NoError(t, m.Steps(-1))
	var runs, sessions string
	require.NoError(t, db.QueryRow(`SELECT string_agg(provider_id || '=' || COALESCE(native_session_id,'none'), ',' ORDER BY provider_id) FROM dev_machine_agent_runs WHERE workspace_id=$1`, fixture.workspaceA).Scan(&runs))
	require.Equal(t, "codex="+codexThread+",opencode=none", runs, "rolling back forgets only the IDs the older check refuses")
	require.NoError(t, db.QueryRow(`SELECT string_agg(provider || '=' || COALESCE(native_session_id,'none'), ',' ORDER BY provider) FROM agent_sessions WHERE workspace_id=$1`, fixture.workspaceA).Scan(&sessions))
	require.Equal(t, "codex="+codexThread+",opencode=none", sessions)
	require.Error(t, setRun(openCodeRun, openCodeSession), "the older check holds again")
	require.Error(t, setSession(openCodeTask, openCodeSession))
	require.NoError(t, m.Steps(1))
	require.NoError(t, setRun(openCodeRun, openCodeSession))
}
