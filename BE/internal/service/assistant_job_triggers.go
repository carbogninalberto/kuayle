package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const jobLifetime = 30 * 24 * time.Hour
const maxJobContinuations = 10

type jobSpec struct {
	Kind            string        `json:"kind"`
	Action          string        `json:"action"`
	Instructions    string        `json:"instructions"`
	At              string        `json:"at,omitempty"`
	DelaySeconds    int           `json:"delay_seconds,omitempty"`
	IntervalSeconds int           `json:"interval_seconds,omitempty"`
	Cron            string        `json:"cron,omitempty"`
	Deadline        string        `json:"deadline,omitempty"`
	Condition       *jobCondition `json:"condition,omitempty"`
}

type jobCondition struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Path      string          `json:"path"`
	Op        string          `json:"op"`
	Value     any             `json:"value"`
}

func jobTime(raw string, zone *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, raw, zone); err == nil && t.Format(layout) == raw {
			return t, nil
		}
	}
	return time.Time{}, toolFailure("give a valid local ISO date and time or an RFC3339 timestamp with offset")
}

func (p jobSpec) nextCron(now time.Time, zone *time.Location) (time.Time, error) {
	if len(strings.Fields(p.Cron)) != 5 {
		return time.Time{}, toolFailure("cron needs five fields: minute hour day month weekday")
	}
	schedule, err := cron.ParseStandard(p.Cron)
	if err != nil {
		return time.Time{}, toolFailure("invalid five-field cron expression")
	}
	return schedule.Next(now.In(zone)), nil
}

func (p jobSpec) schedule(now time.Time, zone *time.Location) (time.Time, time.Time, error) {
	deadline := now.Add(jobLifetime)
	var next time.Time
	var err error
	if p.Deadline != "" {
		deadline, err = jobTime(p.Deadline, zone)
		if err != nil {
			return next, deadline, err
		}
	}
	if !deadline.After(now) || deadline.After(now.Add(jobLifetime)) {
		return next, deadline, toolFailure("deadline must be in the next 30 days")
	}
	if (p.Kind != "once" && (p.At != "" || p.DelaySeconds != 0)) ||
		(p.Kind != "cron" && p.Cron != "") ||
		(p.Kind != "condition" && p.Condition != nil) ||
		(p.Kind != "condition" && p.Kind != "interval" && p.IntervalSeconds != 0) {
		return next, deadline, toolFailure("give only the fields for the selected trigger")
	}
	switch p.Kind {
	case "once":
		if (p.At != "") == (p.DelaySeconds != 0) {
			return next, deadline, toolFailure("give one of at or delay_seconds")
		}
		if p.At != "" {
			next, err = jobTime(p.At, zone)
		} else if p.DelaySeconds > 0 && p.DelaySeconds <= int(jobLifetime.Seconds()) {
			next = now.Add(time.Duration(p.DelaySeconds) * time.Second)
		}
	case "cron":
		next, err = p.nextCron(now, zone)
	case "condition", "interval":
		if p.IntervalSeconds < 60 || p.IntervalSeconds > int(jobLifetime.Seconds()) {
			return next, deadline, toolFailure("interval_seconds must be between 60 and 2592000")
		}
		next = now.Add(time.Duration(p.IntervalSeconds) * time.Second)
		if p.Kind == "condition" {
			if p.Condition == nil || p.Deadline == "" {
				return next, deadline, toolFailure("a condition needs a probe and a hard deadline")
			}
			if err = p.Condition.validate(); err != nil {
				return next, deadline, err
			}
			next = now
		}
	default:
		return next, deadline, toolFailure("unknown trigger kind")
	}
	if err != nil {
		return next, deadline, err
	}
	if next.Before(now) || !next.Before(deadline) || (p.Kind != "condition" && next.Equal(now)) {
		return next, deadline, toolFailure("the first run must fall before the deadline and within the next 30 days")
	}
	return next, deadline, nil
}

func (c jobCondition) validate() error {
	if c.Tool == "" || len(c.Arguments) == 0 || c.Path == "" {
		return toolFailure("give the probe tool, its arguments and a result field path")
	}
	switch c.Op {
	case "eq", "ne":
		switch c.Value.(type) {
		case string, bool, float64:
			return nil
		}
	case "gt", "gte", "lt", "lte":
		if _, ok := c.Value.(float64); ok {
			return nil
		}
	}
	return toolFailure("give a scalar equality target or a numeric threshold")
}

func (c jobCondition) matches(result any) (bool, error) {
	// Normalize typed tool results the same way the model sees them.
	data, err := json.Marshal(result)
	if err != nil {
		return false, err
	}
	var field any
	if err = json.Unmarshal(data, &field); err != nil {
		return false, err
	}
	for _, key := range strings.Split(c.Path, ".") {
		var ok bool
		switch value := field.(type) {
		case map[string]any:
			field, ok = value[key]
		case []any:
			i, e := strconv.Atoi(key)
			if e == nil && i >= 0 && i < len(value) {
				field, ok = value[i], true
			}
		}
		if !ok {
			return false, toolFailure("probe result field was missing")
		}
	}
	switch field.(type) {
	case string, bool, float64:
	default:
		return false, toolFailure("probe result field was not a scalar")
	}
	if c.Op == "eq" {
		return field == c.Value, nil
	}
	if c.Op == "ne" {
		return field != c.Value, nil
	}
	n, ok := field.(float64)
	if !ok {
		// A custom script's stdout is text, including a single numeric reading.
		raw, _ := field.(string)
		n, err = strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return false, toolFailure("probe result field was not numeric")
		}
	}
	target, ok := c.Value.(float64)
	if !ok {
		return false, toolFailure("numeric comparison needs a number")
	}
	switch c.Op {
	case "gt":
		return n > target, nil
	case "gte":
		return n >= target, nil
	case "lt":
		return n < target, nil
	case "lte":
		return n <= target, nil
	}
	return false, toolFailure("unknown comparison")
}
