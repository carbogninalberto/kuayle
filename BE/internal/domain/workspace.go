package domain

import (
	"time"

	"github.com/google/uuid"
)

type Workspace struct {
	ID               uuid.UUID `json:"id" db:"id"`
	Name             string    `json:"name" db:"name"`
	Slug             string    `json:"slug" db:"slug"`
	LogoURL          *string   `json:"logo_url" db:"logo_url"`
	OwnerID          uuid.UUID `json:"owner_id" db:"owner_id"`
	ShareLinkMinRole string    `json:"share_link_min_role" db:"share_link_min_role"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

type WorkspaceMember struct {
	WorkspaceID uuid.UUID `json:"workspace_id" db:"workspace_id"`
	UserID      uuid.UUID `json:"user_id" db:"user_id"`
	Role        string    `json:"role" db:"role"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type WorkspaceMemberWithUser struct {
	WorkspaceID uuid.UUID `db:"workspace_id"`
	UserID      uuid.UUID `db:"user_id"`
	Role        string    `db:"role"`
	Email       string    `db:"email"`
	Name        string    `db:"name"`
	DisplayName string    `db:"display_name"`
	AvatarURL   *string   `db:"avatar_url"`
	CreatedAt   time.Time `db:"created_at"`
}

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleGuest  = "guest"
)

// WorkspaceInviteLink is a shareable invite link. Only the SHA-256 hash of the
// token is stored; the raw token appears solely in the generated invite URL.
// Link roles are limited to member/guest — privileged roles stay on the
// email-invite path where the invitee is a confirmed existing account.
type WorkspaceInviteLink struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	TokenHash   string     `json:"-" db:"token_hash"`
	Role        string     `json:"role" db:"role"`
	CreatedBy   uuid.UUID  `json:"created_by" db:"created_by"`
	ExpiresAt   time.Time  `json:"expires_at" db:"expires_at"`
	MaxUses     *int       `json:"max_uses" db:"max_uses"`
	UseCount    int        `json:"use_count" db:"use_count"`
	RevokedAt   *time.Time `json:"revoked_at" db:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}
