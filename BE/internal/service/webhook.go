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
	// webhookQueueJobs bounds the jobs waiting for a worker in all workspaces.
	webhookQueueJobs = 4096
	// webhookQueueBytes bounds the body bytes waiting for a worker in all
	// workspaces.
	webhookQueueBytes = 32 << 20
	// webhookWorkspaceJobs bounds one workspace's waiting jobs to one
	// sixteenth of the global job bound: filling one workspace never drops
	// another workspace's deliveries, and 16 full workspaces are needed to
	// fill the global bound.
	webhookWorkspaceJobs = webhookQueueJobs / 16
	// webhookWorkspaceBytes bounds one workspace's waiting body bytes to one
	// sixteenth of the global byte bound, so memory stays bounded by
	// webhookQueueBytes plus the small per-job overhead, also when one
	// workspace emits much more than the rest.
	webhookWorkspaceBytes = webhookQueueBytes / 16
	// webhookMaxQueueWait drops a delivery that waited longer for a worker.
	webhookMaxQueueWait = 5 * time.Minute
	// webhookBreakerFailures is the number of consecutive failed deliveries
	// that pause a webhook.
	webhookBreakerFailures = 3
	// webhookBreakerCooldown is how long a paused webhook is skipped before
	// one delivery probes it again.
	webhookBreakerCooldown = 5 * time.Minute
)

var _ webhookEmitter = (*WebhookService)(nil)

type WebhookService struct {
	webhookRepo   *repository.WebhookRepository
	encryptionKey []byte
	client        *http.Client
	// queue holds deliveries for workers started on first use; workers is
	// their number, and breaker pauses webhooks that keep failing. Tests may
	// replace them before the first dispatch.
	queue   *webhookQueue
	breaker *webhookBreaker
	workers int

	mu      sync.Mutex
	started bool
	closed  bool
	// stop aborts in-flight deliveries on Close; workers exit when the queue
	// closes, and wg waits for them.
	stop   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewWebhookService(webhookRepo *repository.WebhookRepository, jwtSecret string) *WebhookService {
	stop, cancel := context.WithCancel(context.Background())
	return &WebhookService{
		webhookRepo:   webhookRepo,
		encryptionKey: crypto.DeriveKey(jwtSecret + ":webhook"),
		client:        newWebhookClient(false),
		queue:         newWebhookQueue(defaultWebhookLimits),
		breaker:       newWebhookBreaker(),
		workers:       webhookWorkers,
		stop:          stop,
		cancel:        cancel,
	}
}

// start runs the workers once; they live until Close.
func (s *WebhookService) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	s.wg.Add(s.workers)
	for range s.workers {
		go func() {
			defer s.wg.Done()
			for {
				job, ok := s.queue.take()
				if !ok {
					return
				}
				s.run(job)
			}
		}()
	}
}

// run delivers one job and then lets its workspace take another turn, also
// when the delivery stops early.
func (s *WebhookService) run(job webhookJob) {
	defer s.queue.done(job.workspaceID)
	s.deliver(job)
}

// Close stops the workers and abandons queued deliveries. Production never
// calls it: workers live for the process. Tests call it so workers do not leak
// between tests.
func (s *WebhookService) Close() {
	s.mu.Lock()
	alreadyClosed := s.closed
	s.closed = true
	s.mu.Unlock()
	if !alreadyClosed {
		s.cancel() // abort in-flight requests
		s.queue.close()
	}
	s.wg.Wait()
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

// dispatch lists the workspace's deliverable webhooks once, filters out the
// ones the breaker paused, marshals each payload and queues one job holding
// all remaining webhooks. It never blocks on delivery: a job the queue cannot
// hold is dropped and counted.
func (s *WebhookService) dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payloads []any) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	system := domain.PublicContext(context.WithoutCancel(ctx))

	listCtx, cancel := context.WithTimeout(system, webhookListTimeout)
	targets, err := s.webhookRepo.ListDeliverable(listCtx, workspaceID, event)
	cancel()
	if err != nil {
		log.WithError(err).WithField("workspace_id", workspaceID).Warn("failed to list webhooks for dispatch")
		return
	}
	var hooks []domain.Webhook
	for _, hook := range targets {
		allowed, skipped := s.breaker.allow(hook.ID, len(payloads))
		if skipped > 0 {
			log.WithField("webhook_id", hook.ID).WithField("dropped", skipped).
				Warn("webhook deliveries dropped while the webhook was paused; trying it again")
		}
		if allowed {
			hooks = append(hooks, hook)
		}
	}
	if len(hooks) == 0 {
		return
	}
	s.start()

	timestamp := time.Now().UTC().Format(time.RFC3339)
	dropped := 0
	for _, payload := range payloads {
		body, err := json.Marshal(dto.WebhookDelivery{Event: event, Payload: payload, Timestamp: timestamp})
		if err != nil {
			log.WithError(err).Warn("failed to marshal webhook payload")
			continue
		}
		if !s.queue.enqueue(webhookJob{ctx: system, workspaceID: workspaceID, event: event, hooks: hooks, body: body}) {
			dropped++
		}
	}
	if dropped > 0 {
		// One line per call, without URLs or secrets.
		log.WithField("workspace_id", workspaceID).WithField("event", event).WithField("dropped", dropped).
			WithField("webhooks", len(hooks)).WithField("dropped_total", s.queue.dropped.Load()).
			Warn("webhook deliveries dropped: the delivery queue is full")
	}
}

// webhookOutcome tells deliver what one attempted delivery means for the
// webhook's circuit breaker.
type webhookOutcome int

const (
	webhookOutcomeSkipped   webhookOutcome = iota // no attempt or no verdict: guard, secret or request setup failed
	webhookOutcomeFailed                          // timed out, refused the connection or answered non-2xx
	webhookOutcomeDelivered                       // answered 2xx
	webhookOutcomeStop                            // the privacy guard refused: send no more hooks for this job
)

// deliver sends a job to its webhooks in order. A paused webhook is skipped,
// and one that keeps failing is paused; no receiver is waited for beyond its
// own delivery timeout.
func (s *WebhookService) deliver(job webhookJob) {
	for _, hook := range job.hooks {
		if s.stop.Err() != nil {
			return
		}
		allowed, skipped := s.breaker.allow(hook.ID, 1)
		if skipped > 0 {
			log.WithField("webhook_id", hook.ID).WithField("dropped", skipped).
				Warn("webhook deliveries dropped while the webhook was paused; trying it again")
		}
		if !allowed {
			continue
		}
		outcome := s.send(job, hook)
		if s.stop.Err() != nil {
			// A request aborted by Close is not the receiver's failure, so it
			// must not count toward the breaker.
			return
		}
		switch outcome {
		case webhookOutcomeStop:
			return
		case webhookOutcomeFailed:
			if s.breaker.record(hook.ID, false) {
				log.WithField("webhook_id", hook.ID).WithField("failures", webhookBreakerFailures).
					WithField("paused_for", webhookBreakerCooldown.String()).
					Warn("webhook paused after repeated failed deliveries")
			}
		case webhookOutcomeDelivered:
			s.breaker.record(hook.ID, true)
		}
	}
}

// send attempts one delivery to one webhook, holding the privacy guard until
// the request ends.
func (s *WebhookService) send(job webhookJob, hook domain.Webhook) webhookOutcome {
	logger := log.WithField("webhook_id", hook.ID).WithField("event", job.event)
	ctx, cancel := context.WithTimeout(job.ctx, webhookDeliveryTimeout)
	defer cancel()
	stop := context.AfterFunc(s.stop, cancel)
	defer stop()

	// Queued payloads must recheck privacy at delivery, holding the
	// transition lock until the external request is finished.
	guard, err := s.webhookRepo.BeginPublicOperation(ctx, job.workspaceID)
	if err != nil {
		if errors.Is(err, repository.ErrPrivateWorkspaceOperation) {
			logger.Debug("webhook delivery skipped: workspace has private teams")
			return webhookOutcomeStop
		}
		logger.WithError(err).Warn("webhook delivery dropped: privacy guard failed")
		return webhookOutcomeSkipped
	}
	defer func() { _ = guard.Rollback() }()
	secret, err := crypto.Decrypt(hook.Secret, s.encryptionKey)
	if err != nil {
		logger.WithError(err).Warn("failed to decrypt webhook secret")
		return webhookOutcomeSkipped
	}

	signature := crypto.HMACSha256(job.body, secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(job.body))
	if err != nil {
		logger.WithError(withoutURL(err)).Warn("failed to create webhook request")
		return webhookOutcomeSkipped
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	req.Header.Set("X-Webhook-Event", job.event)

	resp, err := s.client.Do(req)
	if err != nil {
		logger.WithError(withoutURL(err)).Warn("webhook delivery failed")
		return webhookOutcomeFailed
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		logger.WithField("status", resp.StatusCode).Warn("webhook receiver rejected delivery")
		return webhookOutcomeFailed
	}
	return webhookOutcomeDelivered
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
