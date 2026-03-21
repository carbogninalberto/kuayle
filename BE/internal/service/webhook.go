package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/lib/pq"
	log "github.com/sirupsen/logrus"
)

type WebhookService struct {
	webhookRepo   *repository.WebhookRepository
	encryptionKey []byte
}

func NewWebhookService(webhookRepo *repository.WebhookRepository, jwtSecret string) *WebhookService {
	return &WebhookService{
		webhookRepo:   webhookRepo,
		encryptionKey: crypto.DeriveKey(jwtSecret + ":webhook"),
	}
}

func (s *WebhookService) Create(ctx context.Context, workspaceID uuid.UUID, req dto.CreateWebhookRequest) (*domain.Webhook, error) {
	if err := validate.ValidateWebhookURL(req.URL); err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}

	encSecret, err := crypto.Encrypt(req.Secret, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt webhook secret: %w", err)
	}

	w := &domain.Webhook{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		URL:         req.URL,
		Secret:      encSecret,
		Events:      pq.StringArray(req.Events),
		IsActive:    true,
	}
	if err := s.webhookRepo.Create(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WebhookService) List(ctx context.Context, workspaceID uuid.UUID) ([]domain.Webhook, error) {
	return s.webhookRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *WebhookService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateWebhookRequest) (*domain.Webhook, error) {
	w, err := s.webhookRepo.GetByID(ctx, id)
	if err != nil || w == nil {
		return nil, fmt.Errorf("webhook not found")
	}
	if req.URL != nil {
		if err := validate.ValidateWebhookURL(*req.URL); err != nil {
			return nil, fmt.Errorf("invalid webhook URL: %w", err)
		}
		w.URL = *req.URL
	}
	if req.Events != nil {
		w.Events = pq.StringArray(req.Events)
	}
	if req.IsActive != nil {
		w.IsActive = *req.IsActive
	}
	if err := s.webhookRepo.Update(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WebhookService) Delete(ctx context.Context, id uuid.UUID) error {
	w, err := s.webhookRepo.GetByID(ctx, id)
	if err != nil || w == nil {
		return fmt.Errorf("webhook not found")
	}
	return s.webhookRepo.Delete(ctx, id)
}

// Dispatch sends an event payload to all active webhooks subscribed to the event.
// Payloads are signed with HMAC-SHA256 in the X-Webhook-Signature header.
func (s *WebhookService) Dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payload interface{}) {
	webhooks, err := s.webhookRepo.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		log.WithError(err).Warn("failed to list webhooks for dispatch")
		return
	}

	body, err := json.Marshal(map[string]interface{}{
		"event":     event,
		"payload":   payload,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		log.WithError(err).Warn("failed to marshal webhook payload")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, wh := range webhooks {
		if !wh.IsActive {
			continue
		}
		if !slices.Contains([]string(wh.Events), event) {
			continue
		}

		go func(w domain.Webhook) {
			secret, err := crypto.Decrypt(w.Secret, s.encryptionKey)
			if err != nil {
				log.WithError(err).WithField("webhook_id", w.ID).Warn("failed to decrypt webhook secret")
				return
			}

			signature := crypto.HMACSha256(body, secret)

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
			if err != nil {
				log.WithError(err).WithField("webhook_id", w.ID).Warn("failed to create webhook request")
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Webhook-Signature", "sha256="+signature)
			req.Header.Set("X-Webhook-Event", event)

			resp, err := client.Do(req)
			if err != nil {
				log.WithError(err).WithField("webhook_id", w.ID).Warn("webhook delivery failed")
				return
			}
			resp.Body.Close()
		}(wh)
	}
}
