package domain

import (
	"time"

	"github.com/google/uuid"
)

// AssistantSkill is workspace-authored Markdown guidance for the assistant,
// optionally scoped to a team. Name, description and body are the content in
// effect (CurrentVersion); a draft version above it awaits activation.
type AssistantSkill struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	TeamID      *uuid.UUID `json:"team_id" db:"team_id"`
	TeamKey     *string    `json:"team_key" db:"team_key"`
	TeamName    *string    `json:"team_name" db:"team_name"`
	Name        string     `json:"name" db:"name"`
	Description string     `json:"description" db:"description"`
	// Body is left out of lists.
	Body           string `json:"body,omitempty" db:"body"`
	Enabled        bool   `json:"enabled" db:"enabled"`
	Status         string `json:"status" db:"status"`
	CurrentVersion int    `json:"current_version" db:"current_version"`
	LatestVersion  int    `json:"latest_version" db:"latest_version"`
	// PendingVersion is the newest draft version above CurrentVersion of an
	// active skill: an edit waiting for an owner or admin to activate it.
	PendingVersion  *int       `json:"pending_version" db:"pending_version"`
	CreatedByUserID *uuid.UUID `json:"created_by_user_id" db:"created_by_user_id"`
	AuthorKind      string     `json:"author_kind" db:"author_kind"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// AssistantSkillVersion is one saved edit of a skill.
type AssistantSkillVersion struct {
	SkillID      uuid.UUID  `json:"skill_id" db:"skill_id"`
	Version      int        `json:"version" db:"version"`
	Name         string     `json:"name" db:"name"`
	Description  string     `json:"description" db:"description"`
	Body         string     `json:"body" db:"body"`
	Status       string     `json:"status" db:"status"`
	AuthorKind   string     `json:"author_kind" db:"author_kind"`
	AuthorUserID *uuid.UUID `json:"author_user_id" db:"author_user_id"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
}

// AssistantSkillInput is validated skill content. Status is "draft" or
// "active"; empty keeps the skill's current status on updates.
type AssistantSkillInput struct {
	TeamID      *uuid.UUID
	Name        string
	Description string
	Body        string
	Status      string
	// AuthorKind is "user" for the API and "assistant" for propose_skill.
	AuthorKind string
}

const (
	AssistantSkillDraft  = "draft"
	AssistantSkillActive = "active"
)
