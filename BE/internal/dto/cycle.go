package dto

import "time"

type CreateCycleRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=100"`
	Description *string `json:"description"`
	StartDate   *string `json:"start_date"`
	EndDate     *string `json:"end_date"`
}

type UpdateCycleRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=1,max=100"`
	Description *string `json:"description"`
	Status      *string `json:"status" validate:"omitempty,oneof=upcoming active completed"`
	StartDate   *string `json:"start_date"`
	EndDate     *string `json:"end_date"`
}

type CycleResponse struct {
	ID          string     `json:"id"`
	TeamID      string     `json:"team_id"`
	Name        string     `json:"name"`
	Number      int        `json:"number"`
	Status      string     `json:"status"`
	Description *string    `json:"description"`
	StartDate   *time.Time `json:"start_date"`
	EndDate     *time.Time `json:"end_date"`
	CompletedAt *time.Time `json:"completed_at"`
	Progress    *CycleProgressResponse `json:"progress,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CycleProgressResponse struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Cancelled int `json:"cancelled"`
}
