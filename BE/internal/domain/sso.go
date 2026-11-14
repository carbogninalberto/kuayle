package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ProviderOIDC is the user_identities provider of an OpenID Connect sign-in.
const ProviderOIDC = "oidc"

// SSOConfig is a workspace's OpenID Connect configuration. The client secret
// is stored encrypted and is never part of a response.
type SSOConfig struct {
	WorkspaceID           uuid.UUID      `json:"workspace_id" db:"workspace_id"`
	IssuerURL             string         `json:"issuer_url" db:"issuer_url"`
	ClientID              string         `json:"client_id" db:"client_id"`
	ClientSecretEncrypted string         `json:"-" db:"client_secret_encrypted"`
	AllowedDomains        pq.StringArray `json:"allowed_domains" db:"allowed_domains"`
	ProviderName          string         `json:"provider_name" db:"provider_name"`
	DefaultRole           string         `json:"default_role" db:"default_role"`
	RequireSSO            bool           `json:"require_sso" db:"require_sso"`
	Enabled               bool           `json:"enabled" db:"enabled"`
	CreatedAt             time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at" db:"updated_at"`
}

// UserIdentity links a user to an external IdP subject in one workspace.
// Identities are unique per (workspace, issuer, subject), so the same subject
// in another workspace is a different identity.
type UserIdentity struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	UserID      uuid.UUID  `json:"user_id" db:"user_id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	Provider    string     `json:"provider" db:"provider"`
	Issuer      string     `json:"issuer" db:"issuer"`
	Subject     string     `json:"subject" db:"subject"`
	Email       string     `json:"email" db:"email"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at" db:"last_login_at"`
}

// SSOLoginState is the server-side half of an authorization-code flow. It is
// consumed once by state hash; the PKCE verifier is encrypted at rest.
type SSOLoginState struct {
	StateHash             string    `db:"state_hash"`
	WorkspaceID           uuid.UUID `db:"workspace_id"`
	Nonce                 string    `db:"nonce"`
	CodeVerifierEncrypted string    `db:"code_verifier_encrypted"`
	BindHash              string    `db:"bind_hash"`
	RedirectAfter         string    `db:"redirect_after"`
	ExpiresAt             time.Time `db:"expires_at"`
	CreatedAt             time.Time `db:"created_at"`
}

// SSOWorkspaceRole is one workspace a user belongs to, as needed by the
// require-SSO sign-in check.
type SSOWorkspaceRole struct {
	WorkspaceID uuid.UUID `db:"workspace_id" db:"workspace_id"`
	Slug        string    `db:"slug" db:"slug"`
	Role        string    `db:"role" db:"role"`
}
