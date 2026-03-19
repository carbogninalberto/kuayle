package domain

import (
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID        uuid.UUID `json:"id" db:"id"`
	IssueID   uuid.UUID `json:"issue_id" db:"issue_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	Body      string    `json:"body" db:"body"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
