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
	require.NoError(t, m.Steps(-1))
	var after int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issues`).Scan(&after))
	require.Equal(t, issueCount, after)
	require.NoError(t, m.Steps(1))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM conversation_messages`).Scan(&after))
	require.Zero(t, after, "rollback intentionally deletes conversation data")
}
