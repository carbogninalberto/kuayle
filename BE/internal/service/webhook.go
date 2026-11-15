package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/lib/pq"
	log "github.com/sirupsen/logrus"
)

const (
	// webhookRequestTimeout bounds one HTTP request to a receiver.
	webhookRequestTimeout = 10 * time.Second
	// webhookDeliveryTimeout bounds a whole delivery: waiting for a
	// publication slot, taking the privacy guard and the request itself.
	webhookDeliveryTimeout = time.Minute
	// webhookListTimeout bounds the lookup of a workspace's webhooks.
	webhookListTimeout = 10 * time.Second
)

var _ webhookEmitter = (*WebhookService)(nil)

type WebhookService struct {
	webhookRepo   *repository.WebhookRepository
	encryptionKey []byte
	client        *http.Client
}

func NewWebhookService(webhookRepo *repository.WebhookRepository, jwtSecret string) *WebhookService {
	return &WebhookService{
		webhookRepo:   webhookRepo,
		encryptionKey: crypto.DeriveKey(jwtSecret + ":webhook"),
		client:        newWebhookClient(false),
	}
}

// newWebhookClient dials only public addresses (checked after DNS resolution,
// so a receiver host that rebinds to an internal address is refused) and never
// follows redirects. Tests allow loopback receivers.
func newWebhookClient(allowPrivateNetwork bool) *http.Client {
	dialer := &net.Dialer{Timeout: webhookRequestTimeout}
	if !allowPrivateNetwork {
		dialer.Control = validate.PublicAddressControl
	}
	return &http.Client{
		Timeout: webhookRequestTimeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: webhookRequestTimeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// validateWebhookEvents accepts only event names Kuayle delivers.
func validateWebhookEvents(events []string) error {
	for _, event := range events {
		if !slices.Contains(WebhookEvents, event) {
			return fmt.Errorf("unknown webhook event %q; valid events are %s", event, strings.Join(WebhookEvents, ", "))
		}
	}
	return nil
}

func (s *WebhookService) Create(ctx context.Context, workspaceID uuid.UUID, req dto.CreateWebhookRequest) (*domain.Webhook, error) {
	if err := validate.ValidateWebhookURL(req.URL); err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}
	if err := validateWebhookEvents(req.Events); err != nil {
		return nil, err
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
		if err := validateWebhookEvents(req.Events); err != nil {
			return nil, err
		}
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

// actorPayloadID reports automation (uuid.Nil) as a JSON null.
func actorPayloadID(actorID uuid.UUID) *uuid.UUID {
	if actorID == uuid.Nil {
		return nil
	}
	return &actorID
}

// IssueEvent delivers issue.created, issue.updated, issue.deleted or
// issue.triaged with the issue as the API returns it.
func (s *WebhookService) IssueEvent(ctx context.Context, workspaceID uuid.UUID, event string, issue *domain.Issue, actorID uuid.UUID) {
	if s == nil || issue == nil {
		return
	}
	if event == WebhookEventCommentCreated || !slices.Contains(WebhookEvents, event) {
		log.WithField("event", event).Debug("ignoring unknown webhook issue event")
		return
	}
	s.Dispatch(ctx, workspaceID, event, dto.WebhookIssuePayload{Issue: dto.WebhookIssue(*issue), ActorID: actorPayloadID(actorID)})
}

// CommentCreated delivers comment.created with the comment and a reference to
// its issue.
func (s *WebhookService) CommentCreated(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, comment *domain.Comment, actorID uuid.UUID) {
	if s == nil || issue == nil || comment == nil {
		return
	}
	s.Dispatch(ctx, workspaceID, WebhookEventCommentCreated, dto.WebhookCommentPayload{
		Comment: dto.WebhookComment(*comment),
		Issue:   dto.WebhookIssueRef{ID: issue.ID, Identifier: issue.Identifier, TeamID: issue.TeamID},
		ActorID: actorPayloadID(actorID),
	})
}

// Dispatch sends an event payload to all active webhooks subscribed to the event.
// Payloads are signed with HMAC-SHA256 in the X-Webhook-Signature header.
//
// The body is marshalled before Dispatch returns; delivery happens in the
// background. Deliveries outlive the caller's request and do not run with the
// caller's access: the subscriber is the workspace, not the actor, and the
// privacy guard taken per delivery is the boundary.
func (s *WebhookService) Dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payload interface{}) {
	if s == nil {
		return
	}
	system := domain.PublicContext(context.WithoutCancel(ctx))

	listCtx, cancelList := context.WithTimeout(system, webhookListTimeout)
	webhooks, err := s.webhookRepo.ListByWorkspace(listCtx, workspaceID)
	cancelList()
	if err != nil {
		log.WithError(err).Warn("failed to list webhooks for dispatch")
		return
	}
	targets := webhooks[:0]
	for _, wh := range webhooks {
		if wh.IsActive && slices.Contains([]string(wh.Events), event) {
			targets = append(targets, wh)
		}
	}
	if len(targets) == 0 {
		return
	}

	body, err := json.Marshal(dto.WebhookDelivery{
		Event:     event,
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		log.WithError(err).Warn("failed to marshal webhook payload")
		return
	}

	for _, wh := range targets {
		go s.deliver(system, workspaceID, event, wh, body)
	}
}

func (s *WebhookService) deliver(system context.Context, workspaceID uuid.UUID, event string, w domain.Webhook, body []byte) {
	ctx, cancel := context.WithTimeout(system, webhookDeliveryTimeout)
	defer cancel()
	logger := log.WithField("webhook_id", w.ID).WithField("event", event)

	// Queued payloads must recheck privacy at delivery, holding the
	// transition lock until the external request is finished.
	releaseSlot, err := acquirePublicationSlot(ctx)
	if err != nil {
		logger.WithError(err).Warn("webhook delivery dropped: no publication slot")
		return
	}
	defer releaseSlot()
	guard, err := s.webhookRepo.BeginPublicOperation(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, repository.ErrPrivateWorkspaceOperation) {
			logger.Debug("webhook delivery skipped: workspace has private teams")
		} else {
			logger.WithError(err).Warn("webhook delivery dropped: privacy guard failed")
		}
		return
	}
	defer func() { _ = guard.Rollback() }()
	secret, err := crypto.Decrypt(w.Secret, s.encryptionKey)
	if err != nil {
		logger.WithError(err).Warn("failed to decrypt webhook secret")
		return
	}

	signature := crypto.HMACSha256(body, secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		logger.WithError(err).Warn("failed to create webhook request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	req.Header.Set("X-Webhook-Event", event)

	resp, err := s.client.Do(req)
	if err != nil {
		logger.WithError(err).Warn("webhook delivery failed")
		return
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		logger.WithField("status", resp.StatusCode).Warn("webhook receiver rejected delivery")
	}
}
