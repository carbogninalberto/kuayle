package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CreateImportRequest carries the source configuration and the job-lifetime
// credentials. Credentials are write-only and never echoed in a response.
type CreateImportRequest struct {
	Source      string          `json:"source"`
	Config      json.RawMessage `json:"config"`
	Credentials json.RawMessage `json:"credentials"`
}

type UpdateImportMappingRequest struct {
	Mapping json.RawMessage `json:"mapping"`
}

type ImportJobResponse struct {
	ID              uuid.UUID       `json:"id"`
	Source          string          `json:"source"`
	SourceSite      string          `json:"source_site"`
	Status          string          `json:"status"`
	Config          json.RawMessage `json:"config"`
	Mapping         json.RawMessage `json:"mapping"`
	Preview         json.RawMessage `json:"preview"`
	Total           int             `json:"total"`
	Processed       int             `json:"processed"`
	CreatedCount    int             `json:"created_count"`
	UpdatedCount    int             `json:"updated_count"`
	SkippedCount    int             `json:"skipped_count"`
	FailedCount     int             `json:"failed_count"`
	Error           *string         `json:"error"`
	CancelRequested bool            `json:"cancel_requested"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	StartedAt       *time.Time      `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
}

type ImportJobsResponse struct {
	Imports []ImportJobResponse `json:"imports"`
}

type ImportItemResponse struct {
	ExternalType string    `json:"external_type"`
	ExternalID   string    `json:"external_id"`
	ExternalKey  string    `json:"external_key"`
	Outcome      string    `json:"outcome"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}

type ImportItemsResponse struct {
	Items   []ImportItemResponse `json:"items"`
	HasMore bool                 `json:"has_more"`
}

type ImportPreviewResponse struct {
	Counts         map[string]int        `json:"counts"`
	Statuses       []string              `json:"statuses"`
	Priorities     []string              `json:"priorities"`
	Labels         []string              `json:"labels"`
	Users          []ImportSourceUser    `json:"users"`
	Projects       []ImportSourceProject `json:"projects"`
	UnmappedFields []string              `json:"unmapped_fields"`
}

type ImportSourceUser struct {
	ExternalID  string `json:"external_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type ImportSourceProject struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Key        string `json:"key"`
}

type ImportOptionsResponse struct {
	Teams    []ImportTeamOption                 `json:"teams"`
	Statuses map[uuid.UUID][]ImportStatusOption `json:"statuses"`
	Members  []ImportMemberOption               `json:"members"`
}

type ImportTeamOption struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	IsPrivate bool      `json:"is_private"`
}

type ImportStatusOption struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Category string    `json:"category"`
}

type ImportMemberOption struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}
