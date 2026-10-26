package dto

import "time"

type SystemUpdateStatusResponse struct {
	Enabled    bool       `json:"enabled"`
	Running    bool       `json:"running"`
	Message    string     `json:"message,omitempty"`
	State      string     `json:"state,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	Output     string     `json:"output,omitempty"`
}

type SystemUpdateStartResponse struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
}

// SystemUpdateActivityResponse is public: it reveals only whether an update runs.
type SystemUpdateActivityResponse struct {
	Running bool `json:"running"`
}
