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

// Migration 158 adds the command approvals to the approval action list and
// keeps every action an earlier migration allowed; rolling back removes only
// the new ones.
func TestAssistantCommandRunsMigrationKeepsApprovalActions(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_command_runs_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	actions := func() string {
		t.Helper()
		var definition string
		require.NoError(t, db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='conversation_approvals_action_check'`).Scan(&definition))
		return definition
	}

	require.NoError(t, m.Migrate(157))
	before := actions()
	require.Contains(t, before, "'message_task'")

	require.NoError(t, m.Migrate(158))
	after := actions()
	for _, action := range []string{"message_task", "share_wiki_page", "expose_port", "start_temporary_machine", "run_command", "run_command_write"} {
		require.Contains(t, after, "'"+action+"'")
	}

	require.NoError(t, m.Migrate(157))
	require.Equal(t, before, actions(), "rolling back restores the earlier list")
}
