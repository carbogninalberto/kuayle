package dto

import (
	"encoding/json"
	"time"
)

type CreateIssueTemplateRequest struct {
	Title             string          `json:"title" validate:"required,min=1,max=500"`
	DefaultIssueTitle *string         `json:"default_issue_title" validate:"omitempty,min=1,max=500"`
	Description       *string         `json:"description"`
	Status            *string         `json:"status" validate:"omitempty,oneof=backlog todo in_progress in_review done cancelled"`
	Priority          *int            `json:"priority" validate:"omitempty,min=0,max=4"`
	TeamID            *string         `json:"team_id" validate:"omitempty,uuid"`
	AssigneeID        *string         `json:"assignee_id" validate:"omitempty,uuid"`
	LabelIDs          []string        `json:"label_ids" validate:"omitempty,dive,uuid"`
	RecurrenceRule    json.RawMessage `json:"recurrence_rule"`
}

type UpdateIssueTemplateRequest struct {
	Title             *string         `json:"title" validate:"omitempty,min=1,max=500"`
	DefaultIssueTitle OptionalString  `json:"default_issue_title"`
	Description       *string         `json:"description"`
	Status            OptionalString  `json:"status"`
	Priority          OptionalInt     `json:"priority"`
	TeamID            OptionalString  `json:"team_id"`
	AssigneeID        OptionalString  `json:"assignee_id"`
	LabelIDs          []string        `json:"label_ids" validate:"omitempty,dive,uuid"`
	RecurrenceRule    json.RawMessage `json:"recurrence_rule"`
	IsActive          *bool           `json:"is_active"`
}

type OptionalInt struct {
	Value *int
	Set   bool
}

func (i *OptionalInt) UnmarshalJSON(data []byte) error {
	i.Set = true
	if string(data) == "null" {
		i.Value = nil
		return nil
	}
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	i.Value = &value
	return nil
}

type IssueTemplateResponse struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	TeamID            *string         `json:"team_id"`
	Title             string          `json:"title"`
	DefaultIssueTitle *string         `json:"default_issue_title"`
	Description       *string         `json:"description"`
	Status            *string         `json:"status"`
	Priority          *int            `json:"priority"`
	AssigneeID        *string         `json:"assignee_id"`
	LabelIDs          json.RawMessage `json:"label_ids"`
	RecurrenceRule    json.RawMessage `json:"recurrence_rule"`
	NextRunAt         *time.Time      `json:"next_run_at"`
	IsActive          bool            `json:"is_active"`
	CreatedBy         string          `json:"created_by"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}
