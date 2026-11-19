package domain

import (
	"time"

	"github.com/google/uuid"
)

// AgentAutomation delegates matching issues of a team to a coding agent. It
// matches current issue state (a label and/or a status), optionally only during
// one UTC hour or on a timezone-aware cron schedule. Unattended rules start tasks without approval only when the
// workspace policy allows it.
type AgentAutomation struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	WorkspaceID   uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	TeamID        uuid.UUID  `json:"team_id" db:"team_id"`
	OwnerID       uuid.UUID  `json:"owner_id" db:"owner_id"`
	Name          string     `json:"name" db:"name"`
	Enabled       bool       `json:"enabled" db:"enabled"`
	LabelID       *uuid.UUID `json:"label_id,omitempty" db:"label_id"`
	StatusID      *uuid.UUID `json:"status_id,omitempty" db:"status_id"`
	Instructions  string     `json:"instructions" db:"instructions"`
	Unattended    bool       `json:"unattended" db:"unattended"`
	RunHour       *int       `json:"run_hour,omitempty" db:"run_hour"`
	WorkflowID    *uuid.UUID `json:"workflow_id,omitempty" db:"workflow_id"`
	PinnedVersion *int       `json:"pinned_version,omitempty" db:"pinned_version"`
	Cron          string     `json:"cron,omitempty" db:"cron"`
	Timezone      string     `json:"timezone" db:"timezone"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty" db:"next_run_at"`
	LastCheckedAt *time.Time `json:"-" db:"last_checked_at"`
	MaxActive     int        `json:"max_active" db:"max_active"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

type AgentAutomationRun struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	AutomationID uuid.UUID  `json:"automation_id" db:"automation_id"`
	WorkspaceID  uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID      uuid.UUID  `json:"issue_id" db:"issue_id"`
	Status       string     `json:"status" db:"status"`
	ActorID      *uuid.UUID `json:"actor_id,omitempty" db:"actor_id"`
	RequestID    uuid.UUID  `json:"-" db:"request_id"`
	TaskID       *uuid.UUID `json:"task_id,omitempty" db:"task_id"`
	Error        *string    `json:"error,omitempty" db:"error"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// AgentAutomationRunView is a run with what a reviewer needs to decide on it.
type AgentAutomationRunView struct {
	AgentAutomationRun
	WorkflowID      *uuid.UUID `json:"workflow_id,omitempty" db:"workflow_id"`
	PinnedVersion   *int       `json:"pinned_version,omitempty" db:"pinned_version"`
	AutomationName  string     `json:"automation_name" db:"automation_name"`
	IssueIdentifier string     `json:"issue_identifier" db:"issue_identifier"`
	IssueTitle      string     `json:"issue_title" db:"issue_title"`
	OwnerID         uuid.UUID  `json:"owner_id" db:"owner_id"`
	Instructions    string     `json:"-" db:"instructions"`
	TeamID          uuid.UUID  `json:"-" db:"team_id"`
}
