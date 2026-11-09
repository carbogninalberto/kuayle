package dto

import "time"

type CreateProjectRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=200"`
	Description *string `json:"description"`
	TeamID      *string `json:"team_id" validate:"omitempty,uuid"`
	LeadID      *string `json:"lead_id" validate:"omitempty,uuid"`
	StartDate   *string `json:"start_date"`
	TargetDate  *string `json:"target_date"`
	Color       *string `json:"color" validate:"omitempty,hexcolor|eq="`
	Icon        *string `json:"icon" validate:"omitempty,max=50"`
}

type UpdateProjectRequest struct {
	Name        *string  `json:"name" validate:"omitempty,min=1,max=200"`
	Description *string  `json:"description"`
	Status      *string  `json:"status" validate:"omitempty,oneof=planned in_progress completed cancelled"`
	TeamID      *string  `json:"team_id" validate:"omitempty,uuid"`
	LeadID      *string  `json:"lead_id" validate:"omitempty,uuid"`
	StartDate   *string  `json:"start_date"`
	TargetDate  *string  `json:"target_date"`
	SortOrder   *float64 `json:"sort_order"`
	// Color and Icon: an empty string clears the value.
	Color *string `json:"color" validate:"omitempty,hexcolor|eq="`
	Icon  *string `json:"icon" validate:"omitempty,max=50"`
}

type ProjectResponse struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description *string                  `json:"description"`
	Status      string                   `json:"status"`
	TeamID      *string                  `json:"team_id"`
	LeadID      *string                  `json:"lead_id"`
	StartDate   *time.Time               `json:"start_date"`
	TargetDate  *time.Time               `json:"target_date"`
	SortOrder   float64                  `json:"sort_order"`
	Color       *string                  `json:"color"`
	Icon        *string                  `json:"icon"`
	Progress    *ProjectProgressResponse `json:"progress,omitempty"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

type ProjectProgressResponse struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Cancelled int `json:"cancelled"`
}
