package domain

import (
	"time"

	"github.com/google/uuid"
)

type Workspace struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Slug      string    `json:"slug" db:"slug"`
	LogoURL   *string   `json:"logo_url" db:"logo_url"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
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
