package domain

import (
	"errors"
	"time"
)

// NextAutomationCron applies Kuayle's five-field backup cron grammar to local
// wall time. Search each constant-offset period separately: a missing minute
// never matches, and a repeated minute has two distinct instants.
func NextAutomationCron(expr, zone string, after time.Time) (time.Time, error) {
	if zone == "" || zone == "Local" || len(zone) > 128 || len(expr) > 128 {
		return time.Time{}, errors.New("invalid automation schedule")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	spec, err := ParseBackupCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	from := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := from.AddDate(5, 0, 0)
	for from.Before(limit) {
		local := from.In(loc)
		_, offset := local.Zone()
		_, end := local.ZoneBounds()
		shift := time.Duration(offset) * time.Second
		wall, err := spec.Next(from.Add(shift - time.Minute))
		if err != nil {
			return time.Time{}, err
		}
		next := wall.Add(-shift)
		if !end.IsZero() && !end.After(from) {
			// A synthesized IANA rule may return the end of a 365-day
			// year on the last day of a leap year. Recheck one minute on.
			end = from.Add(time.Minute)
		}
		if end.IsZero() || next.Before(end) {
			if !next.Before(limit) {
				break
			}
			return next, nil
		}
		from = end.UTC()
	}
	return time.Time{}, errors.New("automation schedule has no occurrence within five years")
}
