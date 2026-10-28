package migrations_test

import (
	"context"
	"database/sql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestConversationsUpgradeAndRollbackPreserveExistingIssues(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_chat_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(52))
	var issueCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issues`).Scan(&issueCount))
	require.NoError(t, m.Steps(1))
	var conversation uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Migration test') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&conversation))
	_, err = db.Exec(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,$2,'user','Existing user message','completed')`, conversation, uuid.New())
	require.NoError(t, err)
	// Telegram upgrade/rollback keeps existing conversations and issue history.
	require.NoError(t, m.Steps(1))
	_, err = db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret) VALUES($1,42,'encrypted-fixture','header-fixture')`, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id,conversation_id) VALUES($1,$2,12345,$3)`, fixture.workspaceA, fixture.userA, conversation)
	require.NoError(t, err)
	require.NoError(t, m.Steps(1)) // issue conversation scope
	var issueID, teamID uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issueID, &teamID))
	_, err = db.Exec(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Scoped history',$3,$4)`, fixture.workspaceA, fixture.userA, issueID, teamID)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	var remaining int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM conversations`).Scan(&remaining))
	require.Equal(t, 1, remaining, "rollback removes scoped history rather than making it public")
	require.NoError(t, m.Steps(-1))
	var chatCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM conversation_messages`).Scan(&chatCount))
	require.Equal(t, 1, chatCount)
	require.NoError(t, m.Steps(1))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telegram_settings`).Scan(&chatCount))
	require.Zero(t, chatCount)
	require.NoError(t, m.Steps(-1))
	require.NoError(t, m.Steps(-1))
	var after int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issues`).Scan(&after))
	require.Equal(t, issueCount, after)
	require.NoError(t, m.Steps(1))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM conversation_messages`).Scan(&after))
	require.Zero(t, after, "rollback intentionally deletes conversation data")
}
