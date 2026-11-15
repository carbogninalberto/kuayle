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

// Migration 160 turns issues into a view over issue_records that hides
// soft-deleted rows. Existing rows survive, writes through the view keep
// working with their triggers, every update advances the version, and a
// rollback purges only the soft-deleted rows.
func TestIssueSoftDeleteMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_issue_soft_delete_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(159))
	var commentID uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO comments(issue_id,user_id,body) VALUES($1,$2,'kept') RETURNING id`,
		fixture.issueA, fixture.userA).Scan(&commentID))

	require.NoError(t, m.Migrate(160))

	var title string
	var version int64
	require.NoError(t, db.QueryRow(`SELECT title, version FROM issues WHERE id=$1`, fixture.issueA).Scan(&title, &version))
	require.Equal(t, "miga", title, "existing rows survive")
	require.EqualValues(t, 1, version)

	// Inserts through the view use the base-table defaults and return them.
	var statusID uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT status_id FROM issues WHERE id=$1`, fixture.issueA).Scan(&statusID))
	assetID := uuid.New()
	inserted := uuid.New()
	rows, err := db.Query(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,creator_id,status_id)
		VALUES($1,$2,$3,2,'MIGA-2','Second',$4,$5,$6) RETURNING *`,
		inserted, fixture.workspaceA, fixture.teamA, "see /assets/"+assetID.String(), fixture.userA, statusID)
	require.NoError(t, err)
	columns, err := rows.Columns()
	require.NoError(t, err)
	require.Contains(t, columns, "version")
	require.NotContains(t, columns, "deleted_at", "the view hides the soft-delete columns")
	require.True(t, rows.Next())
	require.NoError(t, rows.Close())
	var events int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issue_lifecycle_events WHERE issue_id=$1 AND event_type='created'`, inserted).Scan(&events))
	require.Equal(t, 1, events, "the lifecycle trigger fires for inserts through the view")
	var ownerTable string
	require.NoError(t, db.QueryRow(`SELECT owner_table FROM private_asset_reference_index WHERE source_id=$1`, inserted).Scan(&ownerTable))
	require.Equal(t, "issues", ownerTable, "asset references keep the logical table name")

	// Updates through the view bump the version and record content history.
	require.NoError(t, db.QueryRow(`UPDATE issues SET title='Renamed' WHERE id=$1 RETURNING version`, inserted).Scan(&version))
	require.EqualValues(t, 2, version)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='title'`, inserted).Scan(&events))
	require.Equal(t, 1, events)

	// Private-team guards still recognise issue rows by their logical name.
	_, err = db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, fixture.teamB)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET team_id=$1 WHERE id=$2`, fixture.teamB, inserted)
	require.ErrorContains(t, err, "private-team boundary")

	// A soft-deleted row disappears from the view but keeps its dependants.
	_, err = db.Exec(`UPDATE issue_records SET deleted_at=NOW() WHERE id=$1`, fixture.issueA)
	require.NoError(t, err)
	var visible int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issues WHERE id=$1`, fixture.issueA).Scan(&visible))
	require.Zero(t, visible)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM comments WHERE id=$1`, commentID).Scan(&visible))
	require.Equal(t, 1, visible)

	// Rolling back purges soft-deleted rows only.
	require.NoError(t, m.Migrate(159))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issues WHERE id IN ($1,$2)`, fixture.issueA, inserted).Scan(&visible))
	require.Equal(t, 1, visible)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM comments WHERE id=$1`, commentID).Scan(&visible))
	require.Zero(t, visible, "dependants of a purged issue cascade")
	var relkind string
	require.NoError(t, db.QueryRow(`SELECT relkind::text FROM pg_class WHERE relname='issues'`).Scan(&relkind))
	require.Equal(t, "r", relkind)
	_, err = db.Exec(`UPDATE issues SET team_id=$1 WHERE id=$2`, fixture.teamB, inserted)
	require.ErrorContains(t, err, "private-team boundary", "rollback restores the guards")

	require.NoError(t, m.Migrate(160), "the migration re-applies cleanly")
	require.NoError(t, db.QueryRow(`SELECT version FROM issues WHERE id=$1`, inserted).Scan(&version))
	require.EqualValues(t, 1, version)
}
