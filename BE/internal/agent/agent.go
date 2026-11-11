// Package agent normalizes agentic coding CLIs behind a safe argv-based interface.
package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
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
	Status  string `json:"status"`
	Summary string `json:"summary"`
	// FinalMessage reports that Summary is the final message the harness
	// reported for the agent, not a fallback to its raw output; a planning
	// step's plan is that message. SummaryTruncated reports that it was cut
	// to MaxSummaryBytes.
	FinalMessage     bool `json:"final_message"`
	SummaryTruncated bool `json:"summary_truncated"`
	// NoFinalMessage reports that the harness exited normally but the agent
	// never concluded, which is the only reason the run failed.
	// FailureReason is what the harness itself said went wrong, if anything.
	NoFinalMessage bool `json:"no_final_message,omitempty"`
	// UnansweredMessages are the sequence numbers of the owner's messages the
	// run received and never replied to. It is a note: the status is the
	// work's own.
	UnansweredMessages []int64    `json:"unanswered_messages,omitempty"`
	FailureReason      string     `json:"failure_reason,omitempty"`
	ChangedFiles       []string   `json:"changed_files"`
	Commits            []string   `json:"commits"`
	Branch             string     `json:"branch,omitempty"`
	PullRequestURL     string     `json:"pull_request_url,omitempty"`
	TestsRun           []string   `json:"tests_run"`
	TestStatus         string     `json:"test_status"`
	RiskNotes          []string   `json:"risk_notes"`
	Artifacts          []Artifact `json:"artifacts"`
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

// MaxSummaryBytes bounds a run's final summary. A reviewer's findings are its
// final message, so this matches the review findings limit
// (domain.MaxAgentReviewFindings) and long reviews are stored whole.
const MaxSummaryBytes = 16 << 10

// truncateSummary cuts a summary to MaxSummaryBytes on a UTF-8 boundary.
func truncateSummary(summary string) string {
	if len(summary) <= MaxSummaryBytes {
		return summary
	}
	summary = summary[:MaxSummaryBytes]
	for !utf8.ValidString(summary) {
		summary = summary[:len(summary)-1]
	}
	return summary
}

func defaultResult(stdout, stderr string, exitCode int) RunResult {
	status := "succeeded"
	if exitCode != 0 {
		status = "failed"
	}
	summary, final := finalMessageAfterTools(stdout)
	if summary == "" {
		summary = plainOutput(stdout)
	}
	// Standard error explains a failure; on a normal exit it is only noise.
	if summary == "" && exitCode != 0 {
		summary = strings.TrimSpace(stderr)
	}
	return RunResult{
		Status:           status,
		Summary:          truncateSummary(summary),
		FinalMessage:     final,
		SummaryTruncated: final && len(summary) > MaxSummaryBytes,
		ChangedFiles:     []string{},
		Commits:          []string{},
		TestsRun:         []string{},
		TestStatus:       "not_run",
		RiskNotes:        []string{},
		Artifacts:        []Artifact{},
	}
}

// plainOutput is what a harness printed apart from its JSON events, which are
// never a summary: a run whose output is only events has none.
func plainOutput(stdout string) string {
	var plain []string
	for _, line := range strings.Split(stdout, "\n") {
		if text := strings.TrimSpace(line); strings.HasPrefix(text, "{") && json.Valid([]byte(text)) {
			continue
		}
		plain = append(plain, line)
	}
	return strings.TrimSpace(strings.Join(plain, "\n"))
}

// requireFinalMessage fails a run that exited normally without the agent's
// final message. It applies to the harnesses whose final-message event Kuayle
// knows (Claude Code, Codex, OpenCode): there, no such event means the agent
// stopped mid-work, and the next step would get no statement of what it did.
// Output without a single event is not such a stream and is left alone.
func requireFinalMessage(result RunResult, stdout string) RunResult {
	if result.Status == "succeeded" && !result.FinalMessage && plainOutput(stdout) != strings.TrimSpace(stdout) {
		result.Status, result.NoFinalMessage = "failed", true
	}
	return result
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
	text, _ := finalMessageAfterTools(stdout)
	return text
}

// codexWorkItems are the Codex item types in which the agent does something.
var codexWorkItems = map[string]bool{"command_execution": true, "file_change": true, "mcp_tool_call": true, "web_search": true, "collab_tool_call": true}

// finalMessageAfterTools returns the agent's last message and whether it is
// the run's conclusion: nothing but reasoning came after it. Text followed by
// a tool call ("Let me look…") is what the agent said on the way, so a run
// that ends on a tool call has no final message. Claude Code's result event
// is its conclusion by definition.
func finalMessageAfterTools(stdout string) (string, bool) {
	last, final := "", false
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
			last, final = strings.TrimSpace(event.Item.Text), true
		case event.Type == "result" && strings.TrimSpace(event.Result) != "":
			last, final = strings.TrimSpace(event.Result), true
		case (event.Type == "item.completed" || event.Type == "item.started") && codexWorkItems[event.Item.Type]:
			// A command, a file change, a tool or a search: work, not a
			// conclusion. Bookkeeping items (todo_list, error) are neither.
			final = false
		}
	}
	return last, final
}
