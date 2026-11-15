package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
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
	// webhookDeliveryTimeout bounds one delivery once a worker takes it: the
	// privacy guard and the request.
	webhookDeliveryTimeout = 30 * time.Second
	// webhookListTimeout bounds the lookup of a workspace's webhooks.
	webhookListTimeout = 10 * time.Second
	// webhookWorkers bounds concurrent deliveries and so the database
	// connections they hold for the privacy guard, well below the server
	// pool of 25. Deliveries do not use the publication slots.
	webhookWorkers = 4
	// webhookQueueLimit, webhookWorkspaceBacklog and webhookQueueBytes bound
	// the deliveries waiting for a worker: in all, per workspace, and their
	// bodies' bytes. A delivery beyond a bound is dropped, never waited for.
	webhookQueueLimit       = 1024
	webhookWorkspaceBacklog = 256
	webhookQueueBytes       = 32 << 20
	// webhookMaxQueueWait drops a delivery that waited longer for a worker.
	webhookMaxQueueWait = 5 * time.Minute
)

var _ webhookEmitter = (*WebhookService)(nil)

type WebhookService struct {
	webhookRepo   *repository.WebhookRepository
	encryptionKey []byte
	client        *http.Client
	// queue holds deliveries for workers started on first use; workers is
	// their number. Tests may replace both before the first dispatch.
	queue     *webhookQueue
	workers   int
	startOnce sync.Once
}

func NewWebhookService(webhookRepo *repository.WebhookRepository, jwtSecret string) *WebhookService {
	return &WebhookService{
		webhookRepo:   webhookRepo,
		encryptionKey: crypto.DeriveKey(jwtSecret + ":webhook"),
		client:        newWebhookClient(false),
		queue:         newWebhookQueue(webhookQueueLimit, webhookWorkspaceBacklog, webhookQueueBytes),
		workers:       webhookWorkers,
	}
}

// start runs the workers once; they live as long as the process.
func (s *WebhookService) start() {
	s.startOnce.Do(func() {
		for range s.workers {
			go func() {
				for {
					job := s.queue.take()
					s.deliver(job)
					s.queue.done(job.workspaceID)
				}
			}()
		}
	})
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
		// A changed list may name only delivered events. The stored list sent
		// back unchanged is accepted even when it names events Kuayle no
		// longer delivers, so a client can resend what it read.
		if !slices.Equal(req.Events, []string(w.Events)) {
			if err := validateWebhookEvents(req.Events); err != nil {
				return nil, err
			}
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
	s.IssueEvents(ctx, workspaceID, event, []domain.Issue{*issue}, actorID)
}

// IssueEvents delivers event for several issues of one workspace, listing the
// workspace's webhooks once. Bulk operations use it.
func (s *WebhookService) IssueEvents(ctx context.Context, workspaceID uuid.UUID, event string, issues []domain.Issue, actorID uuid.UUID) {
	if s == nil || len(issues) == 0 {
		return
	}
	if event == WebhookEventCommentCreated || !slices.Contains(WebhookEvents, event) {
		log.WithField("event", event).Debug("ignoring unknown webhook issue event")
		return
	}
	payloads := make([]any, len(issues))
	for i := range issues {
		payloads[i] = dto.WebhookIssuePayload{Issue: dto.WebhookIssue(issues[i]), ActorID: actorPayloadID(actorID)}
	}
	s.dispatch(ctx, workspaceID, event, payloads)
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
// The body is marshalled before Dispatch returns and delivery is queued to the
// service's bounded workers. Deliveries outlive the caller's request and do not
// run with the caller's access: the subscriber is the workspace, not the actor,
// and the privacy guard taken per delivery is the boundary. The workspace's
// webhooks are listed once per call.
func (s *WebhookService) Dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payload interface{}) {
	if s == nil {
		return
	}
	s.dispatch(ctx, workspaceID, event, []any{payload})
}

// dispatch lists the workspace's deliverable webhooks once, marshals each
// payload and queues one delivery per payload and webhook. It never blocks on
// delivery: a delivery the queue cannot hold is dropped and counted.
func (s *WebhookService) dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payloads []any) {
	system := domain.PublicContext(context.WithoutCancel(ctx))

	listCtx, cancel := context.WithTimeout(system, webhookListTimeout)
	targets, err := s.webhookRepo.ListDeliverable(listCtx, workspaceID, event)
	cancel()
	if err != nil {
		log.WithError(err).WithField("workspace_id", workspaceID).Warn("failed to list webhooks for dispatch")
		return
	}
	if len(targets) == 0 {
		return
	}
	s.start()

	now := time.Now()
	timestamp := now.UTC().Format(time.RFC3339)
	dropped := 0
	for _, payload := range payloads {
		body, err := json.Marshal(dto.WebhookDelivery{Event: event, Payload: payload, Timestamp: timestamp})
		if err != nil {
			log.WithError(err).Warn("failed to marshal webhook payload")
			continue
		}
		for _, hook := range targets {
			if !s.queue.enqueue(webhookJob{ctx: system, workspaceID: workspaceID, event: event, hook: hook, body: body, queued: now}) {
				dropped++
			}
		}
	}
	if dropped > 0 {
		// One line per call, without URLs or secrets.
		log.WithField("workspace_id", workspaceID).WithField("event", event).WithField("dropped", dropped).
			WithField("dropped_total", s.queue.dropped.Load()).Warn("webhook deliveries dropped: the delivery queue is full")
	}
}

func (s *WebhookService) deliver(job webhookJob) {
	logger := log.WithField("webhook_id", job.hook.ID).WithField("event", job.event)
	if waited := time.Since(job.queued); waited > webhookMaxQueueWait {
		s.queue.dropped.Add(1)
		logger.WithField("waited", waited.Round(time.Second).String()).Warn("webhook delivery dropped: it waited too long for a worker")
		return
	}
	ctx, cancel := context.WithTimeout(job.ctx, webhookDeliveryTimeout)
	defer cancel()

	// Queued payloads must recheck privacy at delivery, holding the
	// transition lock until the external request is finished.
	guard, err := s.webhookRepo.BeginPublicOperation(ctx, job.workspaceID)
	if err != nil {
		if errors.Is(err, repository.ErrPrivateWorkspaceOperation) {
			logger.Debug("webhook delivery skipped: workspace has private teams")
		} else {
			logger.WithError(err).Warn("webhook delivery dropped: privacy guard failed")
		}
		return
	}
	defer func() { _ = guard.Rollback() }()
	secret, err := crypto.Decrypt(job.hook.Secret, s.encryptionKey)
	if err != nil {
		logger.WithError(err).Warn("failed to decrypt webhook secret")
		return
	}

	signature := crypto.HMACSha256(job.body, secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.hook.URL, bytes.NewReader(job.body))
	if err != nil {
		logger.WithError(withoutURL(err)).Warn("failed to create webhook request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	req.Header.Set("X-Webhook-Event", job.event)

	resp, err := s.client.Do(req)
	if err != nil {
		logger.WithError(withoutURL(err)).Warn("webhook delivery failed")
		return
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		logger.WithField("status", resp.StatusCode).Warn("webhook receiver rejected delivery")
	}
}

// withoutURL drops the receiver URL from a request error: *url.Error prints
// the whole URL, and receiver URLs often carry a token (Slack, Discord).
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
