package dto

import "github.com/google/uuid"

type RegisterPushSubscriptionRequest struct {
	WorkspaceSlug string     `json:"workspace_slug"`
	Endpoint      string     `json:"endpoint"`
	P256DH        string     `json:"p256dh"`
	Auth          string     `json:"auth"`
	ReplaceID     *uuid.UUID `json:"replace_id,omitempty"`
}
