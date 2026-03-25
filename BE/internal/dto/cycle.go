package dto

import "time"

type CreateCycleRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=100"`
	Description *string `json:"description"`
	Goals       *string `json:"goals"`
	StartDate   string  `json:"start_date" validate:"required"`
	EndDate     string  `json:"end_date" validate:"required"`
}

type UpdateCycleRequest struct {
	Name          *string `json:"name" validate:"omitempty,min=1,max=100"`
	Description   *string `json:"description"`
	Goals         *string `json:"goals"`
	Retrospective *string `json:"retrospective"`
	Status        *string `json:"status" validate:"omitempty,oneof=upcoming active completed"`
	StartDate     *string `json:"start_date"`
	EndDate       *string `json:"end_date"`
}

type CompleteCycleRequest struct {
	Retrospective *string `json:"retrospective"`
	CarryOver     bool    `json:"carry_over"`
}

type CycleResponse struct {
	ID            string     `json:"id"`
	TeamID        string     `json:"team_id"`
	Name          string     `json:"name"`
	Number        int        `json:"number"`
	Status        string     `json:"status"`
	Description   *string    `json:"description"`
	Goals         *string    `json:"goals"`
	Retrospective *string    `json:"retrospective"`
	StartDate     *time.Time `json:"start_date"`
	EndDate       *time.Time `json:"end_date"`
	CompletedAt   *time.Time `json:"completed_at"`
	Progress      *CycleProgressResponse `json:"progress,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CycleProgressResponse struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Cancelled int `json:"cancelled"`
}

type BurndownPoint struct {
	Date      string `json:"date"`
	Scope     int    `json:"scope"`
	Started   int    `json:"started"`
	Completed int    `json:"completed"`
}

type VelocityPoint struct {
	CycleID     string     `json:"cycle_id" db:"cycle_id"`
	CycleName   string     `json:"cycle_name" db:"cycle_name"`
	CycleNumber int        `json:"cycle_number" db:"cycle_number"`
	Scope       int        `json:"scope" db:"scope"`
	Completed   int        `json:"completed" db:"completed"`
	Cancelled   int        `json:"cancelled" db:"cancelled"`
	StartDate   *time.Time `json:"start_date" db:"start_date"`
	EndDate     *time.Time `json:"end_date" db:"end_date"`
}
