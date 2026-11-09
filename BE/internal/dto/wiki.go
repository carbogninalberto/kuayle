package dto

import "time"

// Content is editor HTML (or Markdown, which is converted) and is sanitized
// by the service. Size limits are enforced there in bytes.
type CreateWikiPageRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// UpdateWikiPageRequest replaces the fields that are present. Version is the
// version the editor started from; a stale version is rejected with 409.
type UpdateWikiPageRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	Version int     `json:"version" validate:"required,min=1"`
}

type WikiPageSummaryResponse struct {
	ID            string    `json:"id"`
	TeamID        *string   `json:"team_id"`
	ProjectID     *string   `json:"project_id"`
	Title         string    `json:"title"`
	Version       int       `json:"version"`
	UpdatedBy     *string   `json:"updated_by"`
	UpdatedByName *string   `json:"updated_by_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type WikiPageResponse struct {
	WikiPageSummaryResponse
	Content   string  `json:"content"`
	CreatedBy *string `json:"created_by"`
}
