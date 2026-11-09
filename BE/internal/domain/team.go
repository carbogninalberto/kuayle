package domain

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID                       uuid.UUID `json:"id" db:"id"`
	WorkspaceID              uuid.UUID `json:"workspace_id" db:"workspace_id"`
	IsPrivate                bool      `json:"is_private" db:"is_private"`
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

	// IconSet tells Update to write Icon. Without it the stored icon is kept,
	// so an update of other fields never restores an icon it read earlier.
	IconSet bool `json:"-" db:"-"`

	// ReplacedIcon is the icon the last Update replaced, read under the row
	// lock, so the caller can delete the file of an image icon it displaced.
	ReplacedIcon *string `json:"-" db:"replaced_icon"`
}

type TeamMember struct {
	TeamID    uuid.UUID `json:"team_id" db:"team_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
