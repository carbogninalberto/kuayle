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

// Migration 111 lets a create_release approval exist without an issue, and
// only that action and upgrades; rolling back removes release approvals.
func TestReleaseApprovalsNeedNoIssue(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_release_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(109))

	var conversation, message, reply uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Release') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,gen_random_uuid(),'user','Release','completed') RETURNING id`, conversation).Scan(&message))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,parent_id,role,status) VALUES($1,gen_random_uuid(),$2,'assistant','completed') RETURNING id`, conversation, message).Scan(&reply))
	approve := func(action string, issue any) error {
		var call uuid.UUID
		require.NoError(t, db.QueryRow(`INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,(SELECT count(*) FROM conversation_tool_calls WHERE message_id=$2),gen_random_uuid()::text,$3,'{}','running') RETURNING id`, conversation, reply, action).Scan(&call))
		_, err := db.Exec(`INSERT INTO conversation_approvals(conversation_id,tool_call_id,action,actor_id,issue_id,payload,status,expires_at) VALUES($1,$2,$3,$4,$5,'{}','pending',now()+interval '1 day')`, conversation, call, action, fixture.userA, issue)
		return err
	}
	require.Error(t, approve("create_release", nil), "before 111 there is no release action")

	require.NoError(t, m.Migrate(111))
	require.NoError(t, approve("create_release", nil), "a release belongs to no issue")
	require.NoError(t, approve("upgrade_instance", nil))
	require.Error(t, approve("delegate_issue", nil), "other actions still need an issue")
	require.NoError(t, approve("delegate_issue", fixture.issueA))

	require.NoError(t, m.Steps(-1))
	var releases, others int
	require.NoError(t, db.QueryRow(`SELECT count(*) FILTER (WHERE action='create_release'), count(*) FILTER (WHERE action<>'create_release') FROM conversation_approvals`).Scan(&releases, &others))
	require.Zero(t, releases, "rolling back removes release approvals")
	require.Equal(t, 2, others)
}
