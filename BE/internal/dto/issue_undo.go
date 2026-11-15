package dto

import "time"

// IssueUndoPrevious holds the values an undo puts back. Only the fields the
// edit changed are present. For project_id, cycle_id and assignee_id an empty
// string means "none".
type IssueUndoPrevious struct {
	StatusID    *string   `json:"status_id,omitempty" validate:"omitempty,uuid"`
	Priority    *int      `json:"priority,omitempty" validate:"omitempty,min=0,max=4"`
	AssigneeID  *string   `json:"assignee_id,omitempty" validate:"omitempty,uuid"`
	AssigneeIDs *[]string `json:"assignee_ids,omitempty" validate:"omitempty,dive,uuid"`
	LabelIDs    *[]string `json:"label_ids,omitempty" validate:"omitempty,dive,uuid"`
	ProjectID   *string   `json:"project_id,omitempty" validate:"omitempty,uuid"`
	CycleID     *string   `json:"cycle_id,omitempty" validate:"omitempty,uuid"`
	SortOrder   *float64  `json:"sort_order,omitempty"`
}

// IssueUndo is returned with an undoable edit and sent back to undo it. The
// undo applies only while the issue is still at ExpectedVersion.
type IssueUndo struct {
	ExpectedVersion int64             `json:"expected_version" validate:"min=1"`
	Previous        IssueUndoPrevious `json:"previous"`
}

// BulkIssueUndoItem is one issue of a bulk undo.
type BulkIssueUndoItem struct {
	ID              string            `json:"id" validate:"required,uuid"`
	ExpectedVersion int64             `json:"expected_version" validate:"min=1"`
	Previous        IssueUndoPrevious `json:"previous"`
}

// BulkIssueUndo undoes a bulk edit as one step: every item or none.
type BulkIssueUndo struct {
	Items []BulkIssueUndoItem `json:"items" validate:"required,min=1,max=5000,dive"`
}

type BulkUpdateIssueResponse struct {
	Updated int `json:"updated"`
	// Undo is present when the edit can be undone with POST /issues/bulk/undo.
	Undo *BulkIssueUndo `json:"undo,omitempty"`
}

type BulkUndoIssueResponse struct {
	Updated int `json:"updated"`
}

type DeleteIssueResponse struct {
	Status string `json:"status"`
	// RestorableUntil is when the deleted issue is purged for good.
	RestorableUntil time.Time `json:"restorable_until"`
}

type BulkDeleteIssueResponse struct {
	Deleted         int       `json:"deleted"`
	RestorableUntil time.Time `json:"restorable_until"`
}

type BulkRestoreIssueRequest struct {
	IssueIDs []string `json:"issue_ids" validate:"required,min=1,max=5000,dive,uuid"`
}

type BulkRestoreIssueResponse struct {
	Restored int `json:"restored"`
}
