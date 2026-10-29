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
	// Images already downloaded and stored for this message.
	Attachments []NewConversationAttachment
	// Notice, when set without Text, is answered to the linked sender instead
	// of starting a turn (for example a voice note that cannot be transcribed).
	Notice string
	// Callback fields are set for inline button presses instead of Text.
	CallbackID, CallbackData string
	CallbackMessageID        int64
}

// TelegramApprovalButton is a pending approval offered on a delivered reply.
type TelegramApprovalButton struct {
	ID         uuid.UUID `db:"id"`
	Identifier string    `db:"identifier"`
}

// TelegramCallback is an authenticated approval decision from a linked user.
type TelegramCallback struct {
	UserID, ConversationID, ApprovalID uuid.UUID
	Approve                            bool
	TokenEncrypted                     string
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
	// TelegramMessageID is the message streamed while the reply was written;
	// the final delivery replaces it. PreviewText is what it shows now.
	TelegramMessageID *int64 `db:"telegram_message_id"`
	PreviewText       string `db:"preview_text"`

	Approvals []TelegramApprovalButton `db:"-"`
	// SkillDrafts are the skills the reply proposed with propose_skill.
	SkillDrafts []TelegramSkillDraft `db:"-"`
}

// TelegramSkillDraft is a draft skill saved by a delivered reply.
type TelegramSkillDraft struct {
	Name    string `db:"name"`
	TeamKey string `db:"team_key"`
	Version int    `db:"version"`
}
