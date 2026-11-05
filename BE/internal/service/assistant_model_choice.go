package service

import (
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

// chatModel resolves the model and thinking level of an agent started from
// chat: the explicit model when given, otherwise the default model server's
// model, when that server speaks the harness's protocol.
func chatModel(options *AgentTaskOptions, harness, model string) (string, string) {
	var defaultModel, thinking string
	for _, server := range options.ModelServers {
		if server.IsDefault && containsString(server.Harnesses, harness) {
			defaultModel, thinking = server.DefaultModel, server.Thinking
		}
	}
	if model != "" {
		return model, thinking
	}
	return defaultModel, thinking
}

// validModelName accepts an empty name (use the default) or a short printable one.
func validModelName(name string) bool {
	if utf8.RuneCountInString(name) > 200 || len(name) > 200 {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) }) < 0
}
