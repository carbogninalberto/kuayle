package domain

import (
	"time"

	"github.com/google/uuid"
)

// Conversation image limits. Uploads are sniffed; only these raster types are
// accepted, and they are always served with their own type and nosniff.
const (
	ConversationAttachmentMaxBytes     = 10 << 20
	ConversationAttachmentsPerMessage  = 6
	ConversationAttachmentMaxDimension = 16384
	ConversationAttachmentPendingLimit = 24
)

// ConversationAttachmentTypes maps the accepted image types to their extension.
var ConversationAttachmentTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// ConversationAttachment is an image on a user message. Before the message is
// sent it is pending (MessageID nil) and visible only to its uploader.
type ConversationAttachment struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	WorkspaceID    uuid.UUID  `json:"-" db:"workspace_id"`
	ConversationID uuid.UUID  `json:"conversation_id" db:"conversation_id"`
	MessageID      *uuid.UUID `json:"message_id,omitempty" db:"message_id"`
	UploadedBy     uuid.UUID  `json:"-" db:"uploaded_by"`
	StorageKey     string     `json:"-" db:"storage_key"`
	Filename       string     `json:"filename" db:"filename"`
	ContentType    string     `json:"content_type" db:"content_type"`
	Size           int64      `json:"size" db:"size"`
	Width          int        `json:"width" db:"width"`
	Height         int        `json:"height" db:"height"`
	Position       int        `json:"position" db:"position"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// NewConversationAttachment is a stored image not yet recorded in the database.
type NewConversationAttachment struct {
	StorageKey  string
	Filename    string
	ContentType string
	Size        int64
	Width       int
	Height      int
}

// AgentTaskAttachment is an issue image copied into a task attempt's worktree
// at Path (relative to the worktree root) before the agent starts.
type AgentTaskAttachment struct {
	TaskID      uuid.UUID  `json:"-" db:"task_id"`
	RunID       uuid.UUID  `json:"-" db:"run_id"`
	Position    int        `json:"position" db:"position"`
	AssetID     *uuid.UUID `json:"asset_id,omitempty" db:"asset_id"`
	StorageKey  string     `json:"-" db:"storage_key"`
	Path        string     `json:"path" db:"path"`
	ContentType string     `json:"content_type" db:"content_type"`
	Size        int64      `json:"size" db:"size"`
}

// Task attachment bounds: at most this many images and bytes per attempt.
const (
	AgentTaskAttachmentsMax      = 10
	AgentTaskAttachmentsMaxBytes = 50 << 20
)
