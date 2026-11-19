package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAutomationCronSkipsMissingMinutesAndMatchesBothRepeatedMinutes(t *testing.T) {
	for _, tc := range []struct{ from, want string }{
		{"2026-03-28T01:30:00Z", "2026-03-30T00:30:00Z"},
		{"2026-10-24T00:30:00Z", "2026-10-25T00:30:00Z"},
		{"2026-10-25T00:30:00Z", "2026-10-25T01:30:00Z"},
	} {
		after, err := time.Parse(time.RFC3339, tc.from)
		require.NoError(t, err)
		next, err := NextAutomationCron("30 2 * * *", "Europe/Rome", after)
		require.NoError(t, err)
		require.Equal(t, tc.want, next.UTC().Format(time.RFC3339))
	}
}
