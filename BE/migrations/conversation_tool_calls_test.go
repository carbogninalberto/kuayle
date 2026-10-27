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

func TestToolCallMigrationRefusesLossyDowngrade(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_tool_call_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(60))
	require.NoError(t, m.Steps(-1), "an empty table rolls back")
	require.NoError(t, m.Steps(1))
	var conversation, message uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Tools') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,gen_random_uuid(),'user','Create','completed') RETURNING id`, conversation).Scan(&message))
	var reply uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,parent_id,role,status) VALUES($1,gen_random_uuid(),$2,'assistant','completed') RETURNING id`, conversation, message).Scan(&reply))
	_, err = db.Exec(`INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,0,'call','create_issue','{}','running')`, conversation, reply)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,1,'call','Bad Name','{}','running')`, conversation, reply)
	require.Error(t, err, "tool names are constrained")
	_, err = db.Exec(`INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$2,2,'call','list_teams','{}','completed')`, conversation, reply)
	require.Error(t, err, "a finished call records its completion time")
	require.Error(t, m.Steps(-1))
	require.NoError(t, m.Force(60))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM conversation_tool_calls`).Scan(&count))
	require.Equal(t, 1, count)
}
