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

// Migration 166 turns wiki_pages into a view over wiki_page_records that
// hides soft-deleted pages. Pages survive, registry appends keep their
// ON CONFLICT target, purging a page lifts its children, and a rollback
// purges only the deleted pages.
func TestWikiPageSoftDeleteMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_wiki_soft_delete_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(165))
	parent, child, gone := uuid.New(), uuid.New(), uuid.New()
	for _, page := range []struct {
		id     uuid.UUID
		parent *uuid.UUID
	}{{parent, nil}, {child, &parent}, {gone, nil}} {
		_, err = db.Exec(`INSERT INTO wiki_pages(id,workspace_id,team_id,title,parent_id) VALUES($1,$2,$3,'Page',$4)`,
			page.id, fixture.workspaceA, fixture.teamA, page.parent)
		require.NoError(t, err)
	}

	require.NoError(t, m.Migrate(166))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM wiki_pages WHERE workspace_id=$1`, fixture.workspaceA).Scan(&count))
	require.Equal(t, 3, count, "existing pages survive")
	// The registry append's ON CONFLICT target still resolves through the view.
	day := `INSERT INTO wiki_pages(workspace_id,team_id,title,registry_date) VALUES($1,$2,'Day','2026-01-01')
		ON CONFLICT (team_id, registry_date) WHERE team_id IS NOT NULL AND registry_date IS NOT NULL DO NOTHING`
	_, err = db.Exec(day, fixture.workspaceA, fixture.teamA)
	require.NoError(t, err)
	result, err := db.Exec(day, fixture.workspaceA, fixture.teamA)
	require.NoError(t, err)
	affected, _ := result.RowsAffected()
	require.Zero(t, affected)

	_, err = db.Exec(`UPDATE wiki_page_records SET deleted_at=NOW() WHERE id=$1`, parent)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM wiki_pages WHERE id=$1`, parent).Scan(&count))
	require.Zero(t, count, "a deleted page is hidden")
	_, err = db.Exec(`DELETE FROM wiki_page_records WHERE id=$1`, parent)
	require.NoError(t, err)
	var childParent sql.NullString
	require.NoError(t, db.QueryRow(`SELECT parent_id FROM wiki_pages WHERE id=$1`, child).Scan(&childParent))
	require.False(t, childParent.Valid, "purging lifts the children")

	_, err = db.Exec(`UPDATE wiki_page_records SET deleted_at=NOW() WHERE id=$1`, gone)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(165))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM wiki_pages WHERE id IN ($1,$2)`, child, gone).Scan(&count))
	require.Equal(t, 1, count, "rolling back purges deleted pages only")
	require.NoError(t, m.Migrate(166), "the migration re-applies cleanly")
}
