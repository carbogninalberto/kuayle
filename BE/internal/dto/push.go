package dto

import "github.com/google/uuid"

type RegisterPushSubscriptionRequest struct {
	WorkspaceSlug string     `json:"workspace_slug"`
	Endpoint      string     `json:"endpoint"`
	P256DH        string     `json:"p256dh"`
	Auth          string     `json:"auth"`
	ReplaceID     *uuid.UUID `json:"replace_id,omitempty"`
}

type PushSettings struct {
	Source               string `json:"source"`
	PublicKey            string `json:"public_key"`
	Subject              string `json:"subject"`
	Enabled              bool   `json:"enabled"`
	ReadOnly             bool   `json:"read_only"`
	SubscribedDevices    int    `json:"subscribed_devices"`
	RequiresReenrollment bool   `json:"requires_reenrollment,omitempty"`
}

type PushTestResult struct {
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
}
