package domain

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	UserID       uuid.UUID  `json:"user_id" db:"user_id"`
	WorkspaceID  uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID      *uuid.UUID `json:"issue_id" db:"issue_id"`
	Type         string     `json:"type" db:"type"`
	Title        string     `json:"title" db:"title"`
	ReadAt       *time.Time `json:"read_at" db:"read_at"`
	SnoozedUntil *time.Time `json:"snoozed_until" db:"snoozed_until"`
	ArchivedAt   *time.Time `json:"archived_at" db:"archived_at"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
}
