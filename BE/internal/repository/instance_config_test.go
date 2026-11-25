package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// An override is one row per variable: saving again replaces it and records
// who saved it, a reset deletes it, and the table refuses a malformed name or
// an oversized value. Deleting the account keeps the override.
func TestInstanceConfigOverridesAreOneRowPerVariable(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	first, second := uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{first, second} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Admin','x')`, user, user.String()+"@example.test")
		require.NoError(t, err)
	}
	repo := NewInstanceConfigRepository(db)
	ctx := context.Background()

	rows, err := repo.ListOverrides(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)

	saved, err := repo.SetOverride(ctx, "WORKFLOW_PIPELINES_ENABLED", "false", first)
	require.NoError(t, err)
	require.Equal(t, "false", saved.Value)
	require.Equal(t, first, *saved.UpdatedBy)
	require.False(t, saved.UpdatedAt.IsZero())

	again, err := repo.SetOverride(ctx, "WORKFLOW_PIPELINES_ENABLED", "true", second)
	require.NoError(t, err)
	require.Equal(t, second, *again.UpdatedBy)
	require.False(t, again.UpdatedAt.Before(saved.UpdatedAt))
	_, err = repo.SetOverride(ctx, "BACKUP_TIMEOUT", "90m", first)
	require.NoError(t, err)

	rows, err = repo.ListOverrides(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	values := map[string]string{}
	for _, row := range rows {
		values[row.Name] = row.Value
	}
	require.Equal(t, map[string]string{"WORKFLOW_PIPELINES_ENABLED": "true", "BACKUP_TIMEOUT": "90m"}, values)

	_, err = db.Exec(`DELETE FROM users WHERE id=$1`, second)
	require.NoError(t, err)
	rows, err = repo.ListOverrides(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "an override outlives the account that saved it")

	require.NoError(t, repo.DeleteOverride(ctx, "WORKFLOW_PIPELINES_ENABLED"))
	require.NoError(t, repo.DeleteOverride(ctx, "WORKFLOW_PIPELINES_ENABLED"), "resetting twice is not an error")
	rows, err = repo.ListOverrides(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "BACKUP_TIMEOUT", rows[0].Name)

	_, err = repo.SetOverride(ctx, "lower_case", "true", first)
	require.Error(t, err)
	_, err = repo.SetOverride(ctx, "BACKUP_TIMEOUT", strings.Repeat("9", 257), first)
	require.Error(t, err)
}
