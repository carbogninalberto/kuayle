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
	ID            string  `json:"id"`
	TeamID        *string `json:"team_id"`
	ProjectID     *string `json:"project_id"`
	Title         string  `json:"title"`
	Version       int     `json:"version"`
	UpdatedBy     *string `json:"updated_by"`
	UpdatedByName *string `json:"updated_by_name"`
	// RegistryDate is the UTC day (YYYY-MM-DD) a registry page logs.
	RegistryDate *string   `json:"registry_date"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type WikiPageResponse struct {
	WikiPageSummaryResponse
	Content   string  `json:"content"`
	CreatedBy *string `json:"created_by"`
}

// WikiReworkRequest asks the AI model to rewrite selected text of a saved
// page; the page itself is read from storage.
type WikiReworkRequest struct {
	SelectedText string `json:"selected_text" validate:"required,max=8000"`
}

type WikiRevisionResponse struct {
	ID           string    `json:"id"`
	Version      int       `json:"version"`
	OldTitle     string    `json:"old_title"`
	NewTitle     string    `json:"new_title"`
	OldContent   string    `json:"old_content"`
	NewContent   string    `json:"new_content"`
	EditedBy     *string   `json:"edited_by"`
	EditedByName *string   `json:"edited_by_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// WikiSettingsRequest turns a wiki's opt-in features on or off.
type WikiSettingsRequest struct {
	RegistryEnabled *bool `json:"registry_enabled"`
}

type WikiSettingsResponse struct {
	RegistryEnabled bool `json:"registry_enabled"`
}

// WikiRegistryRequest is one plain-text entry for today's registry page.
type WikiRegistryRequest struct {
	Text string `json:"text"`
}

// WikiRegistryDay is one day of a registry feed: that day's page, whose
// content holds one <p><strong>HH:MM</strong> text</p> per entry.
type WikiRegistryDay struct {
	Date      string    `json:"date"`
	PageID    string    `json:"page_id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WikiRegistryFeedResponse struct {
	Days       []WikiRegistryDay `json:"days"`
	NextBefore *string           `json:"next_before"`
}
