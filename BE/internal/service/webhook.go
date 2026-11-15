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
	// webhookDeliveryTimeout bounds one worker turn: claiming a delivery,
	// the privacy guard, the request and recording its outcome.
	webhookDeliveryTimeout = 30 * time.Second
	// webhookListTimeout bounds listing a workspace's webhooks and storing
	// the deliveries of one change.
	webhookListTimeout = 10 * time.Second
	// webhookWorkers bounds concurrent deliveries per instance and so the
	// database connections they hold for a turn, well below the server pool
	// of 25. Deliveries do not use the publication slots.
	webhookWorkers = 4
	// webhookPollInterval is how often idle workers look for deliveries that
	// became due (retries) or were stored by another instance. Deliveries
	// stored by this instance wake a worker at once.
	webhookPollInterval = 2 * time.Second
	// webhookQueueJobs bounds the pending deliveries in all workspaces.
	webhookQueueJobs = 16384
	// webhookQueueBytes bounds the pending payload bytes in all workspaces.
	webhookQueueBytes = 128 << 20
	// webhookWorkspaceJobs bounds one workspace's pending deliveries to one
	// sixteenth of the global bound: filling one workspace never drops
	// another workspace's deliveries, and 16 full workspaces are needed to
	// fill the global bound.
	webhookWorkspaceJobs = webhookQueueJobs / 16
	// webhookWorkspaceBytes bounds one workspace's pending payload bytes to
	// one sixteenth of the global byte bound.
	webhookWorkspaceBytes = webhookQueueBytes / 16
	// webhookWebhookJobs bounds one webhook's pending deliveries to a
	// quarter of its workspace's, so a dead receiver does not use up its
	// workspace's share while the other webhooks keep receiving.
	webhookWebhookJobs = webhookWorkspaceJobs / 4
	// webhookMaxAge gives up a delivery that is still pending this long
	// after it was stored, for example while its webhook stays paused.
	webhookMaxAge = 24 * time.Hour
	// webhookMaxAttempts is the number of attempts before a delivery is
	// given up: the first one and one per retry delay.
	webhookMaxAttempts = 6
	// webhookBreakerFailures is the number of consecutive failed deliveries
	// that pause a webhook.
	webhookBreakerFailures = 3
	// webhookBreakerCooldown is how long a paused webhook is skipped before
	// one delivery probes it again.
	webhookBreakerCooldown = 5 * time.Minute
	// webhookDeliveryRetention is how long finished deliveries stay in the
	// delivery log.
	webhookDeliveryRetention = 7 * 24 * time.Hour
	// webhookDeliveryLogLimit is how many recent deliveries the log returns.
	webhookDeliveryLogLimit = 50
	webhookPurgeBatch       = 500
)

// defaultWebhookRetryDelays are the waits after each failed attempt before the
// next one; a failure after the last delay gives the delivery up.
var defaultWebhookRetryDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// webhookRetryDelay returns how long to wait after the attempt-th failed
// attempt (1-based), and false when that attempt was the last one.
func webhookRetryDelay(delays []time.Duration, attempt int) (time.Duration, bool) {
	if attempt < 1 || attempt > len(delays) {
		return 0, false
	}
	return delays[attempt-1], true
}

// Failure codes stored as a delivery's last error, besides the repository's
// codes for deliveries given up unsent. They name no URL.
const (
	webhookErrorBadStatus         = "bad_status"
	webhookErrorTimeout           = "timeout"
	webhookErrorConnection        = "connection_failed"
	webhookErrorAddressNotPublic  = "address_not_public"
	webhookErrorInvalidURL        = "invalid_url"
	webhookErrorSecretUnavailable = "secret_unavailable"
)

// ErrWebhookNotFound is returned for a webhook that does not exist or belongs
// to another workspace.
var ErrWebhookNotFound = errors.New("webhook not found")

// webhookLimits bounds the pending deliveries, counted in the store.
type webhookLimits struct {
	jobs, bytes                   int // pending in all workspaces
	workspaceJobs, workspaceBytes int // pending per workspace
	webhookJobs                   int // pending per webhook
}

var defaultWebhookLimits = webhookLimits{
	jobs:           webhookQueueJobs,
	bytes:          webhookQueueBytes,
	workspaceJobs:  webhookWorkspaceJobs,
	workspaceBytes: webhookWorkspaceBytes,
	webhookJobs:    webhookWebhookJobs,
}

func (l webhookLimits) store() repository.WebhookDeliveryLimits {
	return repository.WebhookDeliveryLimits{Jobs: l.jobs, Bytes: l.bytes, WorkspaceJobs: l.workspaceJobs, WorkspaceBytes: l.workspaceBytes, WebhookJobs: l.webhookJobs}
}

var _ webhookEmitter = (*WebhookService)(nil)

// WebhookService manages workspace webhooks and delivers their events.
//
// A change is stored as one delivery per subscribed webhook when it is
// emitted, after the write that made it is committed. Workers in every
// instance send stored deliveries: each workspace has one delivery in flight
// at a time across instances, workspaces take turns, and a failure is retried
// with backoff until it is given up. A webhook that keeps failing is paused,
// and the pause is stored, so every instance honors it.
type WebhookService struct {
	webhookRepo   *repository.WebhookRepository
	encryptionKey []byte
	client        *http.Client
	// Tests may change these before Start or the first dispatch.
	workers          int
	limits           webhookLimits
	retryDelays      []time.Duration
	breakerThreshold int
	breakerCooldown  time.Duration
	maxAge           time.Duration
	pollInterval     time.Duration

	kick chan struct{} // wakes one idle worker

	mu      sync.Mutex
	started bool
	closed  bool
	// stop aborts in-flight deliveries on Close; wg waits for the workers.
	stop   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewWebhookService(webhookRepo *repository.WebhookRepository, jwtSecret string) *WebhookService {
	stop, cancel := context.WithCancel(context.Background())
	return &WebhookService{
		webhookRepo:      webhookRepo,
		encryptionKey:    crypto.DeriveKey(jwtSecret + ":webhook"),
		client:           newWebhookClient(false),
		workers:          webhookWorkers,
		limits:           defaultWebhookLimits,
		retryDelays:      defaultWebhookRetryDelays,
		breakerThreshold: webhookBreakerFailures,
		breakerCooldown:  webhookBreakerCooldown,
		maxAge:           webhookMaxAge,
		pollInterval:     webhookPollInterval,
		kick:             make(chan struct{}, 1),
		stop:             stop,
		cancel:           cancel,
	}
}

// Start runs the workers until Close. The server starts them at boot, so
// deliveries stored before a restart are sent; storing a delivery also starts
// them. It is idempotent.
func (s *WebhookService) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	s.wg.Add(s.workers + 1)
	for range s.workers {
		go func() {
			defer s.wg.Done()
			s.work()
		}()
	}
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop.Done():
				return
			case <-ticker.C:
				s.wake()
			}
		}
	}()
	s.wake()
}

// wake lets one idle worker look for a due delivery.
func (s *WebhookService) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// work sends deliveries while there are any, then waits to be woken.
func (s *WebhookService) work() {
	for {
		select {
		case <-s.stop.Done():
			return
		case <-s.kick:
		}
		for s.stop.Err() == nil && s.deliverNext() {
			s.wake() // there may be more for another worker
		}
	}
}

// Close stops the workers. A delivery in flight is aborted and stays pending,
// without a counted attempt, for this or another instance to send later.
// Deliveries can still be stored after Close.
func (s *WebhookService) Close() {
	s.mu.Lock()
	alreadyClosed := s.closed
	s.closed = true
	s.mu.Unlock()
	if !alreadyClosed {
		s.cancel()
	}
	s.wg.Wait()
}

// RunCleanup purges finished deliveries older than the retention every
// interval until ctx ends.
func (s *WebhookService) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.PurgeDeliveries(ctx); err != nil {
				log.WithError(err).Warn("failed to purge old webhook deliveries")
			}
		}
	}
}

// PurgeDeliveries deletes delivered and failed deliveries older than the
// retention and returns how many it deleted.
func (s *WebhookService) PurgeDeliveries(ctx context.Context) (int, error) {
	return s.webhookRepo.PurgeDeliveries(ctx, webhookDeliveryRetention, webhookPurgeBatch)
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

// Create stores a webhook. In a workspace that has or had a private team it
// returns repository.ErrPrivateWorkspaceOperation: webhooks are off there.
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

// Update changes the fields the request names. Enabling an inactive webhook in
// a workspace that has or had a private team returns
// repository.ErrPrivateWorkspaceOperation.
func (s *WebhookService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateWebhookRequest) (*domain.Webhook, error) {
	w, err := s.webhookRepo.GetByID(ctx, id)
	if err != nil || w == nil {
		return nil, ErrWebhookNotFound
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
		return ErrWebhookNotFound
	}
	return s.webhookRepo.Delete(ctx, id)
}

// ListDeliveries returns the webhook's recent deliveries, newest first, or
// ErrWebhookNotFound when the webhook is not in the request's workspace.
func (s *WebhookService) ListDeliveries(ctx context.Context, id uuid.UUID) ([]domain.WebhookDelivery, error) {
	w, err := s.webhookRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, ErrWebhookNotFound
	}
	return s.webhookRepo.ListDeliveries(ctx, id, webhookDeliveryLogLimit)
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
	if slices.Contains(webhookCommentEvents, event) || !slices.Contains(WebhookEvents, event) {
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
	s.CommentEvent(ctx, workspaceID, WebhookEventCommentCreated, issue, comment, actorID)
}

// CommentEvent delivers comment.created or comment.updated with the comment
// and a reference to its issue.
func (s *WebhookService) CommentEvent(ctx context.Context, workspaceID uuid.UUID, event string, issue *domain.Issue, comment *domain.Comment, actorID uuid.UUID) {
	if s == nil || issue == nil || comment == nil {
		return
	}
	if !slices.Contains(webhookCommentEvents, event) {
		log.WithField("event", event).Debug("ignoring unknown webhook comment event")
		return
	}
	s.dispatch(ctx, workspaceID, event, []any{dto.WebhookCommentPayload{
		Comment: dto.WebhookComment(*comment),
		Issue:   dto.WebhookIssueRef{ID: issue.ID, Identifier: issue.Identifier, TeamID: issue.TeamID},
		ActorID: actorPayloadID(actorID),
	}})
}

// Dispatch sends an event payload to all active webhooks subscribed to the event.
// Payloads are signed with HMAC-SHA256 in the X-Webhook-Signature header.
//
// The body is marshalled and stored before Dispatch returns; workers send it.
// Deliveries outlive the caller's request and do not run with the caller's
// access: the subscriber is the workspace, not the actor, and the privacy
// guard taken per delivery is the boundary. The workspace's webhooks are
// listed once per call.
func (s *WebhookService) Dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payload interface{}) {
	if s == nil {
		return
	}
	s.dispatch(ctx, workspaceID, event, []any{payload})
}

// dispatch lists the workspace's deliverable webhooks once, marshals each
// payload and stores one delivery per payload and webhook, as far as the
// bounds allow. It never fails the caller: a delivery the store cannot take
// is dropped and logged.
func (s *WebhookService) dispatch(ctx context.Context, workspaceID uuid.UUID, event string, payloads []any) {
	system := domain.PublicContext(context.WithoutCancel(ctx))
	storeCtx, cancel := context.WithTimeout(system, webhookListTimeout)
	defer cancel()
	hooks, err := s.webhookRepo.ListDeliverable(storeCtx, workspaceID, event)
	if err != nil {
		log.WithError(err).WithField("workspace_id", workspaceID).Warn("failed to list webhooks for dispatch")
		return
	}
	if len(hooks) == 0 {
		return
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	deliveries := make([]repository.NewWebhookDelivery, 0, len(payloads)*len(hooks))
	for _, payload := range payloads {
		body, err := json.Marshal(dto.WebhookDelivery{Event: event, Payload: payload, Timestamp: timestamp})
		if err != nil {
			log.WithError(err).Warn("failed to marshal webhook payload")
			continue
		}
		for _, hook := range hooks {
			deliveries = append(deliveries, repository.NewWebhookDelivery{WebhookID: hook.ID, Body: body})
		}
	}
	stored, err := s.webhookRepo.EnqueueDeliveries(storeCtx, workspaceID, event, deliveries, s.limits.store())
	if err != nil {
		log.WithError(err).WithField("workspace_id", workspaceID).WithField("event", event).WithField("dropped", len(deliveries)).
			Warn("failed to store webhook deliveries")
		return
	}
	if dropped := len(deliveries) - stored; dropped > 0 {
		// One line per call, without URLs or secrets.
		log.WithField("workspace_id", workspaceID).WithField("event", event).WithField("dropped", dropped).
			WithField("webhooks", len(hooks)).Warn("webhook deliveries dropped: the delivery queue is full")
	}
	if stored > 0 {
		s.Start()
		s.wake()
	}
}

// deliverNext takes one workspace's turn: it sends that workspace's earliest
// due delivery and records the outcome. It reports whether there was a turn
// to take, so the worker looks for another one at once.
func (s *WebhookService) deliverNext() bool {
	ctx, cancel := context.WithTimeout(domain.PublicContext(s.stop), webhookDeliveryTimeout)
	defer cancel()
	claim, err := s.webhookRepo.ClaimDelivery(ctx, s.maxAge)
	if err != nil {
		if s.stop.Err() == nil {
			log.WithError(err).Warn("failed to claim a webhook delivery")
		}
		return false
	}
	if claim == nil {
		return false
	}
	defer claim.Release()
	logger := log.WithField("workspace_id", claim.WorkspaceID)
	if claim.Private > 0 {
		logger.WithField("dropped", claim.Private).Debug("webhook deliveries dropped: the workspace has private teams")
	}
	if claim.Expired > 0 {
		logger.WithField("dropped", claim.Expired).WithField("max_age", s.maxAge.String()).
			Warn("webhook deliveries dropped: they waited too long")
	}
	if claim.Skipped != "" {
		logger.WithField("webhook_id", claim.SkippedWebhook).WithField("reason", claim.Skipped).
			Debug("webhook delivery dropped: the webhook no longer takes it")
	}
	job := claim.Delivery
	if job == nil {
		return true
	}
	attempt := s.send(ctx, job)
	if s.stop.Err() != nil {
		// Aborted by Close: not the receiver's failure. The delivery stays
		// pending without a counted attempt.
		return false
	}
	pausedAfter, err := claim.Finish(ctx, attempt, repository.WebhookBreaker{Threshold: s.breakerThreshold, Cooldown: s.breakerCooldown})
	if err != nil {
		log.WithError(err).WithField("webhook_id", job.WebhookID).WithField("delivery_id", job.ID).Warn("failed to record a webhook delivery")
		return false
	}
	if pausedAfter > 0 {
		log.WithField("webhook_id", job.WebhookID).WithField("failures", pausedAfter).
			WithField("paused_for", s.breakerCooldown.String()).
			Warn("webhook paused after repeated failed deliveries")
	}
	return true
}

// send attempts one delivery while the claim holds the workspace's privacy
// guard, and classifies the outcome.
func (s *WebhookService) send(ctx context.Context, job *repository.WebhookDeliveryJob) repository.WebhookAttempt {
	logger := log.WithField("webhook_id", job.WebhookID).WithField("event", job.Event).WithField("delivery_id", job.ID)
	secret, err := crypto.Decrypt(job.Secret, s.encryptionKey)
	if err != nil {
		logger.WithError(err).Warn("failed to decrypt webhook secret")
		return repository.WebhookAttempt{Error: webhookErrorSecretUnavailable}
	}
	body := []byte(job.Body)
	signature := crypto.HMACSha256(body, secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.URL, bytes.NewReader(body))
	if err != nil {
		logger.WithError(withoutURL(err)).Warn("failed to create webhook request")
		return repository.WebhookAttempt{Error: webhookErrorInvalidURL}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	req.Header.Set("X-Webhook-Event", job.Event)
	req.Header.Set("X-Webhook-Delivery", job.ID.String())

	attempt := repository.WebhookAttempt{Sent: true, Breaker: true}
	resp, err := s.client.Do(req)
	if err != nil {
		if s.stop.Err() != nil {
			return attempt
		}
		logger.WithError(withoutURL(err)).Warn("webhook delivery failed")
		attempt.Error = webhookTransportError(err)
	} else {
		resp.Body.Close()
		attempt.StatusCode = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			attempt.Delivered = true
			return attempt
		}
		logger.WithField("status", resp.StatusCode).Warn("webhook receiver rejected delivery")
		attempt.Error = webhookErrorBadStatus
	}
	attempt.RetryIn, _ = webhookRetryDelay(s.retryDelays, job.Attempts+1)
	return attempt
}

// webhookTransportError names a failed request without its URL.
func webhookTransportError(err error) string {
	if errors.Is(err, validate.ErrNonPublicAddress) {
		return webhookErrorAddressNotPublic
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return webhookErrorTimeout
	}
	return webhookErrorConnection
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
