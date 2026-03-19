package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type View struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	CreatorID   uuid.UUID       `json:"creator_id" db:"creator_id"`
	Name        string          `json:"name" db:"name"`
	Description *string         `json:"description" db:"description"`
	Filters     json.RawMessage `json:"filters" db:"filters"`
	IsShared    bool            `json:"is_shared" db:"is_shared"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}
