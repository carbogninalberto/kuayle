package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Overview triggers.
const (
	WikiOverviewTriggerManual   = "manual"
	WikiOverviewTriggerSchedule = "schedule"
)

// Overview schedule frequencies.
const (
	WikiOverviewDaily  = "daily"
	WikiOverviewWeekly = "weekly"
)

// WikiOverviewMaxDays bounds an overview's period, in days.
const WikiOverviewMaxDays = 31

// WikiOverview is an AI summary of a wiki registry over a period.
type WikiOverview struct {
	ID              uuid.UUID  `db:"id"`
	WorkspaceID     uuid.UUID  `db:"workspace_id"`
	TeamID          *uuid.UUID `db:"team_id"`
	ProjectID       *uuid.UUID `db:"project_id"`
	PeriodStart     time.Time  `db:"period_start"`
	PeriodEnd       time.Time  `db:"period_end"`
	Content         string     `db:"content"`
	Trigger         string     `db:"trigger"`
	GeneratedBy     *uuid.UUID `db:"generated_by"`
	GeneratedByName *string    `db:"generated_by_name"`
	GeneratedAt     time.Time  `db:"generated_at"`
}

// WikiOverviewSchedule says when overviews are generated automatically: every
// day, or every week on Weekday (0 = Sunday), at Time (HH:MM), summarising the
// PeriodDays days that end on the day of the run.
type WikiOverviewSchedule struct {
	Enabled    bool   `db:"overview_enabled"`
	Frequency  string `db:"overview_frequency"`
	Weekday    int    `db:"overview_weekday"`
	Time       string `db:"overview_time"`
	PeriodDays int    `db:"overview_period_days"`
}

// DefaultWikiOverviewSchedule is the schedule of a wiki that never set one.
func DefaultWikiOverviewSchedule() WikiOverviewSchedule {
	return WikiOverviewSchedule{Frequency: WikiOverviewWeekly, Weekday: 1, Time: "09:00", PeriodDays: 7}
}

// WikiSettings are a wiki's opt-in features and its overview schedule state.
type WikiSettings struct {
	ID              *uuid.UUID `db:"id"`
	RegistryEnabled bool       `db:"registry_enabled"`
	WikiOverviewSchedule
	NextRunAt *time.Time `db:"overview_next_run_at"`
	LastRunAt *time.Time `db:"overview_last_run_at"`
	LastError *string    `db:"overview_last_error"`
}

// ErrWikiOverviewSchedule rejects an invalid schedule.
var ErrWikiOverviewSchedule = errors.New("invalid overview schedule")

func (s WikiOverviewSchedule) clock() (hour, minute int, err error) {
	if len(s.Time) != 5 {
		return 0, 0, ErrWikiOverviewSchedule
	}
	if _, err := fmt.Sscanf(s.Time, "%02d:%02d", &hour, &minute); err != nil || hour > 23 || minute > 59 || hour < 0 || minute < 0 {
		return 0, 0, ErrWikiOverviewSchedule
	}
	return hour, minute, nil
}

// Validate reports ErrWikiOverviewSchedule with the field at fault.
func (s WikiOverviewSchedule) Validate() error {
	switch {
	case s.Frequency != WikiOverviewDaily && s.Frequency != WikiOverviewWeekly:
		return fmt.Errorf("%w: frequency must be daily or weekly", ErrWikiOverviewSchedule)
	case s.Weekday < 0 || s.Weekday > 6:
		return fmt.Errorf("%w: weekday must be 0 (Sunday) to 6", ErrWikiOverviewSchedule)
	case s.PeriodDays < 1 || s.PeriodDays > WikiOverviewMaxDays:
		return fmt.Errorf("%w: period_days must be 1 to 31", ErrWikiOverviewSchedule)
	}
	if _, _, err := s.clock(); err != nil {
		return fmt.Errorf("%w: time must be HH:MM", ErrWikiOverviewSchedule)
	}
	return nil
}

// NextRun returns the first scheduled time strictly after after, in loc.
// Dates are built with time.Date, so a wall-clock time keeps its meaning
// across daylight saving changes.
func (s WikiOverviewSchedule) NextRun(after time.Time, loc *time.Location) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	hour, minute, _ := s.clock()
	local := after.In(loc)
	at := func(dayOffset int) time.Time {
		return time.Date(local.Year(), local.Month(), local.Day()+dayOffset, hour, minute, 0, 0, loc)
	}
	offset := 0
	if s.Frequency == WikiOverviewWeekly {
		offset = (s.Weekday - int(local.Weekday()) + 7) % 7
	}
	next := at(offset)
	if !next.After(after) {
		if s.Frequency == WikiOverviewWeekly {
			next = at(offset + 7)
		} else {
			next = at(offset + 1)
		}
	}
	return next, nil
}

// Period returns the first and last day (in loc) of the PeriodDays days that
// end on the day of at.
func (s WikiOverviewSchedule) Period(at time.Time, loc *time.Location) (time.Time, time.Time) {
	local := at.In(loc)
	end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	return end.AddDate(0, 0, 1-s.PeriodDays), end
}

// DueWikiOverview is a wiki whose overview schedule is due.
type DueWikiOverview struct {
	SettingsID  uuid.UUID  `db:"id"`
	WorkspaceID uuid.UUID  `db:"workspace_id"`
	TeamID      *uuid.UUID `db:"team_id"`
	ProjectID   *uuid.UUID `db:"project_id"`
	Actor       *uuid.UUID `db:"overview_actor"`
	NextRunAt   time.Time  `db:"overview_next_run_at"`
	WikiOverviewSchedule
}

// Scope returns the wiki the schedule belongs to.
func (d DueWikiOverview) Scope() WikiScope {
	if d.TeamID != nil {
		return TeamWiki(*d.TeamID)
	}
	if d.ProjectID != nil {
		return ProjectWiki(*d.ProjectID)
	}
	return WikiScope{}
}
