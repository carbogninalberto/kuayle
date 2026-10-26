package repository

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrTelegramDenied = errors.New("Telegram access denied")
var ErrTelegramBusy = errors.New("Telegram settings update is already in progress")
var ErrTelegramInvalid = errors.New("invalid Telegram settings or account link")

const telegramColumns = `workspace_id,bot_id,bot_username,token_encrypted,webhook_secret,enabled,allowed_user_ids,status`

type TelegramRepository struct{ db *sqlx.DB }

func NewTelegramRepository(db *sqlx.DB) *TelegramRepository { return &TelegramRepository{db} }
func (r *TelegramRepository) Get(ctx context.Context, ws uuid.UUID) (*domain.TelegramSettings, error) {
	var s domain.TelegramSettings
	err := r.db.GetContext(ctx, &s, `SELECT `+telegramColumns+` FROM telegram_settings WHERE workspace_id=$1`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}
func telegramOwner(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID) error {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return ErrTelegramDenied
	}
	var owner uuid.UUID
	if err = tx.GetContext(ctx, &owner, `SELECT owner_id FROM workspaces WHERE id=$1 FOR SHARE`, ws); err != nil {
		return err
	}
	if owner != user {
		return ErrTelegramDenied
	}
	return conversationMember(ctx, tx, ws, user)
}
func (r *TelegramRepository) Save(ctx context.Context, s domain.TelegramSettings) error {
	if s.BotID <= 0 || len(s.AllowedUserIDs) > 100 {
		return ErrTelegramInvalid
	}
	for _, id := range s.AllowedUserIDs {
		if id <= 0 || id > 4503599627370495 {
			return ErrTelegramInvalid
		}
	}
	if s.AllowedUserIDs == nil {
		s.AllowedUserIDs = []int64{}
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = telegramOwner(ctx, tx, s.WorkspaceID); err != nil {
		return err
	}
	// All configuration changes serialize with inbound/outbound authorization.
	var old int64
	err = tx.GetContext(ctx, &old, `SELECT bot_id FROM telegram_settings WHERE workspace_id=$1 FOR UPDATE`, s.WorkspaceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if old != 0 && old != s.BotID {
		// A different bot must never inherit another bot's identities or delivery queue.
		for _, table := range []string{"telegram_links", "telegram_link_codes", "telegram_deliveries"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE workspace_id=$1`, s.WorkspaceID); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_settings(workspace_id,bot_id,bot_username,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(workspace_id) DO UPDATE SET bot_id=excluded.bot_id,bot_username=excluded.bot_username,token_encrypted=excluded.token_encrypted,webhook_secret=excluded.webhook_secret,enabled=excluded.enabled,allowed_user_ids=excluded.allowed_user_ids,status='saved',updated_at=now()`, s.WorkspaceID, s.BotID, s.BotUsername, s.TokenEncrypted, s.WebhookSecret, s.Enabled, s.AllowedUserIDs)
	if err != nil {
		return err
	}
	// Remove revoked links and pending codes. Re-allowing an ID requires explicit linking again.
	for _, table := range []string{"telegram_links", "telegram_link_codes"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE workspace_id=$1 AND NOT(telegram_user_id=ANY($2))`, s.WorkspaceID, s.AllowedUserIDs); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_deliveries SET status='denied',updated_at=now() WHERE workspace_id=$1 AND status='queued' AND (NOT $2 OR NOT(telegram_user_id=ANY($3)))`, s.WorkspaceID, s.Enabled, s.AllowedUserIDs); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *TelegramRepository) Remove(ctx context.Context, ws uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = telegramOwner(ctx, tx, ws); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM telegram_settings WHERE workspace_id=$1`, ws)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *TelegramRepository) SetStatus(ctx context.Context, ws uuid.UUID, secret, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE telegram_settings SET status=$3 WHERE workspace_id=$1 AND webhook_secret=$2`, ws, secret, status)
	return err
}
func (r *TelegramRepository) LinkedID(ctx context.Context, ws uuid.UUID) (*int64, error) {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	var id int64
	err = r.db.GetContext(ctx, &id, `SELECT l.telegram_user_id FROM telegram_links l JOIN workspace_members m ON m.workspace_id=l.workspace_id AND m.user_id=l.user_id WHERE l.workspace_id=$1 AND l.user_id=$2 AND m.role<>'guest'`, ws, user)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}
func (r *TelegramRepository) CreateLink(ctx context.Context, ws uuid.UUID, id int64, hash string) error {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return ErrTelegramDenied
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var s domain.TelegramSettings
	if err = tx.GetContext(ctx, &s, `SELECT `+telegramColumns+` FROM telegram_settings WHERE workspace_id=$1 FOR UPDATE`, ws); err != nil {
		return ErrTelegramDenied
	}
	if !s.Enabled || !slices.Contains(s.AllowedUserIDs, id) {
		return ErrTelegramDenied
	}
	if err = conversationMember(ctx, tx, ws, user); err != nil {
		return ErrTelegramDenied
	}
	var exists bool
	if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM telegram_links WHERE workspace_id=$1 AND telegram_user_id=$2 AND user_id<>$3)`, ws, id, user); err != nil {
		return err
	}
	if exists {
		return ErrTelegramDenied
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_link_codes(workspace_id,user_id,telegram_user_id,code_hash) VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id,user_id) DO UPDATE SET telegram_user_id=excluded.telegram_user_id,code_hash=excluded.code_hash,expires_at=now()+interval '10 minutes'`, ws, user, id, hash)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *TelegramRepository) Unlink(ctx context.Context, ws uuid.UUID) error {
	user, err := conversationActor(ctx, ws)
	if err != nil {
		return ErrTelegramDenied
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Same lock order as receive/save/delivery.
	var id uuid.UUID
	if err = tx.GetContext(ctx, &id, `SELECT workspace_id FROM telegram_settings WHERE workspace_id=$1 FOR UPDATE`, ws); err != nil {
		return err
	}
	for _, table := range []string{"telegram_links", "telegram_link_codes"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE workspace_id=$1 AND user_id=$2`, ws, user); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE telegram_deliveries SET status='denied' WHERE workspace_id=$1 AND user_id=$2 AND status='queued'`, ws, user)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *TelegramRepository) Receive(ctx context.Context, ws uuid.UUID, secret string, u domain.TelegramUpdate, codeHash, provider, model string) error {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// NO KEY UPDATE serializes webhook/settings decisions without blocking the
	// FK KEY SHARE lock acquired by Web conversation outbox inserts.
	var s domain.TelegramSettings
	err = tx.GetContext(ctx, &s, `SELECT `+telegramColumns+` FROM telegram_settings WHERE workspace_id=$1 FOR NO KEY UPDATE`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTelegramDenied
	}
	if err != nil {
		return err
	}
	if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(s.WebhookSecret)) != 1 {
		return ErrTelegramDenied
	}
	// Fail closed, including forwarded identities, groups, bots, and unsupported callbacks.
	if !s.Enabled || !u.Private || u.ChatID != u.UserID || !slices.Contains(s.AllowedUserIDs, u.UserID) || u.ID < 0 || len(u.Text) > 16384 || strings.TrimSpace(u.Text) == "" {
		return nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO telegram_updates(workspace_id,bot_id,update_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, ws, s.BotID, u.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	var user uuid.UUID
	var conversation *uuid.UUID
	if strings.HasPrefix(u.Text, "/start ") {
		err = tx.GetContext(ctx, &user, `SELECT user_id FROM telegram_link_codes WHERE workspace_id=$1 AND telegram_user_id=$2 AND code_hash=$3 AND expires_at>now() FOR UPDATE`, ws, u.UserID, codeHash)
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		if err != nil {
			return err
		}
		if err = conversationMember(ctx, tx, ws, user); err != nil {
			return tx.Commit()
		}
		// Never reassign an existing Telegram identity to a different Kuayle user.
		var existing uuid.UUID
		err = tx.GetContext(ctx, &existing, `SELECT user_id FROM telegram_links WHERE workspace_id=$1 AND telegram_user_id=$2`, ws, u.UserID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && existing != user {
			return tx.Commit()
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM telegram_links WHERE workspace_id=$1 AND user_id=$2 AND telegram_user_id<>$3`, ws, user, u.UserID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,$3) ON CONFLICT(workspace_id,telegram_user_id) DO NOTHING`, ws, user, u.UserID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM telegram_link_codes WHERE workspace_id=$1 AND (user_id=$2 OR telegram_user_id=$3)`, ws, user, u.UserID)
		if err != nil {
			return err
		}
		err = telegramReply(ctx, tx, ws, user, u.UserID, nil, nil, "Account linked. Send a message to start a conversation. Use /new for a new conversation or /stop to cancel a reply.")
	} else {
		err = tx.QueryRowContext(ctx, `SELECT user_id,conversation_id FROM telegram_links WHERE workspace_id=$1 AND telegram_user_id=$2`, ws, u.UserID).Scan(&user, &conversation)
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		if err != nil {
			return err
		}
		if err = conversationMember(ctx, tx, ws, user); err != nil {
			return tx.Commit()
		}
		if conversation == nil || u.Text == "/new" {
			var id uuid.UUID
			err = tx.GetContext(ctx, &id, `INSERT INTO conversations(workspace_id,creator_id,title) VALUES($1,$2,'Telegram conversation') RETURNING id`, ws, user)
			if err != nil {
				return err
			}
			conversation = &id
			_, err = tx.ExecContext(ctx, `UPDATE telegram_links SET conversation_id=$3 WHERE workspace_id=$1 AND telegram_user_id=$2`, ws, u.UserID, id)
			if err != nil {
				return err
			}
		}
		// The settings lock serializes application link changes. Do not lock the
		// link before its issue: issue deletion also clears this link. Acquire
		// the same issue/conversation locks used by Web and provider writers.
		chat, lockErr := lockConversation(ctx, tx, ws, *conversation, user, false)
		err = lockErr
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		if err != nil {
			return err
		}
		if err = conversationScope(ctx, tx, *chat); errors.Is(err, ErrConversationNotFound) {
			return tx.Commit()
		} else if err != nil {
			return err
		}
		switch {
		case u.Text == "/new":
			err = telegramReply(ctx, tx, ws, user, u.UserID, conversation, nil, "New conversation ready.")
		case u.Text == "/stop":
			var active uuid.UUID
			err = tx.GetContext(ctx, &active, `SELECT id FROM conversation_messages WHERE conversation_id=$1 AND status IN ('queued','streaming')`, conversation)
			if err == nil {
				err = transitionConversationMessage(ctx, tx, active, "cancelled")
			}
			if errors.Is(err, sql.ErrNoRows) {
				err = nil
			}
			if err != nil {
				return err
			}
			err = telegramReply(ctx, tx, ws, user, u.UserID, conversation, nil, "Reply stopped.")
		case strings.HasPrefix(u.Text, "/"):
			err = telegramReply(ctx, tx, ws, user, u.UserID, conversation, nil, "Send a message, /new for a new conversation, or /stop to cancel a reply.")
		case provider == "" || model == "":
			err = telegramReply(ctx, tx, ws, user, u.UserID, conversation, nil, "Configure the workspace AI provider in Kuayle first.")
		default:
			if err = activeConversation(ctx, tx, *conversation); errors.Is(err, ErrConversationBusy) {
				err = telegramReply(ctx, tx, ws, user, u.UserID, conversation, nil, "A reply is already in progress. Wait or use /stop, then send your message again.")
			} else if err == nil {
				request := uuid.New()
				var message *domain.ConversationMessage
				message, err = addConversationMessage(ctx, tx, *conversation, request, nil, "user", u.Text, "completed", "", "")
				if err == nil {
					_, err = addConversationMessage(ctx, tx, *conversation, request, &message.ID, "assistant", "", "queued", provider, model)
				}

				if err == nil {
					_, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, conversation)
				}
			}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func telegramReply(ctx context.Context, tx *sqlx.Tx, ws, user uuid.UUID, id int64, conversation, message *uuid.UUID, text string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,message_id,text) VALUES($1,$2,$3,$4,$5,$6)`, ws, user, id, conversation, message, text)
	return err
}

// Deliver holds current configuration/membership locks through the bounded HTTP
// send. A successful revocation cannot race with a later authorized delivery.
// Sending is claimed before external I/O; ambiguous outcomes are never retried.
func (r *TelegramRepository) ClaimDelivery(ctx context.Context) (*domain.TelegramDelivery, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_deliveries SET status='uncertain',updated_at=now() WHERE status='sending' AND updated_at<now()-interval '1 minute'`); err != nil {
		return nil, err
	}
	var d domain.TelegramDelivery
	err = tx.GetContext(ctx, &d, `SELECT d.id,d.workspace_id,d.user_id,d.telegram_user_id,d.conversation_id,d.message_id,CASE WHEN d.message_id IS NULL THEN d.text WHEN m.status='completed' THEN m.content ELSE 'Reply interrupted or cancelled. Open Kuayle to retry.' END AS text,s.token_encrypted,w.slug FROM telegram_deliveries d JOIN telegram_settings s ON s.workspace_id=d.workspace_id JOIN workspaces w ON w.id=d.workspace_id LEFT JOIN conversation_messages m ON m.id=d.message_id WHERE d.status='queued' AND (d.message_id IS NULL OR m.status NOT IN ('queued','streaming')) ORDER BY d.created_at LIMIT 1 FOR UPDATE OF d SKIP LOCKED`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE telegram_deliveries SET status='sending',updated_at=now() WHERE id=$1`, d.ID)
	if err != nil {
		return nil, err
	}
	return &d, tx.Commit()
}
func (r *TelegramRepository) Deliver(ctx context.Context, d domain.TelegramDelivery, send func(domain.TelegramDelivery) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Publication precedes configuration/row locks, matching team visibility
	// transitions. It is distinct from the graph-write barrier and permits
	// unrelated content edits while the bounded outbound request runs.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended($1::text,63))`, d.WorkspaceID); err != nil {
		return err
	}
	var s domain.TelegramSettings
	err = tx.GetContext(ctx, &s, `SELECT `+telegramColumns+` FROM telegram_settings WHERE workspace_id=$1 FOR SHARE`, d.WorkspaceID)
	if err != nil {
		return err
	}
	allowed := s.Enabled && slices.Contains(s.AllowedUserIDs, d.TelegramUserID)
	if allowed {
		allowed = conversationMember(ctx, tx, d.WorkspaceID, d.UserID) == nil
	}
	if allowed && d.ConversationID != nil {
		allowed, err = telegramConversationScope(ctx, tx, d)
		if err != nil {
			return err
		}
	}
	var pending bool
	if err = tx.GetContext(ctx, &pending, `SELECT status='sending' FROM telegram_deliveries WHERE id=$1 FOR UPDATE`, d.ID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if !pending {
		return nil
	}
	var linked bool
	if err = tx.GetContext(ctx, &linked, `SELECT EXISTS(SELECT 1 FROM telegram_links WHERE workspace_id=$1 AND user_id=$2 AND telegram_user_id=$3)`, d.WorkspaceID, d.UserID, d.TelegramUserID); err != nil {
		return err
	}
	status := "denied"
	if allowed && linked {
		d.TokenEncrypted = s.TokenEncrypted
		status = "sent"
		if send(d) != nil {
			status = "uncertain"
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE telegram_deliveries SET status=$2,updated_at=now() WHERE id=$1 AND status='sending'`, d.ID, status)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Serialize remote webhook installation across API replicas, including removal.
// The outer transaction only owns an advisory lock; Save owns data transactions.
func (r *TelegramRepository) Configure(ctx context.Context, ws uuid.UUID, fn func() error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.GetContext(ctx, &locked, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,72169))`, ws.String()); err != nil {
		return err
	}
	if !locked {
		return ErrTelegramBusy
	}
	if err = fn(); err != nil {
		return err
	}
	return tx.Commit()
}

// Authenticate avoids taking a row lock for an unauthenticated public request.
// Receive rechecks the same secret under its serialization lock before mutation.
func (r *TelegramRepository) Authenticate(ctx context.Context, ws uuid.UUID, secret string) error {
	config, err := r.Get(ctx, ws)
	if err != nil {
		return err
	}
	if config == nil || secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(config.WebhookSecret)) != 1 {
		return ErrTelegramDenied
	}
	return nil
}

// Pin the issue and current team membership until outbound I/O finishes. No
// conversation row lock is acquired here, avoiding the Web outbox lock order.
func telegramConversationScope(ctx context.Context, tx *sqlx.Tx, d domain.TelegramDelivery) (bool, error) {
	var c domain.Conversation
	err := tx.GetContext(ctx, &c, `SELECT * FROM conversations WHERE id=$1 AND workspace_id=$2 AND creator_id=$3`, d.ConversationID, d.WorkspaceID, d.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if c.IssueID == nil {
		return true, nil
	}
	var team uuid.UUID
	err = tx.GetContext(ctx, &team, `SELECT team_id FROM issues WHERE id=$1 AND workspace_id=$2 AND team_id=$3 FOR SHARE`, c.IssueID, c.WorkspaceID, c.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var members []uuid.UUID
	err = tx.SelectContext(ctx, &members, `SELECT user_id FROM team_members WHERE team_id=$1 AND user_id=$2 FOR SHARE`, team, d.UserID)
	if err != nil {
		return false, err
	}
	var allowed bool
	err = tx.GetContext(ctx, &allowed, `SELECT kuayle_team_access($1,$2,$3)`, team, d.UserID, d.WorkspaceID)
	return allowed, err
}
