package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIssueTemplateScheduleAcrossTimezones(t *testing.T) {
	for _, tt := range []struct{ name, rule, after, want string }{
		{"local morning before spring change", `{"cron":"0 9 * * *","timezone":"America/New_York"}`, "2026-03-07T14:00:00Z", "2026-03-08T13:00:00Z"},
		{"spring gap runs when clocks resume", `{"cron":"30 2 * * *","timezone":"America/New_York"}`, "2026-03-07T07:30:00Z", "2026-03-08T07:00:00Z"},
		{"spring gap does not shift subsequent days", `{"cron":"30 2 * * *","timezone":"America/New_York"}`, "2026-03-08T07:00:00Z", "2026-03-09T06:30:00Z"},
		{"fall overlap uses the first occurrence", `{"cron":"30 1 * * *","timezone":"America/New_York"}`, "2026-10-31T05:30:00Z", "2026-11-01T05:30:00Z"},
		{"fall overlap is not repeated", `{"cron":"30 1 * * *","timezone":"America/New_York"}`, "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z"},
		{"fall overlap remains skipped on restart", `{"cron":"30 1 * * *","timezone":"America/New_York"}`, "2026-11-01T06:10:00Z", "2026-11-02T06:30:00Z"},
		{"fractional offset", `{"cron":"0 9 * * *","timezone":"Asia/Kathmandu"}`, "2026-11-01T03:15:00Z", "2026-11-02T03:15:00Z"},
		{"UTC default", `{"cron":"0 9 * * *"}`, "2026-11-01T09:00:00Z", "2026-11-02T09:00:00Z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			after, err := time.Parse(time.RFC3339, tt.after)
			require.NoError(t, err)
			rule, err := ParseIssueTemplateSchedule(json.RawMessage(tt.rule))
			require.NoError(t, err)
			next, err := rule.Next(after)
			require.NoError(t, err)
			require.Equal(t, tt.want, next.Format(time.RFC3339))
		})
	}
}
