package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ConversationJob struct {
	ConditionMet   bool            `db:"condition_met" json:"-"`
	Timezone       string          `db:"timezone" json:"timezone"`
	ProbeRunID     *uuid.UUID      `db:"probe_run_id" json:"-"`
	ID             uuid.UUID       `db:"id" json:"id"`
	WorkspaceID    uuid.UUID       `db:"workspace_id" json:"-"`
	UserID         uuid.UUID       `db:"user_id" json:"-"`
	ConversationID uuid.UUID       `db:"conversation_id" json:"conversation_id"`
	ParentID       uuid.UUID       `db:"parent_id" json:"parent_id"`
	TeamID         *uuid.UUID      `db:"team_id" json:"-"`
	ProjectID      *uuid.UUID      `db:"project_id" json:"-"`
	IssueID        *uuid.UUID      `db:"issue_id" json:"-"`
	Kind           string          `db:"kind" json:"kind"`
	Spec           json.RawMessage `db:"spec" json:"-"`
	Action         string          `db:"action" json:"action"`
	Instructions   string          `db:"instructions" json:"instructions"`
	State          string          `db:"state" json:"state"`
	NextRunAt      time.Time       `db:"next_run_at" json:"next_run_at"`
	Deadline       time.Time       `db:"deadline" json:"deadline"`
	LeaseToken     *uuid.UUID      `db:"lease_token" json:"-"`
	LeaseUntil     *time.Time      `db:"lease_until" json:"-"`
	Attempts       int             `db:"attempts" json:"-"`
	FireCount      int             `db:"fire_count" json:"-"`
	LastError      string          `db:"last_error" json:"last_error,omitempty"`
	LastOutcome    string          `db:"last_outcome" json:"last_outcome"`
	FiredAt        *time.Time      `db:"fired_at" json:"fired_at,omitempty"`
}
