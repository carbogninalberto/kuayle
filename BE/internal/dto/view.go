package dto

import (
	"encoding/json"
	"time"
)

type CreateViewRequest struct {
	Name        string          `json:"name" validate:"required,min=1,max=100"`
	Description *string         `json:"description"`
	Filters     json.RawMessage `json:"filters" validate:"required"`
	IsShared    bool            `json:"is_shared"`
	Icon        *string         `json:"icon"`
	Color       *string         `json:"color"`
}

type UpdateViewRequest struct {
	Name        *string         `json:"name" validate:"omitempty,min=1,max=100"`
	Description *string         `json:"description"`
	Filters     json.RawMessage `json:"filters"`
	IsShared    *bool           `json:"is_shared"`
	Icon        *string         `json:"icon"`
	Color       *string         `json:"color"`
}

type ViewResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	CreatorID   string          `json:"creator_id"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Filters     json.RawMessage `json:"filters"`
	IsShared    bool            `json:"is_shared"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
