package dto

import "time"

type CreateIssueRequest struct {
	Title       string   `json:"title" validate:"required,min=1,max=500"`
	Description *string  `json:"description"`
	Status      string   `json:"status" validate:"omitempty,oneof=backlog todo in_progress in_review done cancelled"`
	StatusID    *string  `json:"status_id" validate:"omitempty,uuid"`
	Priority    *int     `json:"priority" validate:"omitempty,min=0,max=4"`
	TeamID      string   `json:"team_id" validate:"required,uuid"`
	ProjectID   *string  `json:"project_id" validate:"omitempty,uuid"`
	AssigneeID  *string  `json:"assignee_id" validate:"omitempty,uuid"`
	AssigneeIDs []string `json:"assignee_ids" validate:"omitempty,dive,uuid"`
	LabelIDs    []string `json:"label_ids" validate:"omitempty,dive,uuid"`
	ParentID    *string  `json:"parent_id" validate:"omitempty,uuid"`
	DueDate     *string  `json:"due_date" validate:"omitempty"`
	CycleID     *string  `json:"cycle_id" validate:"omitempty,uuid"`
}

type CreateSubIssueRequest struct {
	Title       string   `json:"title" validate:"required,min=1,max=500"`
	Description *string  `json:"description"`
	Status      string   `json:"status" validate:"omitempty,oneof=backlog todo in_progress in_review done cancelled"`
	StatusID    *string  `json:"status_id" validate:"omitempty,uuid"`
	Priority    *int     `json:"priority" validate:"omitempty,min=0,max=4"`
	ProjectID   *string  `json:"project_id" validate:"omitempty,uuid"`
	AssigneeID  *string  `json:"assignee_id" validate:"omitempty,uuid"`
	AssigneeIDs []string `json:"assignee_ids" validate:"omitempty,dive,uuid"`
	LabelIDs    []string `json:"label_ids" validate:"omitempty,dive,uuid"`
	DueDate     *string  `json:"due_date" validate:"omitempty"`
	CycleID     *string  `json:"cycle_id" validate:"omitempty,uuid"`
}

type BulkCreateSubIssueRequest struct {
	Issues []CreateSubIssueRequest `json:"issues" validate:"required,min=1,dive"`
}

type DuplicateIssueRequest struct {
	IncludeSubIssues bool `json:"include_sub_issues"`
}

type UpdateIssueRequest struct {
	Title       *string  `json:"title" validate:"omitempty,min=1,max=500"`
	Description *string  `json:"description"`
	Status      *string  `json:"status" validate:"omitempty,oneof=backlog todo in_progress in_review done cancelled"`
	StatusID    *string  `json:"status_id" validate:"omitempty,uuid"`
	Priority    *int     `json:"priority" validate:"omitempty,min=0,max=4"`
	AssigneeID  *string  `json:"assignee_id" validate:"omitempty,uuid"`
	AssigneeIDs []string `json:"assignee_ids" validate:"omitempty,dive,uuid"`
	ProjectID   *string  `json:"project_id" validate:"omitempty,uuid"`
	CycleID     *string  `json:"cycle_id" validate:"omitempty,uuid"`
	LabelIDs    []string `json:"label_ids" validate:"omitempty,dive,uuid"`
	ParentID    *string  `json:"parent_id" validate:"omitempty,uuid"`
	DueDate     *string  `json:"due_date"`
	SortOrder   *float64 `json:"sort_order"`
}

type IssueResponse struct {
	ID              string                        `json:"id"`
	Identifier      string                        `json:"identifier"`
	Title           string                        `json:"title"`
	Description     *string                       `json:"description"`
	Status          string                        `json:"status"`
	StatusID        *string                       `json:"status_id,omitempty"`
	StatusInfo      *StatusInfoResponse           `json:"status_info,omitempty"`
	Priority        int                           `json:"priority"`
	TeamID          string                        `json:"team_id"`
	ProjectID       *string                       `json:"project_id"`
	CycleID         *string                       `json:"cycle_id"`
	CreatorID       string                        `json:"creator_id"`
	AssigneeID      *string                       `json:"assignee_id"`
	ParentID        *string                       `json:"parent_id"`
	DueDate         *time.Time                    `json:"due_date"`
	SortOrder       float64                       `json:"sort_order"`
	Labels          []LabelResponse               `json:"labels,omitempty"`
	Creator         *UserResponse                 `json:"creator,omitempty"`
	Assignee        *UserResponse                 `json:"assignee,omitempty"`
	Assignees       []UserResponse                `json:"assignees,omitempty"`
	Parent          *IssueSummaryResponse         `json:"parent,omitempty"`
	SubIssueCount   *int                          `json:"sub_issue_count,omitempty"`
	SubIssueDone    *int                          `json:"sub_issue_done,omitempty"`
	RelationCounts  *IssueRelationCountsResponse  `json:"relation_counts,omitempty"`
	RelationSummary *IssueRelationSummaryResponse `json:"relation_summary,omitempty"`
	IsSubscribed    bool                          `json:"is_subscribed"`
	CreatedAt       time.Time                     `json:"created_at"`
	UpdatedAt       time.Time                     `json:"updated_at"`
}

type IssueRelationCountsResponse struct {
	Related   int `json:"related"`
	BlockedBy int `json:"blocked_by"`
	Blocking  int `json:"blocking"`
	Duplicate int `json:"duplicate"`
}

type IssueRelationSummaryResponse struct {
	Related   []IssueSummaryResponse `json:"related,omitempty"`
	BlockedBy []IssueSummaryResponse `json:"blocked_by,omitempty"`
	Blocking  []IssueSummaryResponse `json:"blocking,omitempty"`
	Duplicate []IssueSummaryResponse `json:"duplicate,omitempty"`
}

type IssueSummaryResponse struct {
	ID          string              `json:"id"`
	Identifier  string              `json:"identifier"`
	Title       string              `json:"title"`
	Description *string             `json:"description,omitempty"`
	Status      string              `json:"status,omitempty"`
	StatusID    *string             `json:"status_id,omitempty"`
	StatusInfo  *StatusInfoResponse `json:"status_info,omitempty"`
	Priority    int                 `json:"priority,omitempty"`
	Assignee    *UserResponse       `json:"assignee,omitempty"`
}

type IssueHistoryResponse struct {
	ID              string    `json:"id"`
	IssueID         string    `json:"issue_id"`
	UserID          string    `json:"user_id"`
	Field           string    `json:"field"`
	OldValue        *string   `json:"old_value"`
	NewValue        *string   `json:"new_value"`
	OldDisplayValue *string   `json:"old_display_value,omitempty"`
	NewDisplayValue *string   `json:"new_display_value,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type ConvertIssueToProjectResponse struct {
	Project ProjectResponse `json:"project"`
}

type StatusInfoResponse struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Color    *string `json:"color"`
	Position int     `json:"position"`
}

type BulkUpdateIssueRequest struct {
	IssueIDs   []string `json:"issue_ids" validate:"required,min=1,dive,uuid"`
	Status     *string  `json:"status" validate:"omitempty,oneof=backlog todo in_progress in_review done cancelled"`
	StatusID   *string  `json:"status_id" validate:"omitempty,uuid"`
	Priority   *int     `json:"priority" validate:"omitempty,min=0,max=4"`
	AssigneeID *string  `json:"assignee_id" validate:"omitempty,uuid"`
	LabelIDs   []string `json:"label_ids" validate:"omitempty,dive,uuid"`
	ParentID   *string  `json:"parent_id"`
}

type BulkDeleteIssueRequest struct {
	IssueIDs []string `json:"issue_ids" validate:"required,min=1,dive,uuid"`
}

type SubscriptionResponse struct {
	IsSubscribed bool `json:"is_subscribed"`
}

type IssueFilterParams struct {
	PaginationParams
	Status     string `query:"status"`
	Priority   string `query:"priority"`
	AssigneeID string `query:"assignee"`
	CreatorID  string `query:"creator"`
	TeamID     string `query:"team"`
	ProjectID  string `query:"project"`
	CycleID    string `query:"cycle"`
	LabelID    string `query:"label"`
	Search     string `query:"search"`
	DueBefore  string `query:"due_before"`
	DueAfter   string `query:"due_after"`
	Triaged    string `query:"triaged"`
	SubIssues  string `query:"sub_issues"`
	ParentID   string `query:"parent_id"`
	GroupBy    string `query:"group_by" validate:"omitempty,oneof=status priority assignee project"`
	Sort       string `query:"sort" validate:"omitempty,oneof=created_at updated_at priority sort_order status due_date"`
	Order      string `query:"order" validate:"omitempty,oneof=asc desc"`
}
