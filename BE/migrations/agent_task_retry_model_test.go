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

// Existing retry receipts asked for no model; new receipts record the model and
// server asked for, and rolling back removes only those columns.
func TestAgentTaskRetryModelMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_retry_model_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(108))
	var issue, team, conversation, task uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Retry',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,generation) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex',2) RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task))
	existing := uuid.New()
	_, err = db.Exec(`INSERT INTO agent_task_retries(task_id,request_id,expected_generation,prompt) VALUES($1,$2,1,'Retry')`, task, existing)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(109))
	var model string
	var server *uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT model,model_server_id FROM agent_task_retries WHERE request_id=$1`, existing).Scan(&model, &server))
	require.Empty(t, model, "an existing receipt asked for no model")
	require.Nil(t, server)
	chosen := uuid.New()
	_, err = db.Exec(`INSERT INTO agent_task_retries(task_id,request_id,expected_generation,prompt,model,model_server_id) VALUES($1,$2,2,'Retry','gpt-5',$3)`, task, uuid.New(), chosen)
	require.NoError(t, err, "the server asked for is kept even when it no longer exists")
	_, err = db.Exec(`INSERT INTO agent_task_retries(task_id,request_id,expected_generation,prompt,model) VALUES($1,$2,2,'Retry',$3)`, task, uuid.New(), strings.Repeat("m", 201))
	require.Error(t, err, "model names are bounded")

	require.NoError(t, m.Steps(-1))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_task_retries WHERE task_id=$1`, task).Scan(&count))
	require.Equal(t, 2, count, "rolling back keeps the receipts")
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='agent_task_retries' AND column_name IN ('model','model_server_id')`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, m.Migrate(109), "the migration re-applies cleanly")
}
