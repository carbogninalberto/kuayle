package dto

type SystemUpdateStatusResponse struct {
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Message string `json:"message,omitempty"`
}

type SystemUpdateStartResponse struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
}
