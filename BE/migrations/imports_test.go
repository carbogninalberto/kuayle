package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

func TestImportsMigrationFilesExist(t *testing.T) {
	for _, name := range []string{"000174_imports.up.sql", "000174_imports.down.sql"} {
		_, err := os.Stat(name)
		require.NoError(t, err, "migration file %s must exist", name)
	}
	up, err := os.ReadFile("000174_imports.up.sql")
	require.NoError(t, err)
	require.Contains(t, string(up), "UNIQUE (workspace_id, source, source_site, external_type, external_id)")
}

func TestImportsMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	defer admin.Close()

	name := "kuayle_imports_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.ExecContext(context.Background(), `CREATE DATABASE "`+name+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
	})

	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	defer m.Close()

	require.NoError(t, m.Migrate(173))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, m.Migrate(174))
	for _, table := range []string{"import_jobs", "import_job_items", "import_external_refs"} {
		var exists bool
		err = db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, table).Scan(&exists)
		require.NoError(t, err)
		require.True(t, exists, "table %s should exist after migration", table)
	}

	require.NoError(t, m.Steps(-1))
	for _, table := range []string{"import_jobs", "import_job_items", "import_external_refs"} {
		var exists bool
		err = db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, table).Scan(&exists)
		require.NoError(t, err)
		require.False(t, exists, "table %s should be gone after rollback", table)
	}
}
