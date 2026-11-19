package domain

import (
	"time"

	"github.com/google/uuid"
)

type PushSubscription struct {
	ID            uuid.UUID  `db:"id"`
	UserID        uuid.UUID  `db:"user_id"`
	WorkspaceID   uuid.UUID  `db:"workspace_id"`
	Endpoint      string     `db:"endpoint"`
	P256DH        string     `db:"p256dh"`
	Auth          string     `db:"auth"`
	UserAgent     string     `db:"user_agent"`
	CreatedAt     time.Time  `db:"created_at"`
	LastSuccessAt *time.Time `db:"last_success_at"`
	FailureCount  int        `db:"failure_count"`
	SSOScoped     bool       `db:"sso_scoped"`
}
