package openapi

import (
	"encoding/json"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/stretchr/testify/require"
)

// The document lists exactly the events the service delivers.
func TestDocumentedWebhookEventsMatchDeliveredEvents(t *testing.T) {
	require.Equal(t, service.WebhookEvents, webhookEventNames())

	built, err := Build()
	require.NoError(t, err)
	var doc struct {
		Webhooks map[string]json.RawMessage `json:"webhooks"`
	}
	require.NoError(t, json.Unmarshal(built, &doc))
	names := make([]string, 0, len(doc.Webhooks))
	for name := range doc.Webhooks {
		names = append(names, name)
	}
	require.ElementsMatch(t, service.WebhookEvents, names)
}
