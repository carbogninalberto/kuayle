package dto

import "time"

type CreateLabelRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=50"`
	Color       string  `json:"color" validate:"required,hexcolor"`
	Description *string `json:"description"`
	ParentID    *string `json:"parent_id" validate:"omitempty,uuid"`
}

type UpdateLabelRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=1,max=50"`
	Color       *string `json:"color" validate:"omitempty,hexcolor"`
	Description *string `json:"description"`
}

type LabelResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description *string   `json:"description"`
	ParentID    *string   `json:"parent_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
