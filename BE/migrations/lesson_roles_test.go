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

// Migration 209 lets a lesson be for one role of a team workflow's roster.
// Existing lessons stay for every task; the same subject may be a lesson of
// several roles, once each. Rolling back deletes the lessons for a role,
// which would otherwise reach every task.
func TestLessonRolesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_lesson_roles_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(208))

	dedupe := strings.Repeat("a", 64)
	insert := func(role any) error {
		_, err := db.Exec(`INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,state,text,proposed_text,source_event,proposer,proposed_by,decided_by,decided_at,position,dedupe_key,role)
 VALUES($1,'acme/app','acme/app','approved','Run the checks','Run the checks','manual','person',$2,$2,now(),1,$3,$4)`, fixture.workspaceA, fixture.userA, dedupe, role)
		return err
	}
	var existing uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,state,text,proposed_text,source_event,proposer,proposed_by,decided_by,decided_at,position,dedupe_key)
 VALUES($1,'acme/app','acme/app','approved','Run the checks','Run the checks','manual','person',$2,$2,now(),1,$3) RETURNING id`, fixture.workspaceA, fixture.userA, dedupe).Scan(&existing))

	require.NoError(t, m.Migrate(209))
	var role sql.NullString
	require.NoError(t, db.QueryRow(`SELECT role FROM workspace_lessons WHERE id=$1`, existing).Scan(&role))
	require.False(t, role.Valid, "an existing lesson stays for every task")
	require.Error(t, insert(nil), "the same subject once for every task")
	require.NoError(t, insert("reviewer"), "and once more for a role")
	require.Error(t, insert("reviewer"), "once per role")
	require.NoError(t, insert("security"))
	for _, bad := range []string{"", "Reviewer", "-x", strings.Repeat("r", 33), "a b"} {
		require.Error(t, insert(bad), bad)
	}

	require.NoError(t, m.Steps(-1))
	var left int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM workspace_lessons WHERE workspace_id=$1`, fixture.workspaceA).Scan(&left))
	require.Equal(t, 1, left, "only the lesson for every task is kept")
	_, err = db.Exec(`INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,state,text,proposed_text,source_event,proposer,proposed_by,decided_by,decided_at,position,dedupe_key)
 VALUES($1,'acme/app','acme/app','approved','Run the checks','Run the checks','manual','person',$2,$2,now(),2,$3)`, fixture.workspaceA, fixture.userA, dedupe)
	require.Error(t, err, "the subject is unique again per repository and team")
}
