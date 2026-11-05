package service

import (
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelChoice is the harness and model the approver picked on the approval
// card. Empty fields keep what the assistant proposed.
type ModelChoice struct {
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
}

// over applies the approver's choice to the assistant's proposal. A different
// harness never inherits the proposed model, which belongs to another harness.
func (c ModelChoice) over(harness, model string) (string, string) {
	chosen, picked := strings.TrimSpace(c.Harness), strings.TrimSpace(c.Model)
	if chosen != "" && chosen != harness {
		return chosen, picked
	}
	if picked != "" {
		return harness, picked
	}
	return harness, model
}

// serverForModel picks the model server that can run the model on the harness:
// the preferred one, then the default, then any other that lists it. A server
// with no model list accepts any model. Nil means none offers it.
func serverForModel(servers []dto.ModelServerResponse, harness, model string, prefer *uuid.UUID) *dto.ModelServerResponse {
	var found *dto.ModelServerResponse
	rank := 0
	for i := range servers {
		server := &servers[i]
		if !containsString(server.Harnesses, harness) || (len(server.Models) > 0 && !containsString(server.Models, model)) {
			continue
		}
		score := 1
		if server.IsDefault {
			score = 2
		}
		if prefer != nil && server.ID == *prefer {
			score = 3
		}
		if score > rank {
			found, rank = server, score
		}
	}
	return found
}

// chatModel resolves the model, thinking level and model server of an agent
// started from chat. Without an explicit model it is the default model
// server's model and thinking, when that server speaks the harness's protocol
// (no server ID: the default applies). An explicit model takes the thinking
// level, and the server when it is not the default, of the server that offers it.
func chatModel(options *AgentTaskOptions, harness, model string) (string, string, *uuid.UUID) {
	var defaultModel, thinking string
	for _, server := range options.ModelServers {
		if server.IsDefault && containsString(server.Harnesses, harness) {
			defaultModel, thinking = server.DefaultModel, server.Thinking
		}
	}
	if model == "" {
		return defaultModel, thinking, nil
	}
	server := serverForModel(options.ModelServers, harness, model, nil)
	if server == nil {
		// No server offers it: validation rejects the request.
		return model, "", nil
	}
	if server.IsDefault {
		return model, server.Thinking, nil
	}
	return model, server.Thinking, &server.ID
}

// validModelName accepts an empty name (use the default) or a short printable one.
func validModelName(name string) bool {
	if utf8.RuneCountInString(name) > 200 || len(name) > 200 {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) }) < 0
}
