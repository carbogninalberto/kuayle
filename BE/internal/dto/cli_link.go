package dto

import (
	"time"

	"github.com/google/uuid"
)

const (
	CLILinkDefaultTTLMinutes = 15
	CLILinkMinTTLMinutes     = 5
	CLILinkMaxTTLMinutes     = 1440
	CLILinkMaxUses           = 10
)

type CreateCLILinkRequest struct {
	Name           string     `json:"name" validate:"omitempty,max=100"`
	Scopes         []string   `json:"scopes" validate:"required,min=1"`
	WorkspaceSlugs []string   `json:"workspace_slugs" validate:"omitempty,max=50"`
	TTLMinutes     int        `json:"ttl_minutes" validate:"omitempty,min=5,max=1440"`
	MaxUses        int        `json:"max_uses" validate:"omitempty,min=1,max=10"`
	TokenExpiresAt *time.Time `json:"token_expires_at"`
}

// CLILinkResponse never includes the plaintext code except in the response
// that creates the link, where it is shown exactly once.
type CLILinkResponse struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Link           string     `json:"link,omitempty"`
	CodePrefix     string     `json:"code_prefix"`
	Scopes         []string   `json:"scopes"`
	WorkspaceSlugs []string   `json:"workspace_slugs"`
	TokenExpiresAt *time.Time `json:"token_expires_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	MaxUses        int        `json:"max_uses"`
	Uses           int        `json:"uses"`
	CreatedAt      time.Time  `json:"created_at"`
}

// RedeemCLILinkRequest is the public body of POST /api/cli/connect. The code
// is never carried in the URL, so servers and proxies do not log it.
type RedeemCLILinkRequest struct {
	Code       string `json:"code" validate:"required,max=200"`
	ClientName string `json:"client_name" validate:"omitempty,max=100"`
}

type RedeemCLILinkResponse struct {
	Token          string     `json:"token"`
	TokenPrefix    string     `json:"token_prefix"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Scopes         []string   `json:"scopes"`
	WorkspaceSlugs []string   `json:"workspace_slugs"`
}
