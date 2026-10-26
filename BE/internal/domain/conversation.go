package domain

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Conversation struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id" db:"workspace_id"`
	CreatorID   uuid.UUID `json:"creator_id" db:"creator_id"`
	Title       string    `json:"title" db:"title"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}
type ConversationMessage struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	ConversationID uuid.UUID  `json:"conversation_id" db:"conversation_id"`
	Sequence       int64      `json:"sequence" db:"sequence"`
	RequestID      uuid.UUID  `json:"request_id" db:"request_id"`
	ParentID       *uuid.UUID `json:"parent_id" db:"parent_id"`
	Role           string     `json:"role" db:"role"`
	Content        string     `json:"content" db:"content"`
	Status         string     `json:"status" db:"status"`
	Provider       string     `json:"provider" db:"provider"`
	Model          string     `json:"model" db:"model"`
	LeaseToken     *uuid.UUID `json:"-" db:"lease_token"`
	LeaseExpiresAt *time.Time `json:"-" db:"lease_expires_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}
type ConversationEvent struct {
	Cursor         int64           `json:"cursor" db:"cursor"`
	ID             uuid.UUID       `json:"id" db:"id"`
	ConversationID uuid.UUID       `json:"conversation_id" db:"conversation_id"`
	Type           string          `json:"type" db:"type"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}
type ConversationHistory struct {
	Conversation Conversation          `json:"conversation"`
	Messages     []ConversationMessage `json:"messages"`
	Cursor       int64                 `json:"cursor"`
	HasMore      bool                  `json:"has_more"`
}
type ConversationAttempt struct {
	Conversation Conversation
	Message      ConversationMessage
}
