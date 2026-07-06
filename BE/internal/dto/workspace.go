package dto

import (
	"encoding/json"
	"time"
)

type CreateWorkspaceRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
	Slug string `json:"slug" validate:"required,min=1,max=50,alphanumunicode"`
}

type OptionalString struct {
	Value *string
	Set   bool
}

func (s *OptionalString) UnmarshalJSON(data []byte) error {
	s.Set = true
	if string(data) == "null" {
		s.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.Value = &value
	return nil
}

type UpdateWorkspaceRequest struct {
	Name             *string        `json:"name" validate:"omitempty,min=1,max=100"`
	LogoURL          OptionalString `json:"logo_url"`
	ShareLinkMinRole *string        `json:"share_link_min_role" validate:"omitempty,oneof=owner admin member"`
}

type WorkspaceOwnerResponse struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

type WorkspaceResponse struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	Slug             string                  `json:"slug"`
	LogoURL          *string                 `json:"logo_url"`
	OwnerID          string                  `json:"owner_id"`
	Owner            *WorkspaceOwnerResponse `json:"owner,omitempty"`
	ShareLinkMinRole string                  `json:"share_link_min_role"`
	CurrentUserRole  string                  `json:"current_user_role"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type InviteMemberRequest struct {
	Email string `json:"email" validate:"required,email"`
	Role  string `json:"role" validate:"required,oneof=admin member guest"`
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role" validate:"required,oneof=owner admin member guest"`
}

type WorkspaceMemberResponse struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}
