package domain

import (
	"encoding/json"
	"errors"
	"time"
	_ "time/tzdata"
)

var ErrInvalidIssueTemplateSchedule = errors.New("Invalid recurrence rule. Check the cron expression and timezone.")

type IssueTemplateSchedule struct {
	cron     BackupCron
	Location *time.Location
}

// ParseIssueTemplateSchedule accepts the same five-field cron grammar as
// automations. A nil schedule represents an absent or explicitly cleared rule.
func ParseIssueTemplateSchedule(raw json.RawMessage) (*IssueTemplateSchedule, error) {
	var rule *struct {
		Cron     string `json:"cron"`
		Timezone string `json:"timezone"`
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, ErrInvalidIssueTemplateSchedule
	}
	if rule == nil {
		return nil, nil
	}
	if rule.Timezone == "" {
		rule.Timezone = "UTC"
	}
	if rule.Timezone == "Local" || len(rule.Timezone) > 128 || len(rule.Cron) > 128 {
		return nil, ErrInvalidIssueTemplateSchedule
	}
	loc, err := time.LoadLocation(rule.Timezone)
	if err != nil {
		return nil, ErrInvalidIssueTemplateSchedule
	}
	cron, err := ParseBackupCron(rule.Cron)
	if err != nil {
		return nil, ErrInvalidIssueTemplateSchedule
	}
	return &IssueTemplateSchedule{cron: cron, Location: loc}, nil
}

// Next walks constant-offset intervals. A slot in a spring gap runs when
// clocks resume, and the second occurrence of a repeated wall minute is ignored.
func (s IssueTemplateSchedule) Next(after time.Time) (time.Time, error) {
	from := after.UTC().Add(time.Nanosecond)
	limit := from.AddDate(5, 0, 0)
	for from.Before(limit) {
		local := from.In(s.Location)
		start, end := local.ZoneBounds()
		_, offset := local.Zone()
		shift := time.Duration(offset) * time.Second
		if !start.IsZero() {
			start = start.UTC()
			_, previous := start.Add(-time.Nanosecond).In(s.Location).Zone()
			previousShift := time.Duration(previous) * time.Second
			if from.Equal(start) && shift > previousShift {
				missing, err := s.cron.Next(start.Add(previousShift - time.Nanosecond))
				if err == nil && missing.Before(start.Add(shift)) {
					return start, nil
				}
			}
			if repeatedUntil := start.Add(previousShift - shift); repeatedUntil.After(from) {
				from = repeatedUntil
			}
		}
		wall, err := s.cron.Next(from.Add(shift - time.Nanosecond))
		if err != nil {
			break
		}
		next := wall.Add(-shift)
		if !end.IsZero() && !end.After(local) {
			// Go can report the end of a 365-day year on day 366 for a
			// synthesized IANA rule. Re-evaluate the offset at the next minute.
			end = local.UTC().Truncate(time.Minute).Add(time.Minute)
		}
		if end.IsZero() || next.Before(end) {
			if next.Before(limit) {
				return next, nil
			}
			break
		}
		from = end.UTC()
	}
	return time.Time{}, ErrInvalidIssueTemplateSchedule
}
