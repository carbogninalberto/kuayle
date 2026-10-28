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

func TestAgentTaskMigrationRefusesOrphaningActiveExecution(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_task_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(55))
	var issue, team, conversation uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Task migration',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, m.Steps(1))
	_, err = db.Exec(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement issue','codex')`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New())
	require.NoError(t, err)
	require.Error(t, m.Steps(-1), "downgrade cannot forget active runtime ownership")
	require.NoError(t, m.Force(56))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_tasks`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = db.Exec(`UPDATE agent_tasks SET state='cancelled'`)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM conversations WHERE id=$1`, conversation).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, m.Steps(1))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_tasks`).Scan(&count))
	require.Zero(t, count)
}
