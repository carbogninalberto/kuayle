package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The assistant's prompt pins today's date, weekday and zone: at
// 2026-11-13 22:30 UTC it is still Friday in UTC, while UTC+13 has
// crossed midnight, so "next Friday" resolves in the user's day.
func TestAssistantTodayPinsTheDateInTheUsersZone(t *testing.T) {
	now := time.Date(2026, 11, 13, 22, 30, 0, 0, time.UTC)
	require.Equal(t, "Today is Saturday, 2026-11-14 (Pacific/Auckland).", assistantToday(now, "Pacific/Auckland", time.UTC))
	require.Equal(t, "Today is Friday, 2026-11-13 (UTC).", assistantToday(now, "", time.UTC), "no profile timezone keeps the workspace's UTC day")
	berlin := time.FixedZone("Europe/Berlin", 3600)
	require.Equal(t, "Today is Friday, 2026-11-13 (Europe/Berlin).", assistantToday(now, "", berlin), "the workspace zone answers when the profile has none")
}

// The zone falls back from the user's profile timezone to the workspace's
// to UTC; an unknown name never fails a reply.
func TestAssistantTimezoneFallsBackFromProfileToWorkspaceToUTC(t *testing.T) {
	berlin := time.FixedZone("Europe/Berlin", 3600)
	require.Equal(t, "Pacific/Auckland", assistantTimezone("Pacific/Auckland", berlin).String())
	require.Equal(t, "Europe/Berlin", assistantTimezone("", berlin).String())
	require.Equal(t, "Europe/Berlin", assistantTimezone("Mars/Olympus", berlin).String(), "an unknown profile timezone falls back")
	require.Equal(t, time.UTC, assistantTimezone("", nil))
	require.Equal(t, time.UTC, assistantTimezone("Mars/Olympus", nil))
}
