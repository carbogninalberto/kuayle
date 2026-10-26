package domain

import (
	"encoding/json"
	"errors"
)

// AgentRunSecretSnapshot is an encrypted internal handoff, never an API response.
// Public runtime configuration must not be redacted as if it were a credential.
type AgentRunSecretSnapshot struct {
	Version     int               `json:"version"`
	Environment map[string]string `json:"environment"`
	Redactions  map[string]string `json:"redactions"`
}

func DecodeAgentRunSecretSnapshot(payload string) (*AgentRunSecretSnapshot, error) {
	var snapshot AgentRunSecretSnapshot
	if json.Unmarshal([]byte(payload), &snapshot) == nil && snapshot.Version == 1 && snapshot.Environment != nil && snapshot.Redactions != nil {
		for name, value := range snapshot.Redactions {
			if original, ok := snapshot.Environment[name]; !ok || original != value {
				return nil, errors.New("invalid native credential snapshot")
			}
		}
		return &snapshot, nil
	}
	// Preserve the redaction policy used by legacy output for deterministic replay.
	var legacy map[string]string
	if json.Unmarshal([]byte(payload), &legacy) != nil || legacy == nil {
		return nil, errors.New("invalid native credential snapshot")
	}
	return &AgentRunSecretSnapshot{Environment: legacy, Redactions: legacy}, nil
}
