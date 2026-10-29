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

func TestAgentLiveMessagesMigrationConstrainsBothDirections(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_agent_live_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(93))
	var issue, team, conversation, task uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Live',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','claude-code') RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task))

	require.NoError(t, m.Migrate(95))
	toAgent := `INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,author_id,body,state,delivered_via,run_id,delivered_at) VALUES($1,$2,'to_agent',$3,$4,$5,'Also add tests',$6,$7,$8,$9)`
	_, err = db.Exec(toAgent, fixture.workspaceA, task, 1, "user", fixture.userA, "queued", nil, nil, nil)
	require.NoError(t, err)
	_, err = db.Exec(toAgent, fixture.workspaceA, task, 1, "orchestrator", fixture.userA, "queued", nil, nil, nil)
	require.Error(t, err, "a sequence number names one inbox file")
	_, err = db.Exec(toAgent, fixture.workspaceA, task, nil, "user", fixture.userA, "queued", nil, nil, nil)
	require.Error(t, err, "messages to the agent are sequenced")
	_, err = db.Exec(toAgent, fixture.workspaceA, task, 2, "agent", nil, "queued", nil, nil, nil)
	require.Error(t, err, "the agent never writes to its own inbox")
	_, err = db.Exec(toAgent, fixture.workspaceA, task, 3, "user", fixture.userA, "delivered", "inbox", nil, nil)
	require.Error(t, err, "a delivered message names its attempt")
	_, err = db.Exec(toAgent, fixture.workspaceA, task, 4, "user", fixture.userA, "delivered", "inbox", uuid.New(), "2026-10-29T10:00:00Z")
	require.NoError(t, err)
	_, err = db.Exec(toAgent, fixture.workspaceA, task, 5, "user", fixture.userA, "received", nil, nil, nil)
	require.Error(t, err, "only the agent's messages are received")

	fromAgent := `INSERT INTO agent_live_messages(workspace_id,task_id,direction,sender,kind,body,state,reject_reason,dedupe_key) VALUES($1,$2,'from_agent','agent',$3,'Keep v1?',$4,$5,$6)`
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "question", "received", nil, "a")
	require.NoError(t, err)
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "question", "received", nil, "a")
	require.Error(t, err, "an outbox file is recorded once")
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "message", "received", nil, nil)
	require.Error(t, err, "agent messages are deduplicated")
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "message", "rejected", nil, "b")
	require.Error(t, err, "a refusal has a reason")
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "order", "received", nil, "c")
	require.Error(t, err)
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "message", "queued", nil, "d")
	require.Error(t, err, "the agent's messages are not queued for anyone")

	_, err = db.Exec(`DELETE FROM agent_tasks WHERE id=$1`, task)
	require.NoError(t, err)
	var left int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_live_messages`).Scan(&left))
	require.Zero(t, left, "messages go with their task")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('agent_live_messages') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
}
