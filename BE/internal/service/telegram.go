package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
var telegramTokenFormat = regexp.MustCompile(`^[1-9][0-9]{0,15}:[A-Za-z0-9_-]{20,100}$`)

type TelegramService struct {
	Repo       *repository.TelegramRepository
	workspaces repository.WorkspaceRepo
	ai         *AISettingsService
	key        []byte
	frontend   string
	client     *http.Client
	endpoint   string
}

func NewTelegramService(repo *repository.TelegramRepository, workspaces repository.WorkspaceRepo, ai *AISettingsService, key []byte, frontend string) *TelegramService {
	client := newAIProviderClient(false)
	client.Timeout = 10 * time.Second
	return &TelegramService{repo, workspaces, ai, key, strings.TrimRight(frontend, "/"), client, "https://api.telegram.org"}
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
	if req.Enabled {
		u, err := url.Parse(s.frontend)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, ErrTelegramAPI
		}
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
		err = s.call(ctx, token, "setWebhook", map[string]any{"url": s.frontend + "/api/telegram/webhook/" + ws.String(), "secret_token": config.WebhookSecret, "allowed_updates": []string{"message", "callback_query"}, "max_connections": 1, "drop_pending_updates": old == nil || !old.Enabled || old.BotID != config.BotID}, nil)
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
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return repository.ErrTelegramInvalid
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
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope) != nil || !envelope.OK {
		return ErrTelegramAPI
	}
	if result != nil && json.Unmarshal(envelope.Result, result) != nil {
		return ErrTelegramAPI
	}
	return nil
}
func (s *TelegramService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
					text += "\n\n" + s.frontend + "/" + url.PathEscape(d.Slug) + "/assistant?conversation=" + d.ConversationID.String()
				}
				return s.call(deliveryCtx, token, "sendMessage", map[string]any{"chat_id": d.TelegramUserID, "text": text, "link_preview_options": map[string]any{"is_disabled": true}}, nil)
			})
			cancel()
		}
	}
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
