package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
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

// An apply and restart is recorded with its changes and the overrides it
// replaced; only one run is active at a time; a boot is counted; a rollback
// restores exactly the changed variables, atomically, and leaves other
// overrides alone.
func TestInstanceRestartRunLifecycle(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	admin := uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Admin','x')`, admin, admin.String()+"@example.test")
	require.NoError(t, err)
	repo := NewInstanceConfigRepository(db)
	ctx := context.Background()
	_, err = repo.SetOverride(ctx, "SMTP_PORT", "1025", admin)
	require.NoError(t, err)
	_, err = repo.SetOverride(ctx, "BACKUP_TIMEOUT", "3h", admin) // a runtime override, not part of the run

	none, err := repo.ActiveRestartRun(ctx)
	require.NoError(t, err)
	require.Nil(t, none)
	latest, err := repo.LatestRestartRun(ctx)
	require.NoError(t, err)
	require.Nil(t, latest)

	previousPort, previousNone := "1025", (*string)(nil)
	run, err := repo.BeginRestartRun(ctx, domain.InstanceRestartRun{
		RequestedBy: &admin,
		Changes: []domain.InstanceRestartChange{
			{Name: "SMTP_PORT", From: "1025", To: "2525"}, {Name: "SMTP_HOST", From: "", To: "mail.example.com"}, {Name: "S3_PUBLIC", From: "true", To: "true"},
		},
		Previous: map[string]*string{"SMTP_PORT": &previousPort, "SMTP_HOST": previousNone, "S3_PUBLIC": previousNone},
	}, map[string]string{"SMTP_PORT": "2525", "SMTP_HOST": "mail.example.com"}, []string{"S3_PUBLIC"})
	require.NoError(t, err)
	require.Equal(t, domain.RestartRunRestarting, run.Status)
	require.Equal(t, admin, *run.RequestedBy)
	require.Len(t, run.Changes, 3)
	require.Equal(t, "1025", *run.Previous["SMTP_PORT"])
	require.Nil(t, run.Previous["SMTP_HOST"])

	stored := overrideValues(t, repo)
	require.Equal(t, map[string]string{"SMTP_PORT": "2525", "SMTP_HOST": "mail.example.com", "BACKUP_TIMEOUT": "3h"}, stored)

	_, err = repo.BeginRestartRun(ctx, domain.InstanceRestartRun{Changes: []domain.InstanceRestartChange{}, Previous: map[string]*string{}},
		map[string]string{"SMTP_PORT": "9"}, nil)
	require.ErrorIs(t, err, domain.ErrRestartRunActive, "one run at a time")
	require.Equal(t, "2525", overrideValues(t, repo)["SMTP_PORT"], "the refused run changed nothing")

	active, err := repo.ActiveRestartRun(ctx)
	require.NoError(t, err)
	require.Equal(t, run.ID, active.ID)

	confirmedBeforeBoot, err := repo.ConfirmRestartRun(ctx, run.ID)
	require.NoError(t, err)
	require.False(t, confirmedBeforeBoot, "a run that has not started with its values cannot be confirmed")

	first, err := repo.MarkRestartBoot(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RestartRunApplying, first.Status)
	require.Equal(t, 1, first.BootAttempts)
	require.NotNil(t, first.AppliedAt)
	second, err := repo.MarkRestartBoot(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, 2, second.BootAttempts)
	require.Equal(t, first.AppliedAt.UnixNano(), second.AppliedAt.UnixNano(), "the first start is when it was applied")

	rolled, err := repo.RollbackRestartRun(ctx, run.ID, "did not start")
	require.NoError(t, err)
	require.True(t, rolled)
	require.Equal(t, map[string]string{"SMTP_PORT": "1025", "BACKUP_TIMEOUT": "3h"}, overrideValues(t, repo),
		"the previous overrides are back and unrelated ones are untouched")
	latest, err = repo.LatestRestartRun(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.RestartRunRolledBack, latest.Status)
	require.Equal(t, "did not start", latest.Failure)
	require.NotNil(t, latest.FinishedAt)
	again, err := repo.RollbackRestartRun(ctx, run.ID, "twice")
	require.NoError(t, err)
	require.False(t, again, "a decided run is not rolled back again")
	gone, err := repo.MarkRestartBoot(ctx, run.ID)
	require.NoError(t, err)
	require.Nil(t, gone)

	// A new run can confirm.
	next, err := repo.BeginRestartRun(ctx, domain.InstanceRestartRun{RequestedBy: &admin, Changes: []domain.InstanceRestartChange{{Name: "SMTP_PORT", From: "1025", To: "587"}},
		Previous: map[string]*string{"SMTP_PORT": &previousPort}}, map[string]string{"SMTP_PORT": "587"}, nil)
	require.NoError(t, err)
	_, err = repo.MarkRestartBoot(ctx, next.ID)
	require.NoError(t, err)
	confirmed, err := repo.ConfirmRestartRun(ctx, next.ID)
	require.NoError(t, err)
	require.True(t, confirmed)
	rolled, err = repo.RollbackRestartRun(ctx, next.ID, "late")
	require.NoError(t, err)
	require.False(t, rolled, "a confirmed run keeps its values")
	require.Equal(t, "587", overrideValues(t, repo)["SMTP_PORT"])

	// Deleting the account keeps the history.
	_, err = db.Exec(`DELETE FROM users WHERE id = $1`, admin)
	require.NoError(t, err)
	latest, err = repo.LatestRestartRun(ctx)
	require.NoError(t, err)
	require.Nil(t, latest.RequestedBy)
	require.Len(t, latest.Changes, 1)
}

func overrideValues(t *testing.T, repo *InstanceConfigRepository) map[string]string {
	t.Helper()
	rows, err := repo.ListOverrides(context.Background())
	require.NoError(t, err)
	values := map[string]string{}
	for _, row := range rows {
		values[row.Name] = row.Value
	}
	return values
}
