package domain

import "github.com/google/uuid"

// RunCheckPayload binds a run_check approval to the task attempt the user saw
// and the exact command shown to them. A nil Command runs the task's check.
type RunCheckPayload struct {
	Identifier string    `json:"identifier"`
	Title      string    `json:"title"`
	TaskID     uuid.UUID `json:"task_id"`
	Generation int64     `json:"generation"`
	Command    []string  `json:"command,omitempty"`
}
