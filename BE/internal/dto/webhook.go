package dto

import "time"

type CreateWebhookRequest struct {
	URL    string   `json:"url" validate:"required,url"`
	Secret string   `json:"secret" validate:"required,min=8"`
	Events []string `json:"events" validate:"required,min=1"`
}

type UpdateWebhookRequest struct {
	URL      *string  `json:"url" validate:"omitempty,url"`
	Events   []string `json:"events"`
	IsActive *bool    `json:"is_active"`
}

type WebhookResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
