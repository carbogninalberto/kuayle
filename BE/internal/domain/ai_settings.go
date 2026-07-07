package domain

import (
	"time"

	"github.com/google/uuid"
)

const AIProviderOpenAICompatible = "openai_compatible"

type AISettings struct {
	WorkspaceID             uuid.UUID `json:"workspace_id" db:"workspace_id"`
	Provider                string    `json:"provider" db:"provider"`
	BaseURL                 string    `json:"base_url" db:"base_url"`
	Model                   string    `json:"model" db:"model"`
	APIKeyEncrypted         *string   `json:"-" db:"api_key_encrypted"`
	DescriptionExpandPrompt string    `json:"description_expand_prompt" db:"description_expand_prompt"`
	CreatedAt               time.Time `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time `json:"updated_at" db:"updated_at"`
}
