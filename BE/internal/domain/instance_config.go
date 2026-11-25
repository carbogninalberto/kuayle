package domain

import (
	"time"

	"github.com/google/uuid"
)

// InstanceConfigOverride is a sysadmin's value for an environment variable
// that can change without a restart (instance_config_overrides). It wins over
// the environment until it is reset.
type InstanceConfigOverride struct {
	Name      string     `db:"name"`
	Value     string     `db:"value"`
	UpdatedBy *uuid.UUID `db:"updated_by"`
	UpdatedAt time.Time  `db:"updated_at"`
}
