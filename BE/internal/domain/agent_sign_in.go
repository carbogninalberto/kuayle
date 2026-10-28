package domain

import "time"

// Kinds of a coding agent's own sign-in.
const (
	// AgentSignInCredentials is the agent's credentials file captured from
	// the user's terminal (Claude Code or Codex).
	AgentSignInCredentials = "credentials"
	// AgentSignInToken is a Claude Code token from `claude setup-token`.
	AgentSignInToken = "token"
)

// AgentSignIn describes a stored sign-in without its value.
type AgentSignIn struct {
	Provider  string     `db:"provider"`
	Kind      string     `db:"kind"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	ExpiresAt *time.Time `db:"expires_at"`
}
