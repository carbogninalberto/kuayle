package dto

import "time"

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

// AgentSignInTerminalInfo describes a sign-in captured from the terminal.
type AgentSignInTerminalInfo struct {
	SavedAt time.Time `json:"saved_at"`
}

type SaveAgentSignInTokenRequest struct {
	Token string `json:"token"`
}
