package domain

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // reset times name IANA zones; machines may lack zoneinfo
)

// A UsageLimit is a coding agent's provider refusing work until a quota
// resets: Claude Code's 5-hour or weekly subscription limit, Codex's usage
// limit, and the like. Unlike a failure, the work and the agent's session are
// intact; the task waits for ResetsAt and continues.
type UsageLimit struct {
	// Message is the provider's own text, shown to the user.
	Message string
	// ResetsAt is when the provider accepts work again, when it said so.
	ResetsAt *time.Time
}

const (
	// UsageLimitErrorCode prefixes a run's error message, like sign_in_required.
	UsageLimitErrorCode = "usage_limit_reached"
	// UsageLimitResumeMargin waits a little past the reset, so the first
	// request after it is not refused by a clock a few seconds behind.
	UsageLimitResumeMargin = 2 * time.Minute
	// UsageLimitUnknownBackoff is the wait when the provider gave no time.
	UsageLimitUnknownBackoff = 30 * time.Minute
	// UsageLimitMaxWait bounds a reset time read from text (weekly limits
	// reset within seven days).
	UsageLimitMaxWait = 8 * 24 * time.Hour
	// MaxUsageLimitAutoResumes is how many times in a row a task resumes on
	// its own before the user has to.
	MaxUsageLimitAutoResumes = 5
)

// ResumeAt is when a task paused by this limit should start again.
func (l UsageLimit) ResumeAt(now time.Time) time.Time {
	if l.ResetsAt == nil {
		return now.Add(UsageLimitUnknownBackoff)
	}
	reset := *l.ResetsAt
	if !reset.After(now) {
		return now.Add(UsageLimitResumeMargin)
	}
	if reset.Sub(now) > UsageLimitMaxWait {
		return now.Add(UsageLimitMaxWait)
	}
	return reset.Add(UsageLimitResumeMargin)
}

// ErrorMessage encodes the limit as a run error message:
// "usage_limit_reached: <message>" with "\nresets_at=<RFC 3339>" when known.
func (l UsageLimit) ErrorMessage() string {
	text := UsageLimitErrorCode + ": " + l.Message
	if l.ResetsAt != nil {
		text += "\nresets_at=" + l.ResetsAt.UTC().Format(time.RFC3339)
	}
	return text
}

// ParseUsageLimitError reads a run error message written by ErrorMessage.
func ParseUsageLimitError(text string) (UsageLimit, bool) {
	rest, ok := strings.CutPrefix(text, UsageLimitErrorCode+": ")
	if !ok {
		return UsageLimit{}, false
	}
	message, reset, _ := strings.Cut(rest, "\nresets_at=")
	limit := UsageLimit{Message: strings.TrimSpace(message)}
	if reset != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(reset)); err == nil {
			limit.ResetsAt = &parsed
		}
	}
	return limit, true
}

var (
	// Wording the providers use for a quota, never for an ordinary failure.
	usageLimitText = regexp.MustCompile(`(?i)(you've hit your [\w-]*\s*(?:usage |weekly |session )?limit|usage limit reached|usage_limit_reached|limit will reset at)`)
	legacyEpoch    = regexp.MustCompile(`(?i)usage limit reached\|(\d{9,11})`)
	clockReset     = regexp.MustCompile(`(?i)reset(?:s| at)\s+(?:at\s+)?(?:(mon|tue|wed|thu|fri|sat|sun)[a-z]*,?\s+)?(?:(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2}),?\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*\(([^)]+)\)`)
	relativeReset  = regexp.MustCompile(`(?i)try again in\s+((?:\d+\s*(?:days?|hours?|hrs?|minutes?|mins?|seconds?|secs?)[\s,and]*)+)`)
	durationPart   = regexp.MustCompile(`(?i)(\d+)\s*(day|hour|hr|minute|min|second|sec)`)
)

// DetectUsageLimit recognises a provider usage limit in a failed run's output
// and summary. Claude Code's stream-json reports it as a rejected
// rate_limit_event with an exact reset time; other agents only say it in text.
func DetectUsageLimit(stdout, stderr, summary string, now time.Time) (UsageLimit, bool) {
	limit, found := claudeStreamLimit(stdout)
	texts := append(jsonMessages(stdout), summary, lastLines(stderr, 20), lastLines(stdout, 20))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" || !usageLimitText.MatchString(text) {
			continue
		}
		if !found || limit.Message == "" {
			limit.Message = firstLine(text)
		}
		if limit.ResetsAt == nil {
			limit.ResetsAt = resetFromText(text, now)
		}
		found = true
		break
	}
	if !found {
		return UsageLimit{}, false
	}
	if limit.Message == "" {
		limit.Message = "The agent's usage limit was reached"
	}
	if len(limit.Message) > 500 {
		limit.Message = limit.Message[:500]
	}
	return limit, true
}

type claudeRateLimitInfo struct {
	Status   string `json:"status"`
	ResetsAt int64  `json:"resetsAt"`
}

// claudeStreamLimit reads Claude Code's rejected rate_limit_event and the
// assistant message flagged usage_limit_reached.
func claudeStreamLimit(stdout string) (UsageLimit, bool) {
	var limit UsageLimit
	found := false
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, "rate_limit") && !strings.Contains(line, UsageLimitErrorCode) {
			continue
		}
		var event struct {
			Type          string               `json:"type"`
			RateLimitInfo *claudeRateLimitInfo `json:"rate_limit_info"`
			APIError      string               `json:"api_error"`
			APIParams     struct {
				RateLimitInfo *claudeRateLimitInfo `json:"rate_limit_info"`
			} `json:"api_error_params"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		info := event.RateLimitInfo
		if event.APIError == UsageLimitErrorCode {
			found = true
			if event.APIParams.RateLimitInfo != nil {
				info = event.APIParams.RateLimitInfo
			}
			for _, part := range event.Message.Content {
				if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
					limit.Message = strings.TrimSpace(part.Text)
				}
			}
		}
		if event.Type == "rate_limit_event" && info != nil && info.Status == "rejected" {
			found = true
		}
		if info != nil && info.Status == "rejected" && info.ResetsAt > 0 {
			reset := time.Unix(info.ResetsAt, 0).UTC()
			limit.ResetsAt = &reset
		}
	}
	return limit, found
}

// jsonMessages collects error and result texts from JSON-lines output
// (Claude result, Codex error/turn.failed, OpenCode error events).
func jsonMessages(stdout string) []string {
	var out []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		for _, key := range []string{"result", "message", "error"} {
			switch value := event[key].(type) {
			case string:
				out = append(out, value)
			case map[string]any:
				if message, ok := value["message"].(string); ok {
					out = append(out, message)
				}
			}
		}
	}
	return out
}

func resetFromText(text string, now time.Time) *time.Time {
	if match := legacyEpoch.FindStringSubmatch(text); match != nil {
		if seconds, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			reset := time.Unix(seconds, 0).UTC()
			return &reset
		}
	}
	if match := clockReset.FindStringSubmatch(text); match != nil {
		if reset, ok := clockTime(match, now); ok {
			return &reset
		}
	}
	if match := relativeReset.FindStringSubmatch(text); match != nil {
		var total time.Duration
		for _, part := range durationPart.FindAllStringSubmatch(match[1], -1) {
			n, _ := strconv.Atoi(part[1])
			unit := map[string]time.Duration{"day": 24 * time.Hour, "hour": time.Hour, "hr": time.Hour, "minute": time.Minute, "min": time.Minute, "second": time.Second, "sec": time.Second}[strings.ToLower(part[2])]
			total += time.Duration(n) * unit
		}
		if total > 0 {
			reset := now.Add(total).UTC()
			return &reset
		}
	}
	return nil
}

// clockTime resolves "4:10am (UTC)", "Thu 9am (Europe/Rome)" or
// "Nov 15, 11am (UTC)" to the next such moment after now.
func clockTime(match []string, now time.Time) (time.Time, bool) {
	weekday, month, day := match[1], match[2], match[3]
	hour, _ := strconv.Atoi(match[4])
	minute := 0
	if match[5] != "" {
		minute, _ = strconv.Atoi(match[5])
	}
	switch strings.ToLower(match[6]) {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour < 12 {
			hour += 12
		}
	}
	if hour > 23 || minute > 59 {
		return time.Time{}, false
	}
	location, err := time.LoadLocation(strings.TrimSpace(match[7]))
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(location)
	if month != "" {
		// A calendar date: this year, or next year once it has passed.
		months := map[string]time.Month{"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April, "may": time.May, "jun": time.June, "jul": time.July, "aug": time.August, "sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December}
		dayOfMonth, _ := strconv.Atoi(day)
		if dayOfMonth < 1 || dayOfMonth > 31 {
			return time.Time{}, false
		}
		reset := time.Date(local.Year(), months[strings.ToLower(month[:3])], dayOfMonth, hour, minute, 0, 0, location)
		if !reset.After(now) {
			reset = reset.AddDate(1, 0, 0)
		}
		return reset.UTC(), true
	}
	reset := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	if weekday != "" {
		weekdays := map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}
		days := (int(weekdays[strings.ToLower(weekday[:3])]) - int(local.Weekday()) + 7) % 7
		reset = reset.AddDate(0, 0, days)
	}
	for !reset.After(now) {
		if weekday != "" {
			reset = reset.AddDate(0, 0, 7)
		} else {
			reset = reset.AddDate(0, 0, 1)
		}
	}
	return reset.UTC(), true
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
