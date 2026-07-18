package dto

import (
	"encoding/json"
	"time"
)

type CreateSharedLinkRequest struct {
	Scope              string          `json:"scope" validate:"required,oneof=workspace team project view"`
	ScopeID            *string         `json:"scope_id" validate:"omitempty,uuid"`
	Filters            json.RawMessage `json:"filters"`
	IncludeDescription bool            `json:"include_description"`
	ExpiresAt          *string         `json:"expires_at" validate:"omitempty"`
}

type UpdateSharedLinkRequest struct {
	IsActive           *bool   `json:"is_active"`
	IncludeDescription *bool   `json:"include_description"`
	ExpiresAt          *string `json:"expires_at"`
}

type SharedLinkResponse struct {
	ID                 string          `json:"id"`
	Token              string          `json:"token"`
	WorkspaceID        string          `json:"workspace_id"`
	CreatedBy          string          `json:"created_by"`
	Scope              string          `json:"scope"`
	ScopeID            *string         `json:"scope_id,omitempty"`
	Filters            json.RawMessage `json:"filters"`
	IncludeDescription bool            `json:"include_description"`
	IsActive           bool            `json:"is_active"`
	ExpiresAt          *time.Time      `json:"expires_at,omitempty"`
	URL                string          `json:"url"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type PublicShareMetaResponse struct {
	Scope         string                 `json:"scope"`
	ScopeID       *string                `json:"scope_id,omitempty"`
	ScopeName     string                 `json:"scope_name"`
	WorkspaceName string                 `json:"workspace_name"`
	Filters       json.RawMessage        `json:"filters"`
	Statuses      []PublicStatusResponse `json:"statuses,omitempty"`
}

type PublicStatusResponse struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Color    *string `json:"color"`
	Position int     `json:"position"`
}

type PublicIssueResponse struct {
	Identifier  string               `json:"identifier"`
	Title       string               `json:"title"`
	Description *string              `json:"description,omitempty"`
	Status      string               `json:"status"`
	StatusInfo  *StatusInfoResponse  `json:"status_info,omitempty"`
	Priority    int                  `json:"priority"`
	Labels      []LabelResponse      `json:"labels,omitempty"`
	Assignees   []PublicUserResponse `json:"assignees,omitempty"`
	DueDate     *time.Time           `json:"due_date,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type PublicUserResponse struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}
