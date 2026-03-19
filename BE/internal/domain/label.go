package domain

import (
	"time"

	"github.com/google/uuid"
)

type Label struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	Name        string     `json:"name" db:"name"`
	Color       string     `json:"color" db:"color"`
	Description *string    `json:"description" db:"description"`
	ParentID    *uuid.UUID `json:"parent_id" db:"parent_id"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

type IssueLabel struct {
	IssueID uuid.UUID `json:"issue_id" db:"issue_id"`
	LabelID uuid.UUID `json:"label_id" db:"label_id"`
}
