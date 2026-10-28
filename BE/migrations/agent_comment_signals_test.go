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

func TestCommentSignalMigrationRefusesLossyDowngrade(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_comment_signal_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(80))
	var issue, team, conversation, task, run uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Signals',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex') RETURNING id,run_id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task, &run))
	insert := `INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,action,request_id) VALUES($1,$2,$3,$4,'commented','recorded',gen_random_uuid())`
	_, err = db.Exec(insert, task, run, "ci", "1")
	require.NoError(t, err, "existing signal kinds remain valid")
	_, err = db.Exec(insert, task, run, "chat", "2")
	require.Error(t, err, "unknown signal kinds are refused")
	_, err = db.Exec(insert, task, run, "comment", "3")
	require.NoError(t, err)
	require.Error(t, m.Steps(-1), "a retained comment signal blocks the downgrade")
	require.NoError(t, m.Force(80))
	_, err = db.Exec(`DELETE FROM agent_task_signals WHERE kind='comment'`)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	_, err = db.Exec(insert, task, run, "comment", "4")
	require.Error(t, err, "the downgrade restores the original kinds")
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_task_signals`).Scan(&count))
	require.Equal(t, 1, count)
}
