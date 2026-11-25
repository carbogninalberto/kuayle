package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// InstanceConfigOverride is a sysadmin's value for an environment variable
// that can change without a restart (instance_config_overrides). It wins over
// the environment until it is reset.
type InstanceConfigOverride struct {
	Name      string     `db:"name"`
	Value     string     `db:"value"`
	UpdatedBy *uuid.UUID `db:"updated_by"`
	UpdatedAt time.Time  `db:"updated_at"`
}

// Statuses of an InstanceRestartRun.
const (
	// RestartRunRestarting: saved; the API is about to exit.
	RestartRunRestarting = "restarting"
	// RestartRunApplying: the API process started with the new values and has
	// not reported healthy yet.
	RestartRunApplying = "applying"
	// RestartRunConfirmed: the API reported healthy with the new values.
	RestartRunConfirmed = "confirmed"
	// RestartRunRolledBack: the previous overrides are back.
	RestartRunRolledBack = "rolled_back"
)

// InstanceRestartChange is one variable of a run, with its effective value
// before and after. Restart-editable variables are never secrets.
type InstanceRestartChange struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// InstanceRestartRun is one "Apply and restart" of restart-only settings
// (instance_config_restart_runs): the audit trail and the data a rollback
// needs.
type InstanceRestartRun struct {
	ID          uuid.UUID
	Status      string
	RequestedBy *uuid.UUID
	RequestedAt time.Time
	Changes     []InstanceRestartChange
	// Previous holds the override of each changed variable before the run; a
	// nil value means there was none.
	Previous     map[string]*string
	BootAttempts int
	AppliedAt    *time.Time
	FinishedAt   *time.Time
	Failure      string
}

// Active reports whether the run still waits for its outcome.
func (r InstanceRestartRun) Active() bool {
	return r.Status == RestartRunRestarting || r.Status == RestartRunApplying
}

// ErrRestartRunActive is returned when a run already waits for its outcome;
// only one runs at a time.
var ErrRestartRunActive = errors.New("an apply and restart is already in progress")
