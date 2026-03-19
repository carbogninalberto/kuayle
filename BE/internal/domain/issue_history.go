package domain

import (
	"time"

	"github.com/google/uuid"
)

type IssueHistory struct {
	ID        uuid.UUID `json:"id" db:"id"`
	IssueID   uuid.UUID `json:"issue_id" db:"issue_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	Field     string    `json:"field" db:"field"`
	OldValue  *string   `json:"old_value" db:"old_value"`
	NewValue  *string   `json:"new_value" db:"new_value"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
