package domain

import (
	"time"

	"github.com/google/uuid"
)

type IssueStatus string

const (
	IssueStatusBacklog    IssueStatus = "backlog"
	IssueStatusTodo       IssueStatus = "todo"
	IssueStatusInProgress IssueStatus = "in_progress"
	IssueStatusInReview   IssueStatus = "in_review"
	IssueStatusDone       IssueStatus = "done"
	IssueStatusCancelled  IssueStatus = "cancelled"
)

type IssuePriority int

const (
	PriorityNone   IssuePriority = 0
	PriorityUrgent IssuePriority = 1
	PriorityHigh   IssuePriority = 2
	PriorityMedium IssuePriority = 3
	PriorityLow    IssuePriority = 4
)

type Issue struct {
	ID             uuid.UUID     `json:"id" db:"id"`
	WorkspaceID    uuid.UUID     `json:"workspace_id" db:"workspace_id"`
	TeamID         uuid.UUID     `json:"team_id" db:"team_id"`
	ProjectID      *uuid.UUID    `json:"project_id" db:"project_id"`
	CycleID        *uuid.UUID    `json:"cycle_id" db:"cycle_id"`
	Number         int           `json:"number" db:"number"`
	Identifier     string        `json:"identifier" db:"identifier_text"`
	Title          string        `json:"title" db:"title"`
	Description    *string       `json:"description" db:"description"`
	Status         IssueStatus   `json:"status" db:"status"`
	Priority       IssuePriority `json:"priority" db:"priority"`
	CreatorID      uuid.UUID     `json:"creator_id" db:"creator_id"`
	AssigneeID     *uuid.UUID    `json:"assignee_id" db:"assignee_id"`
	ParentID       *uuid.UUID    `json:"parent_id" db:"parent_id"`
	DueDate        *time.Time    `json:"due_date" db:"due_date"`
	SortOrder      float64       `json:"sort_order" db:"sort_order"`
	StatusID       *uuid.UUID    `json:"status_id" db:"status_id"`
	Triaged        bool          `json:"triaged" db:"triaged"`
	CreatedAt      time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at" db:"updated_at"`
}
