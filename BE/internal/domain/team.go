package domain

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID             uuid.UUID `json:"id" db:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id" db:"workspace_id"`
	Name           string    `json:"name" db:"name"`
	Key            string    `json:"key" db:"key"`
	Description    *string   `json:"description" db:"description"`
	Color          *string   `json:"color" db:"color"`
	Icon           *string   `json:"icon" db:"icon"`
	EstimateScale  string    `json:"estimate_scale" db:"estimate_scale"`
	TriageEnabled  bool      `json:"triage_enabled" db:"triage_enabled"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

type TeamMember struct {
	TeamID    uuid.UUID `json:"team_id" db:"team_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
