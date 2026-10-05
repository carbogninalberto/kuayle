package dto

import (
	"time"

	"github.com/google/uuid"
)

type CreateTokenRequest struct {
	Name           string     `json:"name" validate:"required,min=1,max=100"`
	Scopes         []string   `json:"scopes" validate:"required,min=1"`
	WorkspaceSlugs []string   `json:"workspace_slugs" validate:"omitempty,max=50"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

// PersonalAccessTokenResponse never includes the token hash. Token (plaintext) is set only
// in the create response — it is shown exactly once.
type PersonalAccessTokenResponse struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Token          string     `json:"token,omitempty"`
	TokenPrefix    string     `json:"token_prefix"`
	Scopes         []string   `json:"scopes"`
	WorkspaceSlugs []string   `json:"workspace_slugs"`
	ExpiresAt      *time.Time `json:"expires_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	CreatedAt      time.Time  `json:"created_at"`
}
