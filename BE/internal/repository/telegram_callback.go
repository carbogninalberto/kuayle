package repository

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ReceiveCallback authenticates an inline approval button press. It applies the
// same fail-closed identity checks as messages (secret, enabled bot, private
// chat, allowlist, verified binding, non-guest membership), records the update
// receipt, and returns the decision for the approval's bound actor only. It
// returns nil when the update must be ignored.
func (r *TelegramRepository) ReceiveCallback(ctx context.Context, ws uuid.UUID, secret string, u domain.TelegramUpdate) (*domain.TelegramCallback, error) {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var s domain.TelegramSettings
	err = tx.GetContext(ctx, &s, `SELECT `+telegramColumns+` FROM telegram_settings WHERE workspace_id=$1 FOR NO KEY UPDATE`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTelegramDenied
	}
	if err != nil {
		return nil, err
	}
	if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(s.WebhookSecret)) != 1 {
		return nil, ErrTelegramDenied
	}
	if !s.Enabled || !u.Private || u.ChatID != u.UserID || !slices.Contains(s.AllowedUserIDs, u.UserID) || u.ID < 0 || u.CallbackID == "" {
		return nil, nil
	}
	decision, raw, _ := strings.Cut(u.CallbackData, ":")
	approval, err := uuid.Parse(raw)
	if err != nil || (decision != "approve" && decision != "deny") {
		return nil, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO telegram_updates(workspace_id,bot_id,update_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, ws, s.BotID, u.ID)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return nil, err
	}
	callback := &domain.TelegramCallback{ApprovalID: approval, Approve: decision == "approve", TokenEncrypted: s.TokenEncrypted}
	err = tx.GetContext(ctx, &callback.UserID, `SELECT user_id FROM telegram_links WHERE workspace_id=$1 AND telegram_user_id=$2`, ws, u.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if err = conversationMember(ctx, tx, ws, callback.UserID); err != nil {
		return nil, tx.Commit()
	}
	// Approvals of other users are indistinguishable from missing ones.
	err = tx.GetContext(ctx, &callback.ConversationID, `SELECT a.conversation_id FROM conversation_approvals a JOIN conversations c ON c.id=a.conversation_id WHERE a.id=$1 AND a.actor_id=$2 AND c.workspace_id=$3`, approval, callback.UserID, ws)
	if errors.Is(err, sql.ErrNoRows) {
		callback.ConversationID = uuid.Nil
	} else if err != nil {
		return nil, err
	}
	return callback, tx.Commit()
}
