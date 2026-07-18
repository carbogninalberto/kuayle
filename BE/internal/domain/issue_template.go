package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type IssueTemplate struct {
	ID             uuid.UUID        `json:"id" db:"id"`
	WorkspaceID    uuid.UUID        `json:"workspace_id" db:"workspace_id"`
	TeamID         *uuid.UUID       `json:"team_id" db:"team_id"`
	Title          string           `json:"title" db:"title"`
	Description    *string          `json:"description" db:"description"`
	Status         *string          `json:"status" db:"status"`
	Priority       *int             `json:"priority" db:"priority"`
	AssigneeID     *uuid.UUID       `json:"assignee_id" db:"assignee_id"`
	LabelIDs       json.RawMessage  `json:"label_ids" db:"label_ids"`
	RecurrenceRule *json.RawMessage `json:"recurrence_rule" db:"recurrence_rule"`
	NextRunAt      *time.Time       `json:"next_run_at" db:"next_run_at"`
	IsActive       bool             `json:"is_active" db:"is_active"`
	CreatedBy      uuid.UUID        `json:"created_by" db:"created_by"`
	CreatedAt      time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at" db:"updated_at"`
}
