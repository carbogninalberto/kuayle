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

// Migration 171 lets an approved merge wait for the checks of the branch
// Kuayle brought up to date (KUA-207): waiting_checks is unresolved and always
// has its next check; workflow steps keep the merge they carry out (KUA-209).
// Rolling back ends a waiting merge as failed with a reason.
func TestMergeWaitsAreUnresolvedAndRollBackAsFailed(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_merge_waits_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(170))

	var conversation, message, reply uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Merge') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,gen_random_uuid(),'user','Merge it','completed') RETURNING id`, conversation).Scan(&message))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,parent_id,role,status) VALUES($1,gen_random_uuid(),$2,'assistant','completed') RETURNING id`, conversation, message).Scan(&reply))
	approval := func() uuid.UUID {
		var call, id uuid.UUID
		require.NoError(t, db.QueryRow(`INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,(SELECT count(*) FROM conversation_tool_calls WHERE message_id=$2),gen_random_uuid()::text,'merge_pull_request','{}','running') RETURNING id`, conversation, reply).Scan(&call))
		require.NoError(t, db.QueryRow(`INSERT INTO conversation_approvals(conversation_id,tool_call_id,action,actor_id,issue_id,payload,status,expires_at) VALUES($1,$2,'merge_pull_request',$3,$4,'{}','approving',now()+interval '1 day') RETURNING id`, conversation, call, fixture.userA, fixture.issueA).Scan(&id))
		return id
	}
	before := approval()
	_, err = db.Exec(`UPDATE conversation_approvals SET status='waiting_checks' WHERE id=$1`, before)
	require.Error(t, err, "before 171 there is no waiting merge")

	require.NoError(t, m.Migrate(171))
	waiting := approval()
	_, err = db.Exec(`UPDATE conversation_approvals SET status='waiting_checks' WHERE id=$1`, waiting)
	require.Error(t, err, "a waiting merge always has its next check")
	_, err = db.Exec(`UPDATE conversation_approvals SET next_check_at=now() WHERE id=$1`, waiting)
	require.Error(t, err, "only a waiting merge has a next check")
	_, err = db.Exec(`UPDATE conversation_approvals SET status='waiting_checks',next_check_at=now(),resolved_at=now() WHERE id=$1`, waiting)
	require.Error(t, err, "a waiting merge is not resolved")
	_, err = db.Exec(`UPDATE conversation_approvals SET status='waiting_checks',next_check_at=now()+interval '1 minute' WHERE id=$1`, waiting)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE conversation_approvals SET status='approved',next_check_at=NULL,resolved_at=now() WHERE id=$1`, before)
	require.NoError(t, err)
	var merge bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_task_workflow_steps' AND column_name='merge')`).Scan(&merge))
	require.True(t, merge)

	require.NoError(t, m.Steps(-1))
	var status string
	var failure sql.NullString
	var resolved sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT status,error,resolved_at FROM conversation_approvals WHERE id=$1`, waiting).Scan(&status, &failure, &resolved))
	require.Equal(t, "failed", status, "a waiting merge ends as failed")
	require.Contains(t, failure.String, "was not merged")
	require.True(t, resolved.Valid)
	require.NoError(t, db.QueryRow(`SELECT status FROM conversation_approvals WHERE id=$1`, before).Scan(&status))
	require.Equal(t, "approved", status, "finished approvals are kept")
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_task_workflow_steps' AND column_name='merge')`).Scan(&merge))
	require.False(t, merge)
}
