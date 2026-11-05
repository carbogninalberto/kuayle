package service

import (
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelChoice is the harness, model and model server the approver picked on
// the approval card. Empty fields keep what the assistant proposed; the server
// counts only together with a model, since two servers may list the same name.
type ModelChoice struct {
	Harness       string     `json:"harness,omitempty"`
	Model         string     `json:"model,omitempty"`
	ModelServerID *uuid.UUID `json:"model_server_id,omitempty"`
}

// over applies the approver's choice to the assistant's proposal and returns
// the harness, model and the server picked for that model (nil: resolve it). A
// different harness never inherits the proposed model, which belongs to another
// harness.
func (c ModelChoice) over(harness, model string) (string, string, *uuid.UUID) {
	chosen, picked := strings.TrimSpace(c.Harness), strings.TrimSpace(c.Model)
	server := c.ModelServerID
	if picked == "" {
		server = nil
	}
	if chosen != "" && chosen != harness {
		return chosen, picked, server
	}
	if picked != "" {
		return harness, picked, server
	}
	return harness, model, nil
}

// findModelServer returns the server with the ID, or nil.
func findModelServer(servers []dto.ModelServerResponse, id uuid.UUID) *dto.ModelServerResponse {
	for i := range servers {
		if servers[i].ID == id {
			return &servers[i]
		}
	}
	return nil
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
// level, and the server when it is not the default, of the chosen server, else
// of the server that offers it. An unknown chosen server is kept so that
// validation rejects it rather than silently running another server.
func chatModel(options *AgentTaskOptions, harness, model string, chosen *uuid.UUID) (string, string, *uuid.UUID) {
	var defaultModel, thinking string
	for _, server := range options.ModelServers {
		if server.IsDefault && containsString(server.Harnesses, harness) {
			defaultModel, thinking = server.DefaultModel, server.Thinking
		}
	}
	if model == "" {
		return defaultModel, thinking, nil
	}
	var server *dto.ModelServerResponse
	if chosen != nil {
		if server = findModelServer(options.ModelServers, *chosen); server == nil {
			return model, "", chosen
		}
	} else {
		server = serverForModel(options.ModelServers, harness, model, nil)
	}
	if server == nil {
		// No server offers it: validation rejects the request.
		return model, "", nil
	}
	if server.IsDefault {
		return model, server.Thinking, nil
	}
	return model, server.Thinking, &server.ID
}

// retryModel resolves the model, thinking level and server of a follow-up
// attempt. No model keeps the task's; the same model on the task's own server
// keeps its thinking level too. Otherwise the chosen server runs it, else the
// one that offers it (the task's own first). The default server stays implicit
// unless the task was pinned to a server. A chosen server that does not exist
// is kept for validation to reject.
func retryModel(servers []dto.ModelServerResponse, task *domain.AgentTask, model string, chosen *uuid.UUID) (string, string, *uuid.UUID) {
	if model == "" {
		return task.Model, task.Thinking, task.ModelServerID
	}
	var server *dto.ModelServerResponse
	if chosen != nil {
		server = findModelServer(servers, *chosen)
		if server == nil {
			return model, "", chosen
		}
		current := task.ModelServerID != nil && *task.ModelServerID == server.ID || task.ModelServerID == nil && server.IsDefault
		if model == task.Model && current {
			return task.Model, task.Thinking, task.ModelServerID
		}
	} else {
		if model == task.Model {
			return task.Model, task.Thinking, task.ModelServerID
		}
		server = serverForModel(servers, task.Provider, model, task.ModelServerID)
	}
	if server == nil {
		// No server offers it: validation rejects the request.
		return model, "", nil
	}
	if server.IsDefault && task.ModelServerID == nil {
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
