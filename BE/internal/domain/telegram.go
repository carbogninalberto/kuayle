package domain

import (
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type TelegramSettings struct {
	WorkspaceID    uuid.UUID     `db:"workspace_id" json:"-"`
	BotID          int64         `db:"bot_id" json:"bot_id"`
	BotUsername    string        `db:"bot_username" json:"bot_username"`
	TokenEncrypted string        `db:"token_encrypted" json:"-"`
	WebhookSecret  string        `db:"webhook_secret" json:"-"`
	Enabled        bool          `db:"enabled" json:"enabled"`
	AllowedUserIDs pq.Int64Array `db:"allowed_user_ids" json:"allowed_user_ids"`
	Status         string        `db:"status" json:"status"`
}

// TelegramUpdate is normalized only from private, non-bot message.from identities.
// Unsupported updates (including callbacks until approvals are implemented) are ignored.
type TelegramUpdate struct {
	ID, UserID, ChatID int64
	Text               string
	Private            bool
}
type TelegramDelivery struct {
	ID             uuid.UUID  `db:"id"`
	WorkspaceID    uuid.UUID  `db:"workspace_id"`
	UserID         uuid.UUID  `db:"user_id"`
	TelegramUserID int64      `db:"telegram_user_id"`
	ConversationID *uuid.UUID `db:"conversation_id"`
	MessageID      *uuid.UUID `db:"message_id"`
	Text           string     `db:"text"`
	TokenEncrypted string     `db:"token_encrypted"`
	Slug           string     `db:"slug"`
}
