package dto

// SSOConfigRequest configures OIDC for one workspace. ClientSecret is
// write-only: omitting it keeps the stored secret.
type SSOConfigRequest struct {
	IssuerURL      string   `json:"issuer_url" validate:"required,url,max=2048"`
	ClientID       string   `json:"client_id" validate:"required,max=255"`
	ClientSecret   *string  `json:"client_secret" validate:"omitempty,max=1024"`
	AllowedDomains []string `json:"allowed_domains" validate:"omitempty,max=100,dive,required,max=255"`
	DefaultRole    string   `json:"default_role" validate:"required,oneof=member guest"`
	RequireSSO     bool     `json:"require_sso"`
	Enabled        bool     `json:"enabled"`
	ProviderName   string   `json:"provider_name" validate:"omitempty,max=100"`
}

// SSOConfigResponse never contains the client secret.
type SSOConfigResponse struct {
	IssuerURL         string   `json:"issuer_url"`
	ClientID          string   `json:"client_id"`
	HasClientSecret   bool     `json:"has_client_secret"`
	AllowedDomains    []string `json:"allowed_domains"`
	DefaultRole       string   `json:"default_role"`
	RequireSSO        bool     `json:"require_sso"`
	Enabled           bool     `json:"enabled"`
	ProviderName      string   `json:"provider_name"`
	ManagedByInstance bool     `json:"managed_by_instance"`
	RedirectURI       string   `json:"redirect_uri"`
}

// SSOPublicInfo is the unauthenticated sign-in hint for a workspace.
type SSOPublicInfo struct {
	Enabled      bool   `json:"enabled"`
	ProviderName string `json:"provider_name"`
}
