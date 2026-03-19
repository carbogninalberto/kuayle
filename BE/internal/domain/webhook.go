package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Webhook struct {
	ID          uuid.UUID      `json:"id" db:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id" db:"workspace_id"`
	URL         string         `json:"url" db:"url"`
	Secret      string         `json:"-" db:"secret"`
	Events      pq.StringArray `json:"events" db:"events"`
	IsActive    bool           `json:"is_active" db:"is_active"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}
