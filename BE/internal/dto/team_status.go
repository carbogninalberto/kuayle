package dto

import "time"

type CreateTeamStatusRequest struct {
	Name       string   `json:"name" validate:"required,min=1,max=100"`
	Category   string   `json:"category" validate:"required,oneof=backlog unstarted started completed cancelled"`
	Color      *string  `json:"color"`
	ProjectIDs []string `json:"project_ids" validate:"omitempty,dive,uuid"`
}

type UpdateTeamStatusRequest struct {
	Name       *string   `json:"name" validate:"omitempty,min=1,max=100"`
	Color      *string   `json:"color"`
	Position   *int      `json:"position" validate:"omitempty,min=0"`
	ProjectIDs *[]string `json:"project_ids" validate:"omitempty,dive,uuid"`
}

type TeamStatusResponse struct {
	ID         string    `json:"id"`
	TeamID     string    `json:"team_id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	Category   string    `json:"category"`
	Color      *string   `json:"color"`
	Position   int       `json:"position"`
	IsDefault  bool      `json:"is_default"`
	ProjectIDs []string  `json:"project_ids"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
