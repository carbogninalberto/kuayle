package service

import "context"

// Guarded outbound operations (AI expansion, the skill writer, Telegram) hold a
// pooled database connection for the publication lock while waiting on external
// HTTP. Bound them well below the server pool (25 connections) so slow providers
// cannot starve ordinary requests. Slots are taken before the connection.
// Webhook deliveries have their own bounded workers instead (see webhook.go).
const maxConcurrentPublications = 8

var publicationSlots = make(chan struct{}, maxConcurrentPublications)

func acquirePublicationSlot(ctx context.Context) (func(), error) {
	select {
	case publicationSlots <- struct{}{}:
		return func() { <-publicationSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
