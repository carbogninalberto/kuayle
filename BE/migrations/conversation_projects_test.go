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

// Migration 86 lets a user tag their own free chats with a project of the same
// workspace. Issue chats follow their issue's project instead of storing one.
// Deleting a project untags chats; rollback removes only the tags.
func TestConversationProjectsMigrationTagsFreeChatsWithinTheWorkspace(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_chat_projects_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(85))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	var existing uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Existing') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&existing))

	require.NoError(t, m.Migrate(86))
	var project *uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT project_id FROM conversations WHERE id=$1`, existing).Scan(&project))
	require.Nil(t, project, "existing chats start without a project")
	assertForeignKeyViolation(t, db, `UPDATE conversations SET project_id=$2 WHERE id=$1`, existing, fixture.projectB)
	_, err = db.Exec(`UPDATE conversations SET project_id=$2 WHERE id=$1`, existing, fixture.projectA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id,project_id) VALUES($1,$2,'Issue chat',$3,$4,$5)`, fixture.workspaceA, fixture.userA, fixture.issueA, fixture.teamA, fixture.projectA)
	require.Equal(t, "23514", postgresCode(err), "issue chats follow their issue's project: %v", err)

	_, err = db.Exec(`UPDATE issues SET project_id=NULL WHERE project_id=$1`, fixture.projectA)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM projects WHERE id=$1`, fixture.projectA)
	require.NoError(t, err)
	var workspace uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT workspace_id,project_id FROM conversations WHERE id=$1`, existing).Scan(&workspace, &project))
	require.Nil(t, project, "deleting a project untags the chat")
	require.Equal(t, fixture.workspaceA, workspace)

	require.NoError(t, m.Migrate(85))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM conversations`).Scan(&count))
	require.Equal(t, 1, count, "rollback keeps conversations")
}
