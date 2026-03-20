package domain

import (
	"time"

	"github.com/google/uuid"
)

type Favorite struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id" db:"workspace_id"`
	UserID      uuid.UUID `json:"user_id" db:"user_id"`
	EntityType  string    `json:"entity_type" db:"entity_type"`
	EntityID    uuid.UUID `json:"entity_id" db:"entity_id"`
	Position    int       `json:"position" db:"position"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
