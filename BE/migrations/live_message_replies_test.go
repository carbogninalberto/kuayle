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

// Migration 151: an agent's message may name the message it answers, and a
// message to the agent counts its deliveries. Messages delivered before it
// owe no reply.
func TestLiveMessageRepliesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_live_replies_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(145))
	var issue, team, conversation, task uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Live',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','claude-code') RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task))
	_, err = db.Exec(`INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,author_id,body,state,delivered_via,run_id,delivered_at) VALUES($1,$2,'to_agent',1,'user',$3,'Before',$4,'inbox',$5,now())`, fixture.workspaceA, task, fixture.userA, "delivered", uuid.New())
	require.NoError(t, err)

	// Main's migration 149 sits between the pause (145) and this one.
	require.NoError(t, m.Migrate(149))
	require.NoError(t, m.Migrate(151))
	var deliveries int
	require.NoError(t, db.QueryRow(`SELECT deliveries FROM agent_live_messages WHERE task_id=$1 AND seq=1`, task).Scan(&deliveries))
	require.Zero(t, deliveries, "a message delivered before the rule owes no reply")
	fromAgent := `INSERT INTO agent_live_messages(workspace_id,task_id,direction,sender,body,state,dedupe_key,reply_to) VALUES($1,$2,'from_agent','agent','Understood','received',$3,$4)`
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "a", 1)
	require.NoError(t, err)
	_, err = db.Exec(fromAgent, fixture.workspaceA, task, "b", 0)
	require.Error(t, err, "a reply names a sequence number")
	_, err = db.Exec(`UPDATE agent_live_messages SET reply_to=1 WHERE task_id=$1 AND seq=1`, task)
	require.Error(t, err, "only the agent replies")
	_, err = db.Exec(`UPDATE agent_tasks SET live_resume_generation=generation+1 WHERE id=$1`, task)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	version, dirty, err := m.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.EqualValues(t, 149, version, "one step down undoes this migration only")
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE (table_name='agent_live_messages' AND column_name IN ('reply_to','deliveries','workflow_node')) OR (table_name='agent_tasks' AND column_name='live_resume_generation')`).Scan(&columns))
	require.Zero(t, columns)
	var kept int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_live_messages WHERE task_id=$1`, task).Scan(&kept))
	require.Equal(t, 2, kept, "the messages themselves are kept")
}
