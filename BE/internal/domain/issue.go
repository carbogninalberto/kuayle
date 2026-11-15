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
	ID          uuid.UUID     `json:"id" db:"id"`
	WorkspaceID uuid.UUID     `json:"workspace_id" db:"workspace_id"`
	TeamID      uuid.UUID     `json:"team_id" db:"team_id"`
	ProjectID   *uuid.UUID    `json:"project_id" db:"project_id"`
	CycleID     *uuid.UUID    `json:"cycle_id" db:"cycle_id"`
	Number      int           `json:"number" db:"number"`
	Identifier  string        `json:"identifier" db:"identifier_text"`
	Title       string        `json:"title" db:"title"`
	Description *string       `json:"description" db:"description"`
	Status      IssueStatus   `json:"status" db:"status"`
	Priority    IssuePriority `json:"priority" db:"priority"`
	CreatorID   uuid.UUID     `json:"creator_id" db:"creator_id"`
	AssigneeID  *uuid.UUID    `json:"assignee_id" db:"assignee_id"`
	ParentID    *uuid.UUID    `json:"parent_id" db:"parent_id"`
	DueDate     *time.Time    `json:"due_date" db:"due_date"`
	SortOrder   float64       `json:"sort_order" db:"sort_order"`
	StatusID    *uuid.UUID    `json:"status_id" db:"status_id"`
	Triaged     bool          `json:"triaged" db:"triaged"`
	StartedAt   *time.Time    `json:"started_at" db:"started_at"`
	CompletedAt *time.Time    `json:"completed_at" db:"completed_at"`
	CancelledAt *time.Time    `json:"cancelled_at" db:"cancelled_at"`
	TriagedAt   *time.Time    `json:"triaged_at" db:"triaged_at"`
	CreatedAt   time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at" db:"updated_at"`
	// Version advances on every write to the issue row (including label and
	// assignee changes); undo compares it to detect edits made in between.
	Version int64 `json:"version" db:"version"`
}

type SubIssueCount struct {
	IssueID uuid.UUID `json:"issue_id" db:"issue_id"`
	Total   int       `json:"total" db:"total"`
	Done    int       `json:"done" db:"done"`
}

// IssueUndoPrevious holds the values an undo writes back. Only the fields an
// edit changed are set; the others are left as they are. ProjectSet and
// CycleSet distinguish "put back no project/cycle" from "leave unchanged".
type IssueUndoPrevious struct {
	StatusID    *uuid.UUID
	Priority    *IssuePriority
	AssigneeSet bool
	AssigneeID  *uuid.UUID
	AssigneeIDs *[]uuid.UUID
	LabelIDs    *[]uuid.UUID
	ProjectSet  bool
	ProjectID   *uuid.UUID
	CycleSet    bool
	CycleID     *uuid.UUID
	SortOrder   *float64
}

// Empty reports whether the undo would write nothing.
func (p IssueUndoPrevious) Empty() bool {
	return p.StatusID == nil && p.Priority == nil && !p.AssigneeSet && p.AssigneeIDs == nil &&
		p.LabelIDs == nil && !p.ProjectSet && !p.CycleSet && p.SortOrder == nil
}

// IssueUndo puts one issue back to Previous if its version is still
// ExpectedVersion, the version the edit being undone left behind.
type IssueUndo struct {
	IssueID         uuid.UUID
	ExpectedVersion int64
	Previous        IssueUndoPrevious
}

// DeletedIssue is a soft-deleted issue that may still be restored.
type DeletedIssue struct {
	Issue
	DeletedAt time.Time  `db:"deleted_at"`
	DeletedBy *uuid.UUID `db:"deleted_by"`
}
