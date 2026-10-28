// Package agent normalizes agentic coding CLIs behind a safe argv-based interface.
package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModeAutonomous  Mode = "autonomous"
)

type Metadata struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	DefaultImage    string   `json:"default_image"`
	RequiredSecrets []string `json:"required_secrets"`
	// OptionalSecrets are offered at machine creation but may be omitted, for example
	// when the CLI is signed in with a subscription instead of an API key.
	OptionalSecrets []string `json:"optional_secrets"`
	SupportedModes  []Mode   `json:"supported_modes"`
	Custom          bool     `json:"custom"`
}

type RunInput struct {
	Mode               Mode            `json:"mode"`
	Prompt             string          `json:"prompt"`
	WorkspacePath      string          `json:"workspace_path"`
	AcceptanceCriteria []string        `json:"acceptance_criteria"`
	TestCommand        []string        `json:"test_command"`
	ExtraArgs          []string        `json:"extra_args"`
	Config             json.RawMessage `json:"config"`
	Model              string          `json:"model,omitempty"`
	Thinking           string          `json:"thinking,omitempty"`
	// OpenAIBaseURL points Codex at the run's model server; its key arrives as
	// KUAYLE_MODEL_API_KEY through scoped secrets, never argv.
	OpenAIBaseURL string `json:"openai_base_url,omitempty"`
	// Session is set by Kuayle for agent task attempts only, never from HTTP.
	Session *SessionInput `json:"-"`
}

// SessionInput keeps a harness's session transcripts in a per-task directory
// on the machine's workspace volume, so a later attempt can resume natively.
// Only transcripts are persisted: configuration and credentials stay on the
// container's ephemeral home.
type SessionInput struct {
	Dir string
	// ResumeID resumes that native session; NewID names a new one when the
	// harness lets Kuayle choose (Claude Code). Codex reports its own ID.
	ResumeID string
	NewID    string
}

type Invocation struct {
	Image       string            `json:"image"`
	Argv        []string          `json:"argv"`
	Environment map[string]string `json:"environment,omitempty"`
	WorkingDir  string            `json:"working_dir"`
	Interactive bool              `json:"interactive"`
	SecretNames []string          `json:"secret_names,omitempty"`
}

type Event struct {
	Type    string          `json:"type"`
	Message string          `json:"message,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Artifact struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	ContentType string `json:"content_type,omitempty"`
}

type RunResult struct {
	Status         string     `json:"status"`
	Summary        string     `json:"summary"`
	ChangedFiles   []string   `json:"changed_files"`
	Commits        []string   `json:"commits"`
	Branch         string     `json:"branch,omitempty"`
	PullRequestURL string     `json:"pull_request_url,omitempty"`
	TestsRun       []string   `json:"tests_run"`
	TestStatus     string     `json:"test_status"`
	RiskNotes      []string   `json:"risk_notes"`
	Artifacts      []Artifact `json:"artifacts"`
}

type Provider interface {
	Metadata() Metadata
	BuildInvocation(RunInput) (Invocation, error)
	ParseEvents([]byte) []Event
	ParseResult(stdout, stderr string, exitCode int) RunResult
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		r.providers[provider.Metadata().ID] = provider
	}
	return r
}

func (r *Registry) Get(id string) (Provider, bool) {
	provider, ok := r.providers[id]
	return provider, ok
}

func (r *Registry) List() []Provider {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	providers := make([]Provider, 0, len(ids))
	for _, id := range ids {
		providers = append(providers, r.providers[id])
	}
	return providers
}

func validateInput(metadata Metadata, input RunInput) error {
	if strings.TrimSpace(input.Prompt) == "" {
		return fmt.Errorf("agent prompt is required")
	}
	if input.Mode != ModeInteractive && input.Mode != ModeAutonomous {
		return fmt.Errorf("unsupported agent mode %q", input.Mode)
	}
	for _, supported := range metadata.SupportedModes {
		if input.Mode == supported {
			return nil
		}
	}
	return fmt.Errorf("provider %s does not support %s mode", metadata.ID, input.Mode)
}

func defaultResult(stdout, stderr string, exitCode int) RunResult {
	status := "succeeded"
	if exitCode != 0 {
		status = "failed"
	}
	summary := finalMessage(stdout)
	if summary == "" {
		summary = strings.TrimSpace(stdout)
	}
	if summary == "" {
		summary = strings.TrimSpace(stderr)
	}
	if len(summary) > 4000 {
		summary = summary[:4000]
	}
	return RunResult{
		Status:       status,
		Summary:      summary,
		ChangedFiles: []string{},
		Commits:      []string{},
		TestsRun:     []string{},
		TestStatus:   "not_run",
		RiskNotes:    []string{},
		Artifacts:    []Artifact{},
	}
}

func parseJSONLines(raw []byte) []Event {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}
		var envelope struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(line), &envelope)
		if envelope.Type == "" {
			envelope.Type = "provider.event"
		}
		events = append(events, Event{Type: envelope.Type, Message: envelope.Message, Payload: json.RawMessage(line)})
	}
	return events
}

// finalMessage returns the agent's last reply from a JSON event stream: the
// last Codex agent_message or Claude Code's final result. It returns "" for
// output that is not such a stream, so callers fall back to the raw text.
func finalMessage(stdout string) string {
	last := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Type   string `json:"type"`
			Result string `json:"result"`
			Item   struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		switch {
		case event.Type == "item.completed" && event.Item.Type == "agent_message" && strings.TrimSpace(event.Item.Text) != "":
			last = strings.TrimSpace(event.Item.Text)
		case event.Type == "result" && strings.TrimSpace(event.Result) != "":
			last = strings.TrimSpace(event.Result)
		}
	}
	return last
}
