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

// Creating and updating a webhook document the delivered event names as the
// only values of events.
func TestWebhookRequestsDocumentDeliveredEventNames(t *testing.T) {
	built, err := Build()
	require.NoError(t, err)
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Items struct {
						Enum []string `json:"enum"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(built, &doc))
	for _, name := range []string{"CreateWebhookRequest", "UpdateWebhookRequest"} {
		require.Equal(t, service.WebhookEvents, doc.Components.Schemas[name].Properties["events"].Items.Enum, name)
	}
}
