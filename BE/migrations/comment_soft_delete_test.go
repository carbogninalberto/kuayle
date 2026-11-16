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

// Migration 168 adds soft-delete and edit columns to comments. Existing
// comments survive as not deleted, soft-deleting one passes the comment
// triggers, and a rollback purges only the deleted comments.
func TestCommentSoftDeleteMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_comment_soft_delete_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	if postgresCode(err) == "42501" {
		_ = admin.Close()
		t.Skip("DATABASE_URL user cannot create an isolated migration database")
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		_ = admin.Close()
	})
	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(167))
	kept, gone := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{kept, gone} {
		_, err = db.Exec(`INSERT INTO comments(id,issue_id,user_id,body) VALUES($1,$2,$3,'Hello')`, id, fixture.issueA, fixture.userA)
		require.NoError(t, err)
	}

	require.NoError(t, m.Migrate(168))

	var deleted, edited sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT deleted_at, edited_at FROM comments WHERE id=$1`, kept).Scan(&deleted, &edited))
	require.False(t, deleted.Valid, "existing comments are not deleted")
	require.False(t, edited.Valid, "existing comments are not edited")
	_, err = db.Exec(`UPDATE comments SET deleted_at=NOW(), deleted_by=$2 WHERE id=$1`, gone, fixture.userA)
	require.NoError(t, err, "soft-deleting passes the comment triggers")
	_, err = db.Exec(`UPDATE comments SET body='Edited', edited_at=NOW() WHERE id=$1`, kept)
	require.NoError(t, err, "editing passes the comment triggers")

	require.NoError(t, m.Migrate(167))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM comments WHERE id IN ($1,$2)`, kept, gone).Scan(&count))
	require.Equal(t, 1, count, "rolling back purges deleted comments only")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name='comments' AND column_name IN ('deleted_at','deleted_by','edited_at')`).Scan(&count))
	require.Zero(t, count, "rolling back drops the columns")
	require.NoError(t, m.Migrate(168), "the migration re-applies cleanly")
}
