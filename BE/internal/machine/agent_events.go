package machine

import (
	"encoding/json"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// agentEventRecord prepares a provider event for persistence. Keep the event
// type used for routing and fall back to the message if the payload is invalid.
func agentEventRecord(parsed agent.Event) (string, json.RawMessage) {
	payload := parsed.Payload
	if !json.Valid(payload) {
		payload, _ = json.Marshal(map[string]any{"message": parsed.Message})
	}
	eventType := sanitize.StripNUL(parsed.Type)
	if len(eventType) == 0 || len(eventType) > 128 {
		eventType = "provider.event"
	}
	return eventType, payload
}
