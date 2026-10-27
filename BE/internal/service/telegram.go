package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

var ErrTelegramAPI = errors.New("Telegram request failed; check the bot token and public HTTPS address")

// TelegramAPIError carries Telegram's own reason for refusing a call, such as
// "Unauthorized" for a revoked token, so settings can say what went wrong.
type TelegramAPIError struct {
	Method      string
	Description string
}

func (e *TelegramAPIError) Error() string {
	return "Telegram " + e.Method + " failed: " + e.Description
}

func (e *TelegramAPIError) Is(target error) bool { return target == ErrTelegramAPI }

var telegramTokenFormat = regexp.MustCompile(`^[1-9][0-9]{0,15}:[A-Za-z0-9_-]{20,100}$`)

type TelegramService struct {
	Repo       *repository.TelegramRepository
	workspaces repository.WorkspaceRepo
	ai         *AISettingsService
	key        []byte
	frontend   string
	client     *http.Client
	endpoint   string
	approvals  telegramApprovals
	// webhookOverride replaces the derived webhook URL; see SetWebhookURL.
	webhookOverride string
}

type telegramApprovals interface {
	ResolveApproval(ctx context.Context, ws, conversation, id uuid.UUID, approve bool) (*domain.ConversationApproval, error)
}

// SetApprovals lets linked users resolve their own approvals with inline buttons.
func (s *TelegramService) SetApprovals(approvals telegramApprovals) {
	s.approvals = approvals
}

func NewTelegramService(repo *repository.TelegramRepository, workspaces repository.WorkspaceRepo, ai *AISettingsService, key []byte, frontend string) *TelegramService {
	client := newAIProviderClient(false)
	client.Timeout = 10 * time.Second
	return &TelegramService{Repo: repo, workspaces: workspaces, ai: ai, key: key, frontend: strings.TrimRight(frontend, "/"), client: client, endpoint: "https://api.telegram.org"}
}

// SetWebhookURL makes Telegram deliver updates to a fixed HTTPS URL instead of
// FRONTEND_URL, for a development relay such as smee.io that forwards them to
// this API's /api/telegram/webhook/:workspaceId. Empty restores the default.
func (s *TelegramService) SetWebhookURL(raw string) error {
	if raw == "" {
		s.webhookOverride = ""
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("TELEGRAM_WEBHOOK_URL must be an https URL")
	}
	s.webhookOverride = raw
	return nil
}

// publicHTTPSURL is the shape Telegram accepts for a webhook.
func publicHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func (s *TelegramService) webhookURL(ws uuid.UUID) string {
	if s.webhookOverride != "" {
		return s.webhookOverride
	}
	return s.frontend + "/api/telegram/webhook/" + ws.String()
}

type TelegramSettingsRequest struct {
	Token          dto.OptionalString `json:"bot_token"`
	Enabled        bool               `json:"enabled"`
	AllowedUserIDs []int64            `json:"allowed_user_ids"`
}
type TelegramStatus struct {
	Configured     bool    `json:"configured"`
	Owner          bool    `json:"owner"`
	Enabled        bool    `json:"enabled"`
	BotUsername    string  `json:"bot_username"`
	Status         string  `json:"status"`
	AllowedUserIDs []int64 `json:"allowed_user_ids,omitempty"`
	LinkedID       *int64  `json:"linked_user_id"`
}

func (s *TelegramService) access(ctx context.Context, ws uuid.UUID) (bool, error) {
	a, ok := domain.AccessFromContext(ctx)
	if !ok || a.WorkspaceID != ws {
		return false, repository.ErrTelegramDenied
	}
	member, err := s.workspaces.GetMember(ctx, ws, a.UserID)
	if err != nil {
		return false, err
	}
	if member == nil || member.Role == "guest" {
		return false, repository.ErrTelegramDenied
	}
	workspace, err := s.workspaces.GetByID(ctx, ws)
	if err != nil {
		return false, err
	}
	if workspace == nil {
		return false, repository.ErrTelegramDenied
	}
	return workspace.OwnerID == a.UserID, nil
}
func (s *TelegramService) Status(ctx context.Context, ws uuid.UUID) (*TelegramStatus, error) {
	owner, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	result := &TelegramStatus{Owner: owner, Status: "not_configured"}
	config, err := s.Repo.Get(ctx, ws)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return result, nil
	}
	result.Configured = true
	result.Enabled = config.Enabled
	result.BotUsername = config.BotUsername
	result.Status = config.Status
	if owner {
		result.AllowedUserIDs = config.AllowedUserIDs
	}
	result.LinkedID, err = s.Repo.LinkedID(ctx, ws)
	return result, err
}
func (s *TelegramService) Save(ctx context.Context, ws uuid.UUID, req TelegramSettingsRequest) (*TelegramStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := acquirePublicationSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	var result *TelegramStatus
	err = s.Repo.Configure(ctx, ws, func() error {
		var err error
		result, err = s.save(ctx, ws, req)
		return err
	})
	return result, err
}
func (s *TelegramService) save(ctx context.Context, ws uuid.UUID, req TelegramSettingsRequest) (*TelegramStatus, error) {
	owner, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, repository.ErrTelegramDenied
	}
	if len(req.AllowedUserIDs) > 100 {
		return nil, repository.ErrTelegramInvalid
	}
	for _, id := range req.AllowedUserIDs {
		if id <= 0 || id > 4503599627370495 {
			return nil, repository.ErrTelegramInvalid
		}
	}
	old, err := s.Repo.Get(ctx, ws)
	if err != nil {
		return nil, err
	}
	// Removing locally immediately revokes webhook access and queued delivery.
	if req.Token.Set && (req.Token.Value == nil || strings.TrimSpace(*req.Token.Value) == "") {
		if err = s.Repo.Remove(ctx, ws); err != nil {
			return nil, err
		}
		return s.Status(ctx, ws)
	}
	var config domain.TelegramSettings
	if old != nil {
		config = *old
	} else {
		config.WorkspaceID = ws
	}
	if req.Token.Set {
		token := strings.TrimSpace(*req.Token.Value)
		if !telegramTokenFormat.MatchString(token) {
			return nil, repository.ErrTelegramInvalid
		}
		var me struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			IsBot    bool   `json:"is_bot"`
		}
		if err = s.call(ctx, token, "getMe", struct{}{}, &me); err != nil {
			return nil, err
		}
		if !me.IsBot || me.ID <= 0 || me.Username == "" {
			return nil, ErrTelegramAPI
		}
		config.BotID = me.ID
		config.BotUsername = me.Username
		config.TokenEncrypted, err = crypto.Encrypt(token, s.key)
		if err != nil {
			return nil, err
		}
	}
	if config.TokenEncrypted == "" {
		return nil, repository.ErrTelegramInvalid
	}
	if req.Enabled && !publicHTTPSURL(s.webhookURL(ws)) {
		return nil, &TelegramAPIError{Method: "setWebhook", Description: "the webhook address must be public HTTPS; set FRONTEND_URL or TELEGRAM_WEBHOOK_URL"}
	}
	config.Enabled = req.Enabled
	config.AllowedUserIDs = req.AllowedUserIDs
	// Retain the header while disabled so Telegram updates can be acknowledged
	// and ignored. Re-enabling rotates it and drops the disabled-period backlog.
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}
	config.WebhookSecret = hex.EncodeToString(secret)
	if !config.Enabled && old != nil {
		config.WebhookSecret = old.WebhookSecret
	}
	if err = s.Repo.Save(ctx, config); err != nil {
		return nil, err
	}
	status := "disabled"
	if config.Enabled {
		token, e := crypto.Decrypt(config.TokenEncrypted, s.key)
		if e != nil {
			return nil, e
		}
		err = s.call(ctx, token, "setWebhook", map[string]any{"url": s.webhookURL(ws), "secret_token": config.WebhookSecret, "allowed_updates": []string{"message", "callback_query"}, "max_connections": 1, "drop_pending_updates": old == nil || !old.Enabled || old.BotID != config.BotID}, nil)
		status = "connected"
		if err != nil {
			status = "connection_failed"
		}
	}
	if e := s.Repo.SetStatus(ctx, ws, config.WebhookSecret, status); e != nil {
		return nil, e
	}
	return s.Status(ctx, ws)
}
func telegramCodeHash(code string) string {
	hash := sha256.Sum256([]byte(code))
	return hex.EncodeToString(hash[:])
}
func (s *TelegramService) Link(ctx context.Context, ws uuid.UUID, id int64) (string, error) {
	if _, err := s.access(ctx, ws); err != nil {
		return "", err
	}
	code := make([]byte, 24)
	if _, err := rand.Read(code); err != nil {
		return "", err
	}
	raw := hex.EncodeToString(code)
	if err := s.Repo.CreateLink(ctx, ws, id, telegramCodeHash(raw)); err != nil {
		return "", err
	}
	return "/start " + raw, nil
}
func (s *TelegramService) Receive(ctx context.Context, ws uuid.UUID, secret string, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Repo.Authenticate(ctx, ws, secret); err != nil {
		return err
	}
	var update struct {
		ID      int64 `json:"update_id"`
		Message *struct {
			Text string `json:"text"`
			From *struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			} `json:"from"`
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
		} `json:"message"`
		Callback *struct {
			ID   string `json:"id"`
			Data string `json:"data"`
			From *struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			} `json:"from"`
			Message *struct {
				ID   int64 `json:"message_id"`
				Chat struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"callback_query"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return repository.ErrTelegramInvalid
	}
	if c := update.Callback; c != nil {
		u := domain.TelegramUpdate{ID: update.ID, CallbackID: c.ID, CallbackData: c.Data}
		if c.From != nil && c.Message != nil && len(c.ID) <= 128 && len(c.Data) <= 64 {
			u.UserID, u.ChatID, u.CallbackMessageID = c.From.ID, c.Message.Chat.ID, c.Message.ID
			u.Private = c.Message.Chat.Type == "private" && !c.From.IsBot
		}
		return s.receiveCallback(ctx, ws, secret, u)
	}
	u := domain.TelegramUpdate{ID: update.ID}
	if update.Message != nil && update.Message.From != nil {
		m := update.Message
		u.UserID = m.From.ID
		u.ChatID = m.Chat.ID
		u.Text = m.Text
		u.Private = m.Chat.Type == "private" && !m.From.IsBot
	}
	provider, model := "", ""
	settings, err := s.ai.Get(ctx, ws)
	if err != nil {
		return err
	}
	if settings != nil && settings.APIKeyEncrypted != nil && *settings.APIKeyEncrypted != "" && settings.BaseURL != "" {
		provider = settings.Provider
		model = settings.Model
	}
	hash := ""
	if strings.HasPrefix(u.Text, "/start ") {
		hash = telegramCodeHash(strings.TrimSpace(strings.TrimPrefix(u.Text, "/start ")))
	}
	return s.Repo.Receive(ctx, ws, secret, u, hash, provider, model)
}

// Never expose errors from the HTTP transport: Telegram embeds the token in URL paths.
func (s *TelegramService) call(ctx context.Context, token, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return ErrTelegramAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/bot"+token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return ErrTelegramAPI
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return ErrTelegramAPI
	}
	defer res.Body.Close()
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope)
	if res.StatusCode != http.StatusOK || decodeErr != nil || !envelope.OK {
		if description := telegramDescription(envelope.Description, token); description != "" {
			return &TelegramAPIError{Method: method, Description: description}
		}
		return ErrTelegramAPI
	}
	if result != nil && json.Unmarshal(envelope.Result, result) != nil {
		return ErrTelegramAPI
	}
	return nil
}

// telegramDescription keeps Telegram's short reason printable and bounded, and
// never echoes the token.
func telegramDescription(description, token string) string {
	description = strings.ReplaceAll(description, token, "[redacted]")
	description = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, description)
	if len(description) > 200 {
		description = description[:200]
	}
	return strings.TrimSpace(description)
}

func (s *TelegramService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.previewReplies(ctx)
			claimCtx, stopClaim := context.WithTimeout(ctx, 5*time.Second)
			d, err := s.Repo.ClaimDelivery(claimCtx)
			stopClaim()
			if err != nil || d == nil {
				continue
			}
			deliveryCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			_ = s.Repo.Deliver(deliveryCtx, *d, func(d domain.TelegramDelivery) error {
				token, err := crypto.Decrypt(d.TokenEncrypted, s.key)
				if err != nil {
					return err
				}
				text := telegramText(d.Text, 3200)
				if d.ConversationID != nil {
					link := s.frontend + "/" + url.PathEscape(d.Slug) + "/assistant?conversation=" + d.ConversationID.String()
					// Telegram only makes public addresses clickable.
					if strings.HasPrefix(link, "https://") {
						text += "\n\n[Open in Kuayle](" + link + ")"
					} else {
						text += "\n\n" + link
					}
				}
				payload := map[string]any{"chat_id": d.TelegramUserID, "link_preview_options": map[string]any{"is_disabled": true}}
				if keyboard := approvalKeyboard(d.Approvals); keyboard != nil {
					payload["reply_markup"] = keyboard
				}
				// The final reply replaces the message streamed while it was written.
				if d.TelegramMessageID != nil {
					edited := map[string]any{"message_id": *d.TelegramMessageID}
					for key, value := range payload {
						edited[key] = value
					}
					if err := s.sendFormatted(deliveryCtx, token, "editMessageText", edited, text, nil); err == nil {
						return nil
					}
				}
				return s.sendFormatted(deliveryCtx, token, "sendMessage", payload, text, nil)
			})
			cancel()
		}
	}
}

// sendFormatted sends Markdown as Telegram HTML, and as plain text when
// Telegram refuses the formatting.
func (s *TelegramService) sendFormatted(ctx context.Context, token, method string, payload map[string]any, markdown string, result any) error {
	formatted := make(map[string]any, len(payload)+2)
	for key, value := range payload {
		formatted[key] = value
	}
	formatted["text"], formatted["parse_mode"] = telegramHTML(markdown), "HTML"
	err := s.call(ctx, token, method, formatted, result)
	var refused *TelegramAPIError
	if !errors.As(err, &refused) {
		return err
	}
	plain := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		plain[key] = value
	}
	plain["text"] = markdown
	return s.call(ctx, token, method, plain, result)
}

// previewReplies streams replies still being written: "typing…" until text
// arrives, then one message edited with the text so far every few seconds.
func (s *TelegramService) previewReplies(ctx context.Context) {
	for range 5 {
		claimCtx, stopClaim := context.WithTimeout(ctx, 5*time.Second)
		d, err := s.Repo.ClaimPreview(claimCtx)
		stopClaim()
		if err != nil || d == nil {
			return
		}
		if strings.TrimSpace(d.Text) != "" {
			d.Text = telegramText(strings.TrimSpace(d.Text), 3000)
		}
		previewCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = s.Repo.Preview(previewCtx, *d, func(d domain.TelegramDelivery) (int64, error) {
			return s.preview(previewCtx, d)
		})
		cancel()
	}
}

func (s *TelegramService) preview(ctx context.Context, d domain.TelegramDelivery) (int64, error) {
	token, err := crypto.Decrypt(d.TokenEncrypted, s.key)
	if err != nil {
		return 0, err
	}
	if d.Text == "" || (d.TelegramMessageID != nil && d.Text == d.PreviewText) {
		return 0, s.call(ctx, token, "sendChatAction", map[string]any{"chat_id": d.TelegramUserID, "action": "typing"}, nil)
	}
	payload := map[string]any{"chat_id": d.TelegramUserID, "link_preview_options": map[string]any{"is_disabled": true}}
	if d.TelegramMessageID != nil {
		payload["message_id"] = *d.TelegramMessageID
		return *d.TelegramMessageID, s.sendFormatted(ctx, token, "editMessageText", payload, d.Text+" …", nil)
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	if err = s.sendFormatted(ctx, token, "sendMessage", payload, d.Text+" …", &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

func telegramText(text string, maxUnits int) string {
	if strings.TrimSpace(text) == "" {
		return "Open Kuayle to view the response."
	}
	units := 0
	var b strings.Builder
	for _, r := range text {
		n := utf16.RuneLen(r)
		if units+n > maxUnits {
			b.WriteString("…")
			break
		}
		units += n
		b.WriteRune(r)
	}
	return b.String()
}

// approvalKeyboard offers one Approve/Deny row per pending approval. The
// callback data only names the approval; the server decides who may act.
func approvalKeyboard(approvals []domain.TelegramApprovalButton) map[string]any {
	if len(approvals) == 0 {
		return nil
	}
	rows := make([][]map[string]string, 0, len(approvals))
	for _, a := range approvals {
		rows = append(rows, []map[string]string{
			{"text": telegramText("Approve "+a.Identifier, 40), "callback_data": "approve:" + a.ID.String()},
			{"text": "Deny", "callback_data": "deny:" + a.ID.String()},
		})
	}
	return map[string]any{"inline_keyboard": rows}
}

// receiveCallback resolves an approval for the linked Kuayle user through the
// same backend path as the Web UI, then answers the button press. Telegram
// answers are best effort; the decision itself is already durable.
func (s *TelegramService) receiveCallback(ctx context.Context, ws uuid.UUID, secret string, u domain.TelegramUpdate) error {
	callback, err := s.Repo.ReceiveCallback(ctx, ws, secret, u)
	if err != nil || callback == nil {
		return err
	}
	answer, final := "This approval is not available.", true
	if s.approvals == nil {
		answer = "Approvals are not available from Telegram."
	} else if callback.ConversationID != uuid.Nil {
		access := domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: callback.UserID})
		approval, resolveErr := s.approvals.ResolveApproval(access, ws, callback.ConversationID, callback.ApprovalID, callback.Approve)
		answer, final = telegramApprovalAnswer(approval, resolveErr)
	}
	token, err := crypto.Decrypt(callback.TokenEncrypted, s.key)
	if err != nil {
		return nil
	}
	_ = s.call(ctx, token, "answerCallbackQuery", map[string]any{"callback_query_id": u.CallbackID, "text": answer}, nil)
	if final && u.CallbackMessageID > 0 {
		_ = s.call(ctx, token, "editMessageReplyMarkup", map[string]any{"chat_id": u.ChatID, "message_id": u.CallbackMessageID, "reply_markup": map[string]any{"inline_keyboard": [][]any{}}}, nil)
	}
	return nil
}

func telegramApprovalAnswer(approval *domain.ConversationApproval, err error) (string, bool) {
	switch {
	case errors.Is(err, repository.ErrApprovalExpired):
		return "This approval has expired.", true
	case errors.Is(err, repository.ErrConversationConflict):
		return "This approval was already decided.", true
	case errors.Is(err, repository.ErrConversationNotFound):
		return "This approval is not available.", true
	case err != nil || approval == nil:
		return "The decision could not be saved. Try again.", false
	}
	switch approval.Status {
	case "approved":
		return "Approved: the coding agent is starting.", true
	case "denied":
		return "Denied.", true
	case "failed":
		message := "The coding agent could not start."
		if approval.Error != nil {
			message += " " + *approval.Error
		}
		return telegramText(message, 190), true
	}
	return "The decision could not be saved. Try again.", false
}
