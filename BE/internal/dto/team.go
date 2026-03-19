package dto

import "time"

type CreateTeamRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=100"`
	Key         string  `json:"key" validate:"required,min=1,max=10,alpha,uppercase"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
}

type UpdateTeamRequest struct {
	Name          *string `json:"name" validate:"omitempty,min=1,max=100"`
	Description   *string `json:"description"`
	Color         *string `json:"color"`
	Icon          *string `json:"icon"`
	EstimateScale *string `json:"estimate_scale" validate:"omitempty,oneof=linear exponential fibonacci tshirt"`
}

type TeamResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Key           string    `json:"key"`
	Description   *string   `json:"description"`
	Color         *string   `json:"color"`
	Icon          *string   `json:"icon"`
	EstimateScale string    `json:"estimate_scale"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
