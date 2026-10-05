package dto

import "time"

type CreateInviteLinkRequest struct {
	Role          string `json:"role" validate:"required,oneof=member guest"`
	ExpiresInDays *int   `json:"expires_in_days" validate:"omitempty,min=1,max=365"`
	MaxUses       *int   `json:"max_uses" validate:"omitempty,min=1"`
}

type InviteLinkResponse struct {
	ID        string     `json:"id"`
	Role      string     `json:"role"`
	CreatedBy string     `json:"created_by"`
	ExpiresAt time.Time  `json:"expires_at"`
	MaxUses   *int       `json:"max_uses"`
	UseCount  int        `json:"use_count"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
	// InviteURL carries the raw token and is only populated on creation.
	InviteURL string `json:"invite_url,omitempty"`
}

// Invite link validity statuses returned by the public preview endpoint.
const (
	InviteStatusValid     = "valid"
	InviteStatusExpired   = "expired"
	InviteStatusRevoked   = "revoked"
	InviteStatusExhausted = "exhausted"
)

type InvitePreviewResponse struct {
	WorkspaceName    string  `json:"workspace_name"`
	WorkspaceSlug    string  `json:"workspace_slug"`
	WorkspaceLogoURL *string `json:"workspace_logo_url"`
	Role             string  `json:"role"`
	Status           string  `json:"status"`
}

type AcceptInviteResponse struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	WorkspaceSlug string `json:"workspace_slug"`
	Role          string `json:"role"`
}
