package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func utc(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBackupScheduleNextRun(t *testing.T) {
	cases := []struct {
		name     string
		schedule BackupSchedule
		after    string
		want     string
	}{
		{"daily later today", BackupSchedule{Kind: "daily", Time: "03:00"}, "2026-11-15 01:00", "2026-11-15 03:00"},
		{"daily at the slot moves to tomorrow", BackupSchedule{Kind: "daily", Time: "03:00"}, "2026-11-15 03:00", "2026-11-16 03:00"},
		{"daily across month end", BackupSchedule{Kind: "daily", Time: "00:30"}, "2026-11-30 23:00", "2026-12-01 00:30"},
		// 2026-11-15 is a Sunday.
		{"weekly later this week", BackupSchedule{Kind: "weekly", Time: "02:00", Weekday: 3}, "2026-11-15 10:00", "2026-11-18 02:00"},
		{"weekly same day earlier wraps a week", BackupSchedule{Kind: "weekly", Time: "02:00", Weekday: 0}, "2026-11-15 10:00", "2026-11-22 02:00"},
		{"weekly wraps across the year", BackupSchedule{Kind: "weekly", Time: "23:59", Weekday: 1}, "2026-12-29 00:00", "2027-01-04 23:59"},
		{"cron every 15 minutes", BackupSchedule{Kind: "cron", Cron: "*/15 * * * *"}, "2026-11-15 10:07", "2026-11-15 10:15"},
		{"cron list and range", BackupSchedule{Kind: "cron", Cron: "0 1,13 * * 1-5"}, "2026-11-14 14:00", "2026-11-16 01:00"},
		{"cron range with step", BackupSchedule{Kind: "cron", Cron: "30 8-20/6 * * *"}, "2026-11-15 15:00", "2026-11-15 20:30"},
		{"cron day of month", BackupSchedule{Kind: "cron", Cron: "0 4 1 * *"}, "2026-11-15 10:00", "2026-12-01 04:00"},
		{"cron month", BackupSchedule{Kind: "cron", Cron: "0 0 1 3 *"}, "2026-11-15 10:00", "2027-03-01 00:00"},
		{"cron day fields are OR'd", BackupSchedule{Kind: "cron", Cron: "0 0 20 * 1"}, "2026-11-15 10:00", "2026-11-16 00:00"},
		{"cron 7 is Sunday", BackupSchedule{Kind: "cron", Cron: "0 5 * * 7"}, "2026-11-15 10:00", "2026-11-22 05:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, err := tc.schedule.NextRun(utc(tc.after))
			require.NoError(t, err)
			require.Equal(t, utc(tc.want), next)
			require.Equal(t, time.UTC, next.Location())
		})
	}
}

func TestBackupScheduleNextRunUsesUTC(t *testing.T) {
	rome := time.FixedZone("CET", 3600)
	after := time.Date(2026, 11, 15, 3, 30, 0, 0, rome) // 02:30 UTC
	next, err := BackupSchedule{Kind: "daily", Time: "03:00"}.NextRun(after)
	require.NoError(t, err)
	require.Equal(t, utc("2026-11-15 03:00"), next)
}

func TestBackupScheduleRejectsInvalid(t *testing.T) {
	for _, schedule := range []BackupSchedule{
		{Kind: "hourly"},
		{Kind: "daily", Time: "3:00"},
		{Kind: "daily", Time: "24:00"},
		{Kind: "daily", Time: "12:60"},
		{Kind: "weekly", Time: "12:00", Weekday: 7},
		{Kind: "weekly", Time: "12:00", Weekday: -1},
		{Kind: "cron", Cron: ""},
		{Kind: "cron", Cron: "* * * *"},
		{Kind: "cron", Cron: "* * * * * *"},
		{Kind: "cron", Cron: "60 * * * *"},
		{Kind: "cron", Cron: "* 24 * * *"},
		{Kind: "cron", Cron: "* * 0 * *"},
		{Kind: "cron", Cron: "* * * 13 *"},
		{Kind: "cron", Cron: "* * * * 8"},
		{Kind: "cron", Cron: "5-1 * * * *"},
		{Kind: "cron", Cron: "*/0 * * * *"},
		{Kind: "cron", Cron: "5/2 * * * *"},
		{Kind: "cron", Cron: "@daily * * * *"},
		{Kind: "cron", Cron: "0 0 * JAN *"},
		{Kind: "cron", Cron: "0 0 L * *"},
		{Kind: "cron", Cron: "-1 * * * *"},
	} {
		_, err := schedule.NextRun(utc("2026-11-15 00:00"))
		require.Error(t, err, "%+v", schedule)
		require.True(t, errors.Is(err, ErrInvalidBackupSchedule), "%+v", schedule)
	}
}

func TestBackupCronThatNeverMatchesIsAnError(t *testing.T) {
	_, err := BackupSchedule{Kind: "cron", Cron: "0 0 30 2 *"}.NextRun(utc("2026-11-15 00:00"))
	require.ErrorIs(t, err, ErrInvalidBackupSchedule)
}

type retentionRun struct {
	age    time.Duration
	status string
	upload string
	kind   string
}

func retentionFixture(now time.Time, specs ...retentionRun) []BackupRun {
	runs := make([]BackupRun, len(specs))
	for i, spec := range specs {
		kind := spec.kind
		if kind == "" {
			kind = BackupKindScheduled
		}
		upload := spec.upload
		if upload == "" {
			upload = BackupUploadNone
		}
		runs[i] = BackupRun{ID: uuid.New(), Kind: kind, Status: spec.status, UploadStatus: upload, StartedAt: now.Add(-spec.age)}
	}
	return runs
}

func ids(runs []BackupRun) []uuid.UUID {
	out := []uuid.UUID{}
	for _, run := range runs {
		out = append(out, run.ID)
	}
	return out
}

func TestSelectForRetention(t *testing.T) {
	now := utc("2026-11-15 12:00")
	day := 24 * time.Hour
	ok := BackupStatusSucceeded

	t.Run("count keeps the newest N successes", func(t *testing.T) {
		runs := retentionFixture(now,
			retentionRun{age: 4 * day, status: ok},
			retentionRun{age: 1 * day, status: ok},
			retentionRun{age: 3 * day, status: ok},
			retentionRun{age: 2 * day, status: BackupStatusFailed},
			retentionRun{age: 2 * day, status: ok},
		)
		got := SelectForRetention(runs, BackupRetention{Kind: "count", Value: 2}, now)
		require.ElementsMatch(t, []uuid.UUID{runs[0].ID, runs[2].ID}, ids(got))
	})

	t.Run("days never removes the last success even when every backup is old", func(t *testing.T) {
		runs := retentionFixture(now,
			retentionRun{age: 30 * day, status: ok},
			retentionRun{age: 20 * day, status: ok},
			retentionRun{age: 40 * day, status: ok},
		)
		got := SelectForRetention(runs, BackupRetention{Kind: "days", Value: 7}, now)
		require.ElementsMatch(t, []uuid.UUID{runs[0].ID, runs[2].ID}, ids(got))
	})

	t.Run("days keeps recent backups", func(t *testing.T) {
		runs := retentionFixture(now,
			retentionRun{age: 1 * day, status: ok},
			retentionRun{age: 6 * day, status: ok},
			retentionRun{age: 8 * day, status: ok},
		)
		got := SelectForRetention(runs, BackupRetention{Kind: "days", Value: 7}, now)
		require.Equal(t, []uuid.UUID{runs[2].ID}, ids(got))
	})

	t.Run("an uploading backup is kept", func(t *testing.T) {
		runs := retentionFixture(now,
			retentionRun{age: 1 * day, status: ok},
			retentionRun{age: 2 * day, status: ok, upload: BackupUploadUploading},
			retentionRun{age: 3 * day, status: ok, upload: BackupUploadUploaded},
		)
		got := SelectForRetention(runs, BackupRetention{Kind: "count", Value: 1}, now)
		require.Equal(t, []uuid.UUID{runs[2].ID}, ids(got))
	})

	t.Run("running, failed, deleted and pre-upgrade runs are left alone", func(t *testing.T) {
		deleted := now
		runs := retentionFixture(now,
			retentionRun{age: 0, status: BackupStatusRunning},
			retentionRun{age: 1 * day, status: ok},
			retentionRun{age: 9 * day, status: BackupStatusFailed},
			retentionRun{age: 9 * day, status: ok, kind: BackupKindPreUpgrade},
			retentionRun{age: 9 * day, status: ok},
		)
		runs[4].DeletedAt = &deleted
		got := SelectForRetention(runs, BackupRetention{Kind: "count", Value: 1}, now)
		require.Empty(t, got)
	})

	t.Run("a newer failure does not make the last success removable", func(t *testing.T) {
		runs := retentionFixture(now,
			retentionRun{age: 1 * day, status: BackupStatusFailed},
			retentionRun{age: 30 * day, status: ok},
		)
		require.Empty(t, SelectForRetention(runs, BackupRetention{Kind: "days", Value: 1}, now))
	})
}
