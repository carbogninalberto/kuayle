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

// Migration 115 scopes labels to a project and makes a label name unique,
// ignoring case, within its scope: the workspace-wide labels, or one project's.
// Active duplicates left by earlier racing creates are renamed, not merged.
func TestLabelProjectScopeMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_label_project_scope_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(113))

	label := func(workspace uuid.UUID, name string, created string, deleted bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		var deletedAt any
		if deleted {
			deletedAt = created
		}
		_, err := db.Exec(`INSERT INTO labels(id,workspace_id,name,color,created_at,deleted_at) VALUES($1,$2,$3,'#112233',$4,$5)`, id, workspace, name, created, deletedAt)
		require.NoError(t, err)
		return id
	}
	keep := label(fixture.workspaceA, "Bug", "2024-01-01T00:00:00Z", false)
	dupe := label(fixture.workspaceA, "bug", "2024-01-02T00:00:00Z", false)
	long := strings.Repeat("x", 50)
	keepLong := label(fixture.workspaceA, long, "2024-01-01T00:00:00Z", false)
	dupeLong := label(fixture.workspaceA, strings.ToUpper(long), "2024-01-03T00:00:00Z", false)
	deleted := label(fixture.workspaceA, "BUG", "2024-01-03T00:00:00Z", true)
	otherWorkspace := label(fixture.workspaceB, "Bug", "2024-01-04T00:00:00Z", false)
	_, err = db.Exec(`INSERT INTO issue_labels(issue_id,label_id) VALUES($1,$2)`, fixture.issueA, dupe)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(115))
	nameOf := func(id uuid.UUID) string {
		t.Helper()
		var n string
		require.NoError(t, db.QueryRow(`SELECT name FROM labels WHERE id=$1`, id).Scan(&n))
		return n
	}
	require.Equal(t, "Bug", nameOf(keep), "the oldest label keeps its name")
	require.Equal(t, "bug ("+dupe.String()[:8]+")", nameOf(dupe))
	require.Equal(t, long, nameOf(keepLong))
	require.Equal(t, strings.ToUpper(long)[:39]+" ("+dupeLong.String()[:8]+")", nameOf(dupeLong), "the renamed label still fits the column")
	require.Equal(t, "BUG", nameOf(deleted), "deleted labels are left alone")
	require.Equal(t, "Bug", nameOf(otherWorkspace), "other workspaces are a separate scope")
	var attached int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM issue_labels WHERE label_id=$1`, dupe).Scan(&attached))
	require.Equal(t, 1, attached, "a renamed label stays on its issues")

	insert := func(workspace uuid.UUID, project *uuid.UUID, name string) error {
		_, err := db.Exec(`INSERT INTO labels(workspace_id,project_id,name,color) VALUES($1,$2,$3,'#112233')`, workspace, project, name)
		return err
	}
	assertUniqueViolation(t, db, `INSERT INTO labels(workspace_id,name,color) VALUES($1,'BUG','#112233')`, fixture.workspaceA)
	require.NoError(t, insert(fixture.workspaceA, &fixture.projectA, "bug"), "a project may reuse a workspace label's name")
	assertUniqueViolation(t, db, `INSERT INTO labels(workspace_id,project_id,name,color) VALUES($1,$2,'Bug','#112233')`, fixture.workspaceA, fixture.projectA)
	_, err = db.Exec(`UPDATE labels SET deleted_at=NOW() WHERE workspace_id=$1 AND project_id=$2`, fixture.workspaceA, fixture.projectA)
	require.NoError(t, err)
	require.NoError(t, insert(fixture.workspaceA, &fixture.projectA, "Bug"), "a deleted label frees its name")

	require.NoError(t, m.Migrate(113))
	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='labels' AND column_name='project_id'`).Scan(&columns))
	require.Zero(t, columns)
	_, err = db.Exec(`INSERT INTO labels(workspace_id,name,color) VALUES($1,'BUG','#112233')`, fixture.workspaceA)
	require.NoError(t, err, "rolling back drops the uniqueness rule")
	require.NoError(t, m.Migrate(115), "the migration can be applied again")
}
