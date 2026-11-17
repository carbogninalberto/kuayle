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

// Migration 170 (KUA-206) adds the commit and the pending details read to
// existing CI signals without touching what they recorded, and its rollback
// keeps the stored summaries.
func TestCIFailureDetailsMigrationKeepsExistingSignals(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_ci_failure_details_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(169))
	var issue, team, conversation, task, run uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Signals',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex') RETURNING id,run_id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task, &run))
	_, err = db.Exec(`INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,name,action,failure_class,failure_evidence,request_id) VALUES($1,$2,'ci','77','failure','GitHub Actions','follow_up','test_failure','nothing received',gen_random_uuid())`, task, run)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(170))
	var headSHA, class string
	var pending bool
	require.NoError(t, db.QueryRow(`SELECT head_sha,failure_details_pending,failure_class FROM agent_task_signals WHERE task_id=$1`, task).Scan(&headSHA, &pending, &class))
	require.Empty(t, headSHA, "an older signal does not know its commit")
	require.False(t, pending, "an older signal waits for nothing")
	require.Equal(t, "test_failure", class)
	_, err = db.Exec(`UPDATE agent_task_signals SET head_sha=$2,failure_details_pending=true,summary='"E2E" concluded failure' WHERE task_id=$1`, task, strings.Repeat("a", 40))
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE agent_task_signals SET head_sha=$2 WHERE task_id=$1`, task, strings.Repeat("a", 65))
	require.Error(t, err, "a commit is bounded")

	require.NoError(t, m.Steps(-1))
	var summary string
	require.NoError(t, db.QueryRow(`SELECT summary FROM agent_task_signals WHERE task_id=$1`, task).Scan(&summary))
	require.Equal(t, `"E2E" concluded failure`, summary, "the rollback keeps stored summaries")
	require.NoError(t, m.Migrate(170))
}
