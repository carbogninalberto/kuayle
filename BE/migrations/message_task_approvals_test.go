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

// Migration 134 adds the message_task approval and the request ID that makes
// an approved message idempotent; rolling back removes those approvals and
// keeps the messages already delivered.
func TestMessageTaskApprovalsDeliverOncePerRequest(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_message_task_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	var issue, team, chat, task uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Live',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&chat))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','claude-code') RETURNING id`, fixture.workspaceA, chat, fixture.userA, issue, team, uuid.New()).Scan(&task))
	require.NoError(t, m.Migrate(133))

	var conversation, message, reply uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Message') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,gen_random_uuid(),'user','Tell it','completed') RETURNING id`, conversation).Scan(&message))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,parent_id,role,status) VALUES($1,gen_random_uuid(),$2,'assistant','completed') RETURNING id`, conversation, message).Scan(&reply))
	approve := func(action string) error {
		var call uuid.UUID
		require.NoError(t, db.QueryRow(`INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,(SELECT count(*) FROM conversation_tool_calls WHERE message_id=$2),gen_random_uuid()::text,$3,'{}','running') RETURNING id`, conversation, reply, action).Scan(&call))
		_, err := db.Exec(`INSERT INTO conversation_approvals(conversation_id,tool_call_id,action,actor_id,issue_id,payload,status,expires_at) VALUES($1,$2,$3,$4,$5,'{}','pending',now()+interval '1 day')`, conversation, call, action, fixture.userA, issue)
		return err
	}
	require.Error(t, approve("message_task"), "before 134 a message cannot ask for approval")
	_, err = db.Exec(`INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,author_id,body,state) VALUES($1,$2,'to_agent',1,'user',$3,'Typed by the owner','queued')`, fixture.workspaceA, task, fixture.userA)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(134))
	require.NoError(t, approve("message_task"))
	require.NoError(t, approve("follow_up_task"))
	send := `INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,author_id,body,state,request_id) VALUES($1,$2,'to_agent',$3,$4,$5,'Also add tests','queued',$6)`
	request := uuid.New()
	_, err = db.Exec(send, fixture.workspaceA, task, 2, "orchestrator", fixture.userA, request)
	require.NoError(t, err)
	_, err = db.Exec(send, fixture.workspaceA, task, 3, "orchestrator", fixture.userA, request)
	require.Error(t, err, "one approval delivers one message")
	_, err = db.Exec(send, fixture.workspaceA, task, 3, "user", fixture.userA, uuid.New())
	require.Error(t, err, "only the assistant's approved messages carry a request")
	_, err = db.Exec(send, fixture.workspaceA, task, 3, "user", fixture.userA, nil)
	require.NoError(t, err, "the owner's own messages need no request")
	_, err = db.Exec(send, fixture.workspaceA, task, 4, "user", fixture.userA, nil)
	require.NoError(t, err)

	require.NoError(t, m.Steps(-1))
	var messages, requested, others int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_live_messages`).Scan(&messages))
	require.Equal(t, 4, messages, "delivered messages are kept")
	require.NoError(t, db.QueryRow(`SELECT count(*) FILTER (WHERE action='message_task'), count(*) FILTER (WHERE action<>'message_task') FROM conversation_approvals`).Scan(&requested, &others))
	require.Zero(t, requested, "rolling back removes message approvals")
	require.Equal(t, 1, others)
	require.NoError(t, m.Migrate(134), "the migration applies again after a rollback")
}
