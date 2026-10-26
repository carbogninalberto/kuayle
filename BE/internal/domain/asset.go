package domain

import (
	"time"

	"github.com/google/uuid"
)

type Asset struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	TeamID      *uuid.UUID `json:"team_id" db:"team_id"`
	StorageKey  string     `json:"storage_key" db:"storage_key"`
	Filename    string     `json:"filename" db:"filename"`
	ContentType string     `json:"content_type" db:"content_type"`
	Size        int64      `json:"size" db:"size"`
	UploadedBy  uuid.UUID  `json:"uploaded_by" db:"uploaded_by"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// AssetContentTypeExtensions lists the content types accepted for uploaded
// assets and the file extensions allowed for each.
var AssetContentTypeExtensions = map[string]map[string]bool{
	"image/jpeg":    {".jpg": true, ".jpeg": true},
	"image/png":     {".png": true},
	"image/gif":     {".gif": true},
	"image/webp":    {".webp": true},
	"image/svg+xml": {".svg": true},

	"application/pdf":  {".pdf": true},
	"text/plain":       {".txt": true, ".log": true},
	"text/csv":         {".csv": true},
	"text/markdown":    {".md": true, ".markdown": true},
	"application/json": {".json": true},
	"text/rtf":         {".rtf": true},

	"application/msword": {".doc": true},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {".docx": true},
	"application/vnd.ms-excel": {".xls": true},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {".xlsx": true},
	"application/vnd.ms-powerpoint":                                             {".ppt": true},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {".pptx": true},

	"application/zip":             {".zip": true},
	"application/gzip":            {".gz": true, ".gzip": true},
	"application/x-tar":           {".tar": true},
	"application/x-7z-compressed": {".7z": true},
	"application/vnd.rar":         {".rar": true},
}

// SafeAssetContentType returns the stored type when it is an accepted upload
// type. Any other type, including one restored from a workspace import, is
// served as an opaque download so it cannot run as script or markup.
func SafeAssetContentType(contentType string) string {
	if _, ok := AssetContentTypeExtensions[contentType]; ok {
		return contentType
	}
	return "application/octet-stream"
}
