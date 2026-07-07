package dto

import "time"

type AISettingsResponse struct {
	Provider                string    `json:"provider"`
	BaseURL                 string    `json:"base_url"`
	Model                   string    `json:"model"`
	HasAPIKey               bool      `json:"has_api_key"`
	DescriptionExpandPrompt string    `json:"description_expand_prompt"`
	DefaultPrompt           string    `json:"default_prompt"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type UpdateAISettingsRequest struct {
	Provider                *string        `json:"provider" validate:"omitempty,oneof=openai_compatible"`
	BaseURL                 *string        `json:"base_url" validate:"omitempty,max=500"`
	Model                   *string        `json:"model" validate:"omitempty,max=200"`
	APIKey                  OptionalString `json:"api_key"`
	DescriptionExpandPrompt *string        `json:"description_expand_prompt" validate:"omitempty,max=4000"`
}

type ExpandIssueDescriptionResponse struct {
	Description string `json:"description"`
}

type ExpandIssueDescriptionRequest struct {
	SelectedText *string `json:"selected_text" validate:"omitempty,max=8000"`
}
