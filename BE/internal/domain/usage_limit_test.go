package domain

import (
	"testing"
	"time"
)

// Lines captured from a production Claude Code run (KUA-98, 2026-11-10) that
// stopped at the 5-hour subscription limit.
const claudeLimitStream = `{"type":"assistant","message":{"content":[{"type":"text","text":"You've hit your 5-hour limit · resets 4:10am (UTC)"}]},"error":"rate_limit","is_api_error_message":true,"api_error_status":429,"api_error":"usage_limit_reached","api_error_params":{"rate_limit_info":{"status":"rejected","resetsAt":1794283800,"rateLimitType":"five_hour"}}}
{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1794283800,"rateLimitType":"five_hour","overageStatus":"rejected"}}
{"type":"result","subtype":"success","is_error":true,"result":"You've hit your 5-hour limit · resets 4:10am (UTC)"}`

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestDetectUsageLimitFromClaudeStream(t *testing.T) {
	now := at(t, "2026-11-10T03:05:54Z")
	limit, ok := DetectUsageLimit(claudeLimitStream, "", "", now)
	if !ok {
		t.Fatal("production Claude Code limit was not detected")
	}
	if limit.Message != "You've hit your 5-hour limit · resets 4:10am (UTC)" {
		t.Fatalf("message = %q", limit.Message)
	}
	if limit.ResetsAt == nil || !limit.ResetsAt.Equal(at(t, "2026-11-10T04:10:00Z")) {
		t.Fatalf("resets at = %v", limit.ResetsAt)
	}
}

func TestDetectUsageLimitIgnoresWarningsAndOrdinaryFailures(t *testing.T) {
	now := at(t, "2026-11-10T03:00:00Z")
	for name, stdout := range map[string]string{
		"warning event": `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1794283800}}`,
		"tests failed":  `{"type":"result","is_error":true,"result":"3 tests failed; the rate limiter test expects 429"}`,
		"empty":         "",
	} {
		if limit, ok := DetectUsageLimit(stdout, "", "", now); ok {
			t.Errorf("%s: detected %+v", name, limit)
		}
	}
	if _, ok := DetectUsageLimit("", "", "Execution failed: exit status 1", now); ok {
		t.Error("an ordinary failure summary was detected as a usage limit")
	}
}

func TestDetectUsageLimitParsesProviderMessages(t *testing.T) {
	now := at(t, "2026-11-10T03:00:00Z")
	for _, tc := range []struct {
		name, text, want string
	}{
		{"claude 5-hour", "You've hit your 5-hour limit · resets 4:10am (UTC)", "2026-11-10T04:10:00Z"},
		{"claude hour only", "You've hit your limit · resets 5am (UTC)", "2026-11-10T05:00:00Z"},
		{"past time rolls to tomorrow", "You've hit your 5-hour limit · resets 2am (UTC)", "2026-11-11T02:00:00Z"},
		{"named zone", "Claude usage limit reached. Your limit will reset at 5pm (Europe/Rome).", "2026-11-10T16:00:00Z"},
		{"weekly with weekday", "You've hit your weekly limit · resets Thu 9am (UTC)", "2026-11-12T09:00:00Z"},
		{"legacy epoch", "Claude AI usage limit reached|1794283800", "2026-11-10T04:10:00Z"},
		{"codex relative", "You've hit your usage limit. Upgrade to Pro (https://openai.com/chatgpt/pricing) or try again in 2 hours 15 minutes.", "2026-11-10T05:15:00Z"},
		{"codex days", "You've hit your usage limit. Try again in 1 day 3 hours.", "2026-11-11T06:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit, ok := DetectUsageLimit("", "", tc.text, now)
			if !ok {
				t.Fatal("not detected")
			}
			if limit.ResetsAt == nil || !limit.ResetsAt.Equal(at(t, tc.want)) {
				t.Fatalf("resets at = %v, want %s", limit.ResetsAt, tc.want)
			}
		})
	}
	// Recognised without a usable reset time.
	limit, ok := DetectUsageLimit("", "", "You've hit your usage limit. Try again later.", now)
	if !ok || limit.ResetsAt != nil {
		t.Fatalf("got %+v, %v", limit, ok)
	}
}

func TestDetectUsageLimitFromCodexJSON(t *testing.T) {
	now := at(t, "2026-11-10T03:00:00Z")
	stdout := `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.failed","error":{"message":"You've hit your usage limit. Upgrade to Plus or try again in 45 minutes."}}`
	limit, ok := DetectUsageLimit(stdout, "", "", now)
	if !ok || limit.ResetsAt == nil || !limit.ResetsAt.Equal(at(t, "2026-11-10T03:45:00Z")) {
		t.Fatalf("got %+v, %v", limit, ok)
	}
}

func TestUsageLimitErrorMessageRoundTrips(t *testing.T) {
	reset := at(t, "2026-11-10T04:10:00Z")
	for _, limit := range []UsageLimit{
		{Message: "You've hit your 5-hour limit · resets 4:10am (UTC)", ResetsAt: &reset},
		{Message: "You've hit your usage limit."},
	} {
		encoded := limit.ErrorMessage()
		decoded, ok := ParseUsageLimitError(encoded)
		if !ok || decoded.Message != limit.Message || (limit.ResetsAt == nil) != (decoded.ResetsAt == nil) ||
			(limit.ResetsAt != nil && !decoded.ResetsAt.Equal(*limit.ResetsAt)) {
			t.Fatalf("%q decoded to %+v, %v", encoded, decoded, ok)
		}
	}
	if _, ok := ParseUsageLimitError("sign_in_required: Claude Code needs you to sign in"); ok {
		t.Fatal("another error code parsed as a usage limit")
	}
}

func TestUsageLimitResumeAt(t *testing.T) {
	now := at(t, "2026-11-10T03:00:00Z")
	reset := at(t, "2026-11-10T04:10:00Z")
	if got := (UsageLimit{ResetsAt: &reset}).ResumeAt(now); !got.Equal(reset.Add(UsageLimitResumeMargin)) {
		t.Fatalf("resume at %v", got)
	}
	if got := (UsageLimit{}).ResumeAt(now); !got.Equal(now.Add(UsageLimitUnknownBackoff)) {
		t.Fatalf("unknown reset resumes at %v", got)
	}
	past := now.Add(-time.Hour)
	if got := (UsageLimit{ResetsAt: &past}).ResumeAt(now); !got.Equal(now.Add(UsageLimitResumeMargin)) {
		t.Fatalf("past reset resumes at %v", got)
	}
	far := now.Add(30 * 24 * time.Hour)
	if got := (UsageLimit{ResetsAt: &far}).ResumeAt(now); !got.Equal(now.Add(UsageLimitMaxWait)) {
		t.Fatalf("far reset resumes at %v", got)
	}
}
