package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type SharedLinkScope string

const (
	SharedLinkScopeWorkspace SharedLinkScope = "workspace"
	SharedLinkScopeTeam      SharedLinkScope = "team"
	SharedLinkScopeProject   SharedLinkScope = "project"
	SharedLinkScopeView      SharedLinkScope = "view"
)

type SharedLink struct {
	ID                 uuid.UUID       `json:"id" db:"id"`
	Token              string          `json:"token" db:"token"`
	WorkspaceID        uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	CreatedBy          uuid.UUID       `json:"created_by" db:"created_by"`
	Scope              SharedLinkScope `json:"scope" db:"scope"`
	ScopeID            *uuid.UUID      `json:"scope_id" db:"scope_id"`
	Filters            json.RawMessage `json:"filters" db:"filters"`
	IncludeDescription bool            `json:"include_description" db:"include_description"`
	IsActive           bool            `json:"is_active" db:"is_active"`
	ExpiresAt          *time.Time      `json:"expires_at" db:"expires_at"`
	CreatedAt          time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at" db:"updated_at"`
}
