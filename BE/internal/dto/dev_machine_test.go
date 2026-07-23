package dto

import (
	"encoding/json"
	"testing"
)

func TestTerminalSessionLaunchResponseOmitsMissingSession(t *testing.T) {
	encoded, err := json.Marshal(TerminalSessionLaunchResponse{Status: "pending", RetryAfterSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["session"]; exists {
		t.Fatalf("pending terminal response contains a fabricated session: %s", encoded)
	}
}
