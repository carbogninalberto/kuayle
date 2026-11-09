package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWikiOverviewScheduleNextRun(t *testing.T) {
	daily := WikiOverviewSchedule{Enabled: true, Frequency: WikiOverviewDaily, Time: "09:30", PeriodDays: 1}
	before := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	next, err := daily.NextRun(before, time.UTC)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC), next, "later today")
	next, err = daily.NextRun(next, time.UTC)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC), next, "the slot itself is not after itself")

	// 2026-11-09 is a Monday.
	weekly := WikiOverviewSchedule{Frequency: WikiOverviewWeekly, Weekday: 5, Time: "17:00", PeriodDays: 7}
	next, err = weekly.NextRun(before, time.UTC)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC), next)
	monday := WikiOverviewSchedule{Frequency: WikiOverviewWeekly, Weekday: 1, Time: "07:00", PeriodDays: 7}
	next, err = monday.NextRun(before, time.UTC)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC), next, "this week's slot already passed")

	// Across the end of daylight saving time the wall-clock time is kept.
	rome, err := time.LoadLocation("Europe/Rome")
	require.NoError(t, err)
	next, err = daily.NextRun(time.Date(2026, 10, 24, 12, 0, 0, 0, rome), rome)
	require.NoError(t, err)
	require.Equal(t, "2026-10-25 09:30 CET", next.Format("2006-01-02 15:04 MST"))

	for _, bad := range []WikiOverviewSchedule{
		{Frequency: "monthly", Time: "09:00", PeriodDays: 7},
		{Frequency: WikiOverviewWeekly, Weekday: 7, Time: "09:00", PeriodDays: 7},
		{Frequency: WikiOverviewDaily, Time: "24:00", PeriodDays: 7},
		{Frequency: WikiOverviewDaily, Time: "9:00", PeriodDays: 7},
		{Frequency: WikiOverviewDaily, Time: "09:00", PeriodDays: 32},
	} {
		_, err := bad.NextRun(before, time.UTC)
		require.ErrorIs(t, err, ErrWikiOverviewSchedule, bad)
	}
}

func TestWikiOverviewSchedulePeriodEndsOnTheRunDay(t *testing.T) {
	start, end := WikiOverviewSchedule{PeriodDays: 7}.Period(time.Date(2026, 11, 9, 23, 30, 0, 0, time.UTC), time.UTC)
	require.Equal(t, "2026-11-03", start.Format(time.DateOnly))
	require.Equal(t, "2026-11-09", end.Format(time.DateOnly))
}
