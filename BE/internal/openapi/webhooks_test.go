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

// Creating a webhook documents the delivered event names as the only values of
// events. Updating leaves the names to the service, because the stored list
// sent back unchanged is accepted, so its events need only be non-empty.
func TestWebhookRequestsDocumentDeliveredEventNames(t *testing.T) {
	built, err := Build()
	require.NoError(t, err)
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					MinItems *int `json:"minItems"`
					Items    struct {
						Enum []string `json:"enum"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(built, &doc))
	create := doc.Components.Schemas["CreateWebhookRequest"].Properties["events"]
	require.Equal(t, service.WebhookEvents, create.Items.Enum, "CreateWebhookRequest")
	update := doc.Components.Schemas["UpdateWebhookRequest"].Properties["events"]
	require.NotNil(t, update.MinItems, "UpdateWebhookRequest events minItems")
	require.Equal(t, 1, *update.MinItems, "UpdateWebhookRequest")
	require.Empty(t, update.Items.Enum, "UpdateWebhookRequest events must not be an enum")
}
