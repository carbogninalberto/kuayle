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

// Existing AI settings keep vision off; only accepted image types and sizes
// can be recorded; deleted images leave their storage key for cleanup; and
// rolling back removes only the image records and the vision flag.
func TestConversationAttachmentsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_attachments_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(81))
	_, err = db.Exec(`INSERT INTO ai_settings(workspace_id,provider,base_url,model) VALUES($1,'openai_compatible','https://model.example.test','m')`, fixture.workspaceA)
	require.NoError(t, err)
	var conversation, message uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Images') RETURNING id`, fixture.workspaceA, fixture.userA).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status) VALUES($1,gen_random_uuid(),'user','hi','completed') RETURNING id`, conversation).Scan(&message))

	require.NoError(t, m.Migrate(82))
	var vision bool
	require.NoError(t, db.QueryRow(`SELECT assistant_vision FROM ai_settings WHERE workspace_id=$1`, fixture.workspaceA).Scan(&vision))
	require.False(t, vision, "vision stays off until the owner enables it")
	insert := func(key, contentType string, size int64) error {
		_, err := db.Exec(`INSERT INTO conversation_attachments(workspace_id,conversation_id,message_id,uploaded_by,storage_key,filename,content_type,size,width,height) VALUES($1,$2,$3,$4,$5,'a.png',$6,$7,10,10)`, fixture.workspaceA, conversation, message, fixture.userA, key, contentType, size)
		return err
	}
	require.NoError(t, insert("conversation-a.png", "image/png", 100))
	require.Error(t, insert("conversation-b.svg", "image/svg+xml", 100), "SVG is never accepted")
	require.Error(t, insert("conversation-c.png", "image/png", 10<<20+1), "the size limit is enforced")
	_, err = db.Exec(`DELETE FROM conversations WHERE id=$1`, conversation)
	require.NoError(t, err)
	var garbage int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM conversation_attachment_garbage WHERE storage_key='conversation-a.png'`).Scan(&garbage))
	require.Equal(t, 1, garbage, "a cascaded delete keeps the key for storage cleanup")

	require.NoError(t, m.Steps(-1))
	var exists bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='ai_settings' AND column_name='assistant_vision')`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, db.QueryRow(`SELECT to_regclass('conversation_attachments') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	var settings int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM ai_settings WHERE workspace_id=$1`, fixture.workspaceA).Scan(&settings))
	require.Equal(t, 1, settings, "AI settings survive the rollback")
}
