package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	DevMachineOpStartProcess DevMachineOperationAction = "start_process"
	AssistantProcessTTL                                = 30 * time.Minute
	AssistantProcessQuota                              = 4
)

// AssistantProcessHost is the container's private DNS name. A new process on
// the same port never resolves to a previous process's container.
func AssistantProcessHost(id uuid.UUID) string {
	return "kuayle-process-" + id.String()
}

// AssistantProcessPayload binds approval to the command, working directory
// and app port shown to the owner.
type AssistantProcessPayload struct {
	MachineID        uuid.UUID `json:"machine_id"`
	Machine          string    `json:"machine"`
	Command          string    `json:"command"`
	WorkingDirectory string    `json:"working_directory"`
	Port             int       `json:"port"`
	TTLMinutes       int       `json:"ttl_minutes"`
}

type AssistantProcess struct {
	ID               uuid.UUID  `db:"id"`
	WorkspaceID      uuid.UUID  `db:"workspace_id"`
	MachineID        *uuid.UUID `db:"machine_id"`
	RoutingKey       string     `db:"routing_key"`
	Command          string     `db:"command"`
	WorkingDirectory string     `db:"working_directory"`
	Port             int        `db:"port"`
	Status           string     `db:"status"`
	ExpiresAt        time.Time  `db:"expires_at"`
}
