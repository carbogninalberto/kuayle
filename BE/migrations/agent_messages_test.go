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

func TestAgentMessagesMigrationKeepsCommentsAndConstrainsTheRegistry(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_agent_messages_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(85))
	var issue, team, conversation, task uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	comment := uuid.New()
	_, err = db.Exec(`INSERT INTO comments(id,issue_id,user_id,body) VALUES($1,$2,$3,'<p>Existing</p>')`, comment, issue, fixture.userA)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Messages',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex') RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task))

	require.NoError(t, m.Migrate(88))
	var agentTask *uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT agent_task_id FROM comments WHERE id=$1`, comment).Scan(&agentTask))
	require.Nil(t, agentTask, "existing comments stay people's comments")

	insert := `INSERT INTO agent_messages(workspace_id,from_task_id,from_issue_id,to_ref,to_issue_id,channel,body,state,reject_reason,dedupe_key,comment_state) VALUES($1,$2,$3,'MIGA-1',$4,$5,'hello',$6,$7,$8,$9)`
	_, err = db.Exec(insert, fixture.workspaceA, task, issue, issue, "internal", "queued", nil, "a", "none")
	require.NoError(t, err)
	_, err = db.Exec(insert, fixture.workspaceA, task, issue, issue, "internal", "queued", nil, "a", "none")
	require.Error(t, err, "an outbox file is recorded once per task")
	_, err = db.Exec(insert, fixture.workspaceA, task, issue, nil, "internal", "queued", nil, "b", "none")
	require.Error(t, err, "only refused messages lack a resolved recipient")
	_, err = db.Exec(insert, fixture.workspaceA, task, issue, nil, "internal", "rejected", "unknown recipient", "c", "none")
	require.NoError(t, err)
	_, err = db.Exec(insert, fixture.workspaceA, task, issue, issue, "internal", "queued", nil, "d", "pending")
	require.Error(t, err, "internal messages never post comments")
	_, err = db.Exec(insert, fixture.workspaceA, task, issue, issue, "comment", "delivered", nil, "e", "pending")
	require.Error(t, err, "a delivered message names the attempt that received it")
	_, err = db.Exec(`INSERT INTO agent_message_holds(workspace_id,issue_a,issue_b) VALUES($1,$2,$2)`, fixture.workspaceA, issue)
	require.Error(t, err, "a held conversation names two issues in order")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('agent_messages') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	var body string
	require.NoError(t, db.QueryRow(`SELECT body FROM comments WHERE id=$1`, comment).Scan(&body))
	require.Equal(t, "<p>Existing</p>", body)
}
