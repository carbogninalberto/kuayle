package dto

import "time"

type CreateProjectRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=200"`
	Description *string `json:"description"`
	LeadID      *string `json:"lead_id" validate:"omitempty,uuid"`
	StartDate   *string `json:"start_date"`
	TargetDate  *string `json:"target_date"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=1,max=200"`
	Description *string `json:"description"`
	Status      *string `json:"status" validate:"omitempty,oneof=planned in_progress completed cancelled"`
	LeadID      *string `json:"lead_id" validate:"omitempty,uuid"`
	StartDate   *string `json:"start_date"`
	TargetDate  *string `json:"target_date"`
	SortOrder   *float64 `json:"sort_order"`
}

type ProjectResponse struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description *string                 `json:"description"`
	Status      string                  `json:"status"`
	LeadID      *string                 `json:"lead_id"`
	StartDate   *time.Time              `json:"start_date"`
	TargetDate  *time.Time              `json:"target_date"`
	SortOrder   float64                 `json:"sort_order"`
	Progress    *ProjectProgressResponse `json:"progress,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

type ProjectProgressResponse struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Cancelled int `json:"cancelled"`
}
