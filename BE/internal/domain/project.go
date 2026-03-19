package domain

import (
	"time"

	"github.com/google/uuid"
)

type ProjectStatus string

const (
	ProjectStatusPlanned    ProjectStatus = "planned"
	ProjectStatusInProgress ProjectStatus = "in_progress"
	ProjectStatusCompleted  ProjectStatus = "completed"
	ProjectStatusCancelled  ProjectStatus = "cancelled"
)

type Project struct {
	ID          uuid.UUID     `json:"id" db:"id"`
	WorkspaceID uuid.UUID     `json:"workspace_id" db:"workspace_id"`
	Name        string        `json:"name" db:"name"`
	Description *string       `json:"description" db:"description"`
	Status      ProjectStatus `json:"status" db:"status"`
	LeadID      *uuid.UUID    `json:"lead_id" db:"lead_id"`
	StartDate   *time.Time    `json:"start_date" db:"start_date"`
	TargetDate  *time.Time    `json:"target_date" db:"target_date"`
	SortOrder   float64       `json:"sort_order" db:"sort_order"`
	CreatedAt   time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at" db:"updated_at"`
}

type ProjectMember struct {
	ProjectID uuid.UUID `json:"project_id" db:"project_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
