package machine

import (
	"encoding/json"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// agentEventRecord returns the event type and jsonb payload stored for one
// parsed provider event. PostgreSQL rejects NUL in text and the \u0000 escape
// in jsonb (22P05), and one such event used to stop the whole run, so both are
// stripped here. A payload that cannot be cleaned falls back to its message.
func agentEventRecord(parsed agent.Event) (string, json.RawMessage) {
	var payload json.RawMessage
	if len(parsed.Payload) > 0 {
		if cleaned, err := sanitize.JSONWithoutNUL(parsed.Payload); err == nil {
			payload = cleaned
		}
	}
	if len(payload) == 0 {
		payload, _ = json.Marshal(map[string]any{"message": sanitize.StripNUL(parsed.Message)})
	}
	eventType := sanitize.StripNUL(parsed.Type)
	if len(eventType) == 0 || len(eventType) > 128 {
		eventType = "provider.event"
	}
	return eventType, payload
}
