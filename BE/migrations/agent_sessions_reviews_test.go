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

func TestAgentSessionMigrationBackfillsAndRefusesLossyDowngrade(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_agent_session_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	var issue, team, conversation, task, run, machine uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Sessions',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state,generation) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex','awaiting_review',2) RETURNING id,run_id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task, &run))
	require.NoError(t, db.QueryRow(`INSERT INTO dev_machines(workspace_id,routing_key,name,status,desired_status,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at) VALUES($1,'sessionmigration','Sessions','running','running','small',1000,2048,20,60,now()+interval '1 hour') RETURNING id`, fixture.workspaceA).Scan(&machine))
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at,native_session_id) VALUES($1,$2,$3,$4,'codex','autonomous','Implement','["codex"]',60,'succeeded',now(),'0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b')`, run, fixture.workspaceA, machine, task)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(84))
	var purpose, state, native string
	var session uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,purpose,state,native_session_id FROM agent_sessions WHERE task_id=$1`, task).Scan(&session, &purpose, &state, &native))
	require.Equal(t, "implement", purpose)
	require.Equal(t, "suspended", state, "a task awaiting review has a resumable session")
	require.Equal(t, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", native)
	var linked uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT session_id FROM dev_machine_agent_runs WHERE id=$1`, run).Scan(&linked))
	require.Equal(t, session, linked)
	var kind string
	require.NoError(t, db.QueryRow(`SELECT kind FROM agent_tasks WHERE id=$1`, task).Scan(&kind))
	require.Equal(t, "implement", kind)

	_, err = db.Exec(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,kind) VALUES($1,$2,$3,$4,$5,$6,'Review','codex','review')`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New())
	require.Error(t, err, "a review always names the task it reviews")
	var review uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,kind,review_of_task_id) VALUES($1,$2,$3,$4,$5,$6,'Review','codex','review',$7) RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New(), task).Scan(&review))
	insertReview := `INSERT INTO agent_reviews(task_id,workspace_id,reviewed_task_id,requested_by,provider,commit_sha,repository_full_name,pull_request_number,pull_request_url,post_to_pull_request,comment_state) VALUES($1,$2,$3,$4,'codex',$5,'acme/app',1,'https://github.com/acme/app/pull/1',$6,$7)`
	_, err = db.Exec(insertReview, review, fixture.workspaceA, task, fixture.userA, "not-a-sha", false, "not_requested")
	require.Error(t, err, "the reviewed commit is a full SHA")
	_, err = db.Exec(insertReview, review, fixture.workspaceA, task, fixture.userA, strings.Repeat("a", 40), false, "pending")
	require.Error(t, err, "a comment is pending only when the requester asked for it")
	_, err = db.Exec(insertReview, review, fixture.workspaceA, task, fixture.userA, strings.Repeat("a", 40), true, "pending")
	require.NoError(t, err)

	require.Error(t, m.Steps(-1), "a retained review task blocks the downgrade")
	require.NoError(t, m.Force(84))
	_, err = db.Exec(`DELETE FROM agent_tasks WHERE id=$1`, review)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_tasks' AND column_name='kind') OR to_regclass('agent_sessions') IS NOT NULL OR to_regclass('agent_reviews') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_tasks`).Scan(&count))
	require.Equal(t, 1, count, "implementation tasks survive the downgrade")
	require.NoError(t, m.Migrate(84), "the migration re-applies cleanly")
}
