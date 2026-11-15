package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// backupTestRepository connects to DATABASE_URL and resets the singleton
// backup state; these tests share that row, so they do not run in parallel.
func backupTestRepository(t *testing.T) (*BackupRepository, *sqlx.DB) {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	reset := func() {
		_, err := db.Exec(`UPDATE backup_settings SET lease_token = NULL, lease_expires_at = NULL,
			enabled = TRUE, next_run_at = NULL WHERE id = 1`)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM backup_runs`)
		require.NoError(t, err)
	}
	reset()
	t.Cleanup(func() {
		reset()
		_, _ = db.Exec(`UPDATE backup_settings SET enabled = FALSE WHERE id = 1`)
		_ = db.Close()
	})
	return NewBackupRepository(db), db
}

func TestBackupLeaseHasOneWinnerUnderConcurrency(t *testing.T) {
	repo, _ := backupTestRepository(t)
	ctx := context.Background()

	const contenders = 8
	var wins sync.Map
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := uuid.New()
			<-start
			won, err := repo.AcquireLease(ctx, token, time.Minute)
			require.NoError(t, err)
			if won {
				wins.Store(token, true)
			}
		}()
	}
	close(start)
	wg.Wait()
	count := 0
	wins.Range(func(any, any) bool { count++; return true })
	require.Equal(t, 1, count, "exactly one replica holds the lease")
}

func TestBackupLeaseExpiresAndIsFencedByToken(t *testing.T) {
	repo, db := backupTestRepository(t)
	ctx := context.Background()
	holder, other := uuid.New(), uuid.New()

	won, err := repo.AcquireLease(ctx, holder, time.Minute)
	require.NoError(t, err)
	require.True(t, won)
	won, err = repo.AcquireLease(ctx, other, time.Minute)
	require.NoError(t, err)
	require.False(t, won, "a live lease cannot be taken")

	require.NoError(t, repo.ReleaseLease(ctx, other), "release by a non-holder is a no-op")
	renewed, err := repo.RenewLease(ctx, other, time.Minute)
	require.NoError(t, err)
	require.False(t, renewed, "a non-holder cannot renew")
	won, err = repo.AcquireLease(ctx, other, time.Minute)
	require.NoError(t, err)
	require.False(t, won, "the holder still has the lease after a foreign release")

	_, err = db.Exec(`UPDATE backup_settings SET lease_expires_at = NOW() - INTERVAL '1 second' WHERE id = 1`)
	require.NoError(t, err)
	won, err = repo.AcquireLease(ctx, other, time.Minute)
	require.NoError(t, err)
	require.True(t, won, "an expired lease can be reacquired")
	renewed, err = repo.RenewLease(ctx, holder, time.Minute)
	require.NoError(t, err)
	require.False(t, renewed, "the previous holder lost the lease")

	require.NoError(t, repo.ReleaseLease(ctx, other))
	won, err = repo.AcquireLease(ctx, holder, time.Minute)
	require.NoError(t, err)
	require.True(t, won, "a released lease is free")
}

func TestBackupClaimScheduleHasOneWinner(t *testing.T) {
	repo, db := backupTestRepository(t)
	ctx := context.Background()
	slot := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	_, err := db.Exec(`UPDATE backup_settings SET next_run_at = $1 WHERE id = 1`, slot)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := repo.ClaimSchedule(ctx, &slot, slot.Add(24*time.Hour))
			require.NoError(t, err)
			if won {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, winners)
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.NextRunAt.Equal(slot.Add(24*time.Hour)))

	_, err = db.Exec(`UPDATE backup_settings SET enabled = FALSE WHERE id = 1`)
	require.NoError(t, err)
	next := slot.Add(24 * time.Hour)
	won, err := repo.ClaimSchedule(ctx, &next, next.Add(24*time.Hour))
	require.NoError(t, err)
	require.False(t, won, "a disabled schedule is not claimed")
}

func TestBackupRunsAndStaleRecovery(t *testing.T) {
	repo, db := backupTestRepository(t)
	ctx := context.Background()

	run := &domain.BackupRun{ID: uuid.New(), Kind: domain.BackupKindManual, Status: domain.BackupStatusRunning,
		UploadStatus: domain.BackupUploadUploading, Destination: "local"}
	require.NoError(t, repo.CreateRun(ctx, run))
	require.False(t, run.StartedAt.IsZero())

	token := uuid.New()
	won, err := repo.AcquireLease(ctx, token, time.Minute)
	require.NoError(t, err)
	require.True(t, won)
	marked, err := repo.MarkStaleRunning(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, marked, "a run under a live lease is not interrupted")

	_, err = db.Exec(`UPDATE backup_settings SET lease_expires_at = NOW() - INTERVAL '1 second' WHERE id = 1`)
	require.NoError(t, err)
	marked, err = repo.MarkStaleRunning(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, marked)
	got, err := repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, domain.BackupStatusFailed, got.Status)
	require.Equal(t, domain.BackupUploadFailed, got.UploadStatus)
	require.Equal(t, "interrupted", *got.Error)

	done := &domain.BackupRun{ID: uuid.New(), Kind: domain.BackupKindScheduled, Status: domain.BackupStatusRunning, UploadStatus: domain.BackupUploadNone}
	require.NoError(t, repo.CreateRun(ctx, done))
	sum, size := "abc", int64(42)
	done.Status, done.ChecksumSHA256, done.SizeBytes = domain.BackupStatusSucceeded, &sum, &size
	done.UploadStatus, done.S3Key = domain.BackupUploadUploaded, "kuayle/done.tar.gz"
	done.S3Endpoint, done.S3Bucket = "https://s3.example.com", "backups"
	require.NoError(t, repo.UpdateRun(ctx, done))
	retainable, err := repo.ListRetainable(ctx)
	require.NoError(t, err)
	require.Len(t, retainable, 1)
	require.Equal(t, done.ID, retainable[0].ID)
	require.Equal(t, "https://s3.example.com", retainable[0].S3Endpoint, "retention sees where the copy went")
	require.Equal(t, "backups", retainable[0].S3Bucket)
	created := &domain.BackupRun{ID: uuid.New(), Kind: domain.BackupKindManual, Status: domain.BackupStatusFailed,
		UploadStatus: domain.BackupUploadNone, S3Endpoint: "https://other.example.com", S3Bucket: "other"}
	require.NoError(t, repo.CreateRun(ctx, created))
	got, err = repo.GetRun(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "https://other.example.com", got.S3Endpoint)
	require.Equal(t, "other", got.S3Bucket)
	require.NoError(t, repo.MarkDeleted(ctx, done.ID, time.Now()))
	retainable, err = repo.ListRetainable(ctx)
	require.NoError(t, err)
	require.Empty(t, retainable)

	runs, err := repo.ListRuns(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 3)

	_, err = repo.GetRun(ctx, uuid.New())
	require.ErrorIs(t, err, ErrBackupRunNotFound)
	require.ErrorIs(t, repo.UpdateRun(ctx, &domain.BackupRun{ID: uuid.New(), Status: "failed", UploadStatus: "none"}), ErrBackupRunNotFound)
}

func TestBackupSettingsRoundTrip(t *testing.T) {
	repo, _ := backupTestRepository(t)
	ctx := context.Background()
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	original := *settings
	t.Cleanup(func() { _ = repo.UpdateSettings(context.Background(), &original) })

	next := time.Date(2026, 11, 16, 4, 30, 0, 0, time.UTC)
	settings.BackupSchedule = domain.BackupSchedule{Kind: "weekly", Time: "04:30", Weekday: 1}
	settings.BackupRetention = domain.BackupRetention{Kind: "days", Value: 14}
	settings.S3Enabled, settings.S3Bucket, settings.S3SecretKeyEncrypted = true, "bucket", "cipher"
	settings.NextRunAt = &next
	require.NoError(t, repo.UpdateSettings(ctx, settings))
	got, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, settings.BackupSchedule, got.BackupSchedule)
	require.Equal(t, settings.BackupRetention, got.BackupRetention)
	require.Equal(t, "cipher", got.S3SecretKeyEncrypted)
	require.True(t, got.NextRunAt.Equal(next))
}
