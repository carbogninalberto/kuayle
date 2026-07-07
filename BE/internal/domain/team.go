package domain

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID                       uuid.UUID `json:"id" db:"id"`
	WorkspaceID              uuid.UUID `json:"workspace_id" db:"workspace_id"`
	Name                     string    `json:"name" db:"name"`
	Key                      string    `json:"key" db:"key"`
	Description              *string   `json:"description" db:"description"`
	Color                    *string   `json:"color" db:"color"`
	Icon                     *string   `json:"icon" db:"icon"`
	TriageEnabled            bool      `json:"triage_enabled" db:"triage_enabled"`
	ParentAutoCloseEnabled   bool      `json:"parent_auto_close_enabled" db:"parent_auto_close_enabled"`
	SubIssueAutoCloseEnabled bool      `json:"sub_issue_auto_close_enabled" db:"sub_issue_auto_close_enabled"`
	IssueCopyPrompt          *string   `json:"issue_copy_prompt" db:"issue_copy_prompt"`
	CreatedAt                time.Time `json:"created_at" db:"created_at"`
	UpdatedAt                time.Time `json:"updated_at" db:"updated_at"`
}

type TeamMember struct {
	TeamID    uuid.UUID `json:"team_id" db:"team_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
