package domain

import (
	"time"

	"github.com/google/uuid"
)

type Asset struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id" db:"workspace_id"`
	StorageKey  string    `json:"storage_key" db:"storage_key"`
	Filename    string    `json:"filename" db:"filename"`
	ContentType string    `json:"content_type" db:"content_type"`
	Size        int64     `json:"size" db:"size"`
	UploadedBy  uuid.UUID `json:"uploaded_by" db:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
