package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// GitHubWebhookDeliveryRetention is how long a processed delivery is
	// remembered. GitHub offers manual redelivery for a few days only; after
	// the marker expires a captured delivery could be replayed again.
	GitHubWebhookDeliveryRetention = 7 * 24 * time.Hour
	// webhookDeliveryLease is how long an unfinished claim blocks the same
	// delivery. Handling takes seconds; a claim older than this belongs to a
	// request that died, and the delivery may be processed again.
	webhookDeliveryLease = 10 * time.Minute
	// maxWebhookDeliveryIDLength bounds X-GitHub-Delivery, which is a GUID.
	maxWebhookDeliveryIDLength = 128
)

// ValidWebhookDeliveryID reports whether id can identify a delivery: GitHub
// sends a GUID, so anything empty, long or not printable ASCII is refused.
func ValidWebhookDeliveryID(id string) bool {
	if id == "" || len(id) > maxWebhookDeliveryIDLength {
		return false
	}
	for _, r := range id {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

// HandleWebhookDelivery processes a signature-verified delivery at most once.
// The delivery ID is claimed before processing, so concurrent copies cannot
// both run, and the claim is released when processing fails: GitHub redelivers
// under the same ID, and a failed delivery must stay redeliverable. duplicate
// is true when the delivery was already processed (or is being processed) and
// nothing was done.
//
// The claim lives in PostgreSQL, like everything the processors write. When it
// cannot be recorded the delivery fails instead of running unprotected; GitHub
// then shows it as failed and it can be redelivered.
func (s *GitHubService) HandleWebhookDelivery(ctx context.Context, eventType, deliveryID string, payload []byte) (duplicate bool, err error) {
	if !ValidWebhookDeliveryID(deliveryID) {
		return false, fmt.Errorf("invalid webhook delivery ID")
	}
	// Markers are kept per installation. The ID comes from the payload the
	// signature covers; events without an installation share scope 0.
	var scope struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	_ = json.Unmarshal(payload, &scope)
	installationID := scope.Installation.ID

	claimed, err := s.ghRepo.ClaimWebhookDelivery(ctx, installationID, deliveryID, webhookDeliveryLease)
	if err != nil {
		return false, fmt.Errorf("recording webhook delivery: %w", err)
	}
	fields := log.Fields{"installation_id": installationID, "delivery_id": deliveryID, "event": eventType}
	if !claimed {
		log.WithFields(fields).Info("Ignored a GitHub webhook delivery that was already received")
		return true, nil
	}
	processed := false
	// Also runs when a processor panics. The request context may already be
	// cancelled, and the marker must still be settled.
	defer func() {
		if processed {
			return
		}
		settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if releaseErr := s.ghRepo.ReleaseWebhookDelivery(settle, installationID, deliveryID); releaseErr != nil {
			log.WithFields(fields).WithError(releaseErr).Warn("Failed GitHub webhook delivery stays claimed; it can be redelivered once the claim expires")
		}
	}()
	if err := s.HandleWebhookEvent(ctx, eventType, payload); err != nil {
		return false, err
	}
	processed = true
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.ghRepo.CompleteWebhookDelivery(settle, installationID, deliveryID); err != nil {
		// The event was handled; only the replay guard for it is incomplete.
		log.WithFields(fields).WithError(err).Warn("GitHub webhook delivery was processed but not marked; a replay is accepted once the claim expires")
	}
	return false, nil
}

// RunWebhookDeliveryCleanup removes expired delivery markers every interval.
func (s *GitHubService) RunWebhookDeliveryCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.ghRepo.DeleteExpiredWebhookDeliveries(ctx, GitHubWebhookDeliveryRetention); err != nil {
				log.WithError(err).Warn("Failed to clean up GitHub webhook delivery markers")
			}
		}
	}
}
