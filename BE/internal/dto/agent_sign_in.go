package dto

import (
	"time"

	"github.com/google/uuid"
)

// AgentSignInResponse is the current user's sign-in status for one coding
// agent. It never carries the sign-in itself.
type AgentSignInResponse struct {
	Provider string `json:"provider"`
	// SupportsToken is true for agents that accept a pasted token
	// (Claude Code's `claude setup-token`, an OpenCode API key).
	SupportsToken bool                     `json:"supports_token"`
	Token         *AgentSignInTokenStatus  `json:"token"`
	Terminal      *AgentSignInTerminalInfo `json:"terminal"`
}

// AgentSignInTokenStatus describes a saved token. For a Claude Code token
// ExpiresAt is estimated as one year after it was saved; it is null for a
// token without a known expiry (an OpenCode API key). Expired tokens are no
// longer used.
type AgentSignInTokenStatus struct {
	SavedAt   time.Time  `json:"saved_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Expired   bool       `json:"expired"`
}

// AgentSignInTerminalInfo describes a saved credentials file. The terminal
// JSON field also covers Codex credentials saved through Settings.
type AgentSignInTerminalInfo struct {
	SavedAt time.Time `json:"saved_at"`
}

type SaveAgentSignInTokenRequest struct {
	Token string `json:"token"`
}

type CodexDeviceStartResponse struct {
	AttemptID       uuid.UUID `json:"attempt_id"`
	VerificationURL string    `json:"verification_url"`
	UserCode        string    `json:"user_code"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type CodexDeviceAttemptRequest struct {
	AttemptID uuid.UUID `json:"attempt_id"`
}

type CodexDevicePollResponse struct {
	Status string `json:"status"`
}
