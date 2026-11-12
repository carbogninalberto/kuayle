package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type builtinProvider struct {
	metadata    Metadata
	build       func(RunInput) []string
	parseResult func(stdout, stderr string, exitCode int) RunResult
	environment func(RunInput) map[string]string
}

func (p *builtinProvider) Metadata() Metadata { return p.metadata }

func (p *builtinProvider) BuildInvocation(input RunInput) (Invocation, error) {
	if err := validateInput(p.metadata, input); err != nil {
		return Invocation{}, err
	}
	if err := validateModelOverrides(p.metadata.ID, input); err != nil {
		return Invocation{}, err
	}
	workingDir := input.WorkspacePath
	if workingDir == "" {
		workingDir = "/workspace"
	}
	argv := p.build(input)
	argv = append(argv, input.ExtraArgs...)
	if input.Session != nil && input.Mode == ModeAutonomous && (p.metadata.ID == "codex" || p.metadata.ID == "claude-code") {
		for _, id := range []string{input.Session.ResumeID, input.Session.NewID} {
			if id != "" && !ValidSessionID(id) {
				return Invocation{}, fmt.Errorf("invalid agent session ID")
			}
		}
		wrapped, err := sessionWrapper(input.Session.Dir, argv)
		if err != nil {
			return Invocation{}, err
		}
		argv = wrapped
	}
	var environment map[string]string
	if p.environment != nil {
		environment = p.environment(input)
	}
	return Invocation{
		Image:       p.metadata.DefaultImage,
		Argv:        argv,
		Environment: environment,
		WorkingDir:  workingDir,
		Interactive: input.Mode == ModeInteractive,
		SecretNames: append([]string(nil), p.metadata.RequiredSecrets...),
	}, nil
}

func (p *builtinProvider) ParseEvents(raw []byte) []Event { return parseJSONLines(raw) }
func (p *builtinProvider) ParseResult(stdout, stderr string, exitCode int) RunResult {
	if p.parseResult != nil {
		return p.parseResult(stdout, stderr, exitCode)
	}
	result := defaultResult(stdout, stderr, exitCode)
	if p.metadata.ID == "codex" || p.metadata.ID == "claude-code" {
		return requireFinalMessage(result, stdout)
	}
	return result
}

func NewClaudeCodeProvider(image string) Provider {
	if image == "" {
		image = "ghcr.io/anthropics/claude-code:2.1.14"
	}
	return &builtinProvider{
		metadata: Metadata{
			ID:              "claude-code",
			DisplayName:     "Claude Code",
			DefaultImage:    image,
			RequiredSecrets: []string{},
			OptionalSecrets: []string{"ANTHROPIC_API_KEY"},
			SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
		},
		build: func(input RunInput) []string {
			argv := []string{"claude"}
			if input.Mode == ModeAutonomous {
				// Unattended runs cannot answer permission prompts, so the default
				// mode denies every edit. The hardened agent container is the
				// sandbox, as for Codex.
				argv = append(argv, "--print", "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions")
			}
			if input.Model != "" {
				argv = append(argv, "--model", input.Model)
			}
			if input.Mode == ModeAutonomous && input.Session != nil {
				if input.Session.ResumeID != "" {
					argv = append(argv, "--resume", input.Session.ResumeID)
				} else if input.Session.NewID != "" {
					argv = append(argv, "--session-id", input.Session.NewID)
				}
			}
			if input.Mode == ModeAutonomous {
				argv = append(argv, "-p", input.Prompt)
			}
			return argv
		},
		environment: func(input RunInput) map[string]string {
			if input.Thinking == "" || input.Thinking == "off" {
				return nil
			}
			return map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": input.Thinking}
		},
	}
}

type openCodeProvider struct {
	metadata Metadata
}

func (p *openCodeProvider) Metadata() Metadata { return p.metadata }

// OpenCode takes its model as --model provider/model and Kuayle's effort as
// --variant (its name for a model's reasoning effort; "off" leaves it to
// OpenCode). The terminal UI has no variant flag. The prompt follows "--" as
// one argument: without it OpenCode's argument parser reads a prompt that
// begins with a dash as its own flags (--help, --auto), so extra arguments
// go before it. Unattended runs cannot answer OpenCode's permission prompts,
// which it auto-rejects and then ends the session, so autonomous runs pass
// --auto; the container is the boundary, as for Claude Code's bypassPermissions.
// OpenCode keeps its log in the container's temporary home, which is lost when
// the run ends, so autonomous runs print warnings and errors to standard error,
// where the run's failure cause quotes them.
func (p *openCodeProvider) BuildInvocation(input RunInput) (Invocation, error) {
	if err := validateInput(p.metadata, input); err != nil {
		return Invocation{}, err
	}
	if err := validateModelOverrides(p.metadata.ID, input); err != nil {
		return Invocation{}, err
	}
	workingDir := input.WorkspacePath
	if workingDir == "" {
		workingDir = "/workspace"
	}
	argv := []string{"opencode"}
	if input.Mode == ModeAutonomous {
		argv = append(argv, "run", "--format", "json", "--auto", "--print-logs", "--log-level", "WARN")
	}
	if input.Model != "" {
		argv = append(argv, "--model", input.Model)
	}
	if input.Mode == ModeAutonomous && input.Thinking != "" && input.Thinking != "off" {
		argv = append(argv, "--variant", input.Thinking)
	}
	argv = append(argv, input.ExtraArgs...)
	if input.Mode == ModeAutonomous {
		argv = append(argv, "--", input.Prompt)
	}
	return Invocation{
		Image:       p.metadata.DefaultImage,
		Argv:        argv,
		WorkingDir:  workingDir,
		Interactive: input.Mode == ModeInteractive,
		SecretNames: append([]string(nil), p.metadata.RequiredSecrets...),
	}, nil
}

func (p *openCodeProvider) ParseEvents(raw []byte) []Event { return parseJSONLines(raw) }

// HarnessPermissionRefusedPrefix starts the failure reason a run records when
// the harness refused a tool call's permission and stopped. The domain package
// classifies it, so domain.HarnessPermissionRefusedPrefix must equal it.
const HarnessPermissionRefusedPrefix = "The harness refused"

func (p *openCodeProvider) ParseResult(stdout, stderr string, exitCode int) RunResult {
	result := requireFinalMessage(parseAssistantJSONResult(stdout, stderr, exitCode), stdout)
	if result.FinalMessage || result.FailureReason != "" {
		return result
	}
	detail, refused := openCodeRefusedPermission(stdout, stderr)
	if !refused {
		return result
	}
	if detail == "" {
		result.FailureReason = HarnessPermissionRefusedPrefix + " a tool call's permission and ended the session"
	} else {
		result.FailureReason = HarnessPermissionRefusedPrefix + " the permission " + detail + " and ended the session"
	}
	return result
}

var (
	openCodeANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// openCodeRefusedPermissionLine is what the harness prints on its standard
	// error when nobody answered a permission prompt.
	openCodeRefusedPermissionLine = regexp.MustCompile(`permission requested: ([A-Za-z_]+) \(([^)\n]*)\); auto-rejecting`)
)

// openCodeRefusedPermission reports whether the harness refused a tool call's
// permission and stopped. detail names the permission as "name (pattern)"; it
// is empty when only the tool call's error records the refusal.
func openCodeRefusedPermission(stdout, stderr string) (string, bool) {
	matches := openCodeRefusedPermissionLine.FindAllStringSubmatch(openCodeANSI.ReplaceAllString(stderr, ""), -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		pattern := last[2]
		if len(pattern) > 200 {
			pattern = strings.ToValidUTF8(pattern[:200], "")
		}
		return last[1] + " (" + pattern + ")", true
	}

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}
		var evt struct {
			Type string `json:"type"`
			Part struct {
				State struct {
					Status string `json:"status"`
					Error  string `json:"error"`
				} `json:"state"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}
		if evt.Type == "tool_use" && evt.Part.State.Status == "error" &&
			strings.Contains(strings.ToLower(evt.Part.State.Error), "rejected permission") {
			return "", true
		}
	}
	return "", false
}

func parseAssistantJSONResult(stdout, stderr string, exitCode int) RunResult {
	result := defaultResult(stdout, stderr, exitCode)

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var lastAssistantText string
	// concluded: no tool ran after the last text, so that text is the
	// agent's final message and not what it said before working on.
	concluded := false
	// harnessError is the error OpenCode reported after the agent last spoke.
	harnessError := ""
	// toolFailures and toolGuesses are risk notes; they fail the run only
	// when the agent did not conclude. Kuayle's validation re-runs the
	// declared tests, so a concluded run's claims are checked there.
	var toolFailures, toolGuesses []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}

		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		typ, _ := evt["type"].(string)
		part, _ := evt["part"].(map[string]any)

		if typ == "text" {
			if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
				lastAssistantText, concluded, harnessError = text, true, ""
			}
		}
		// OpenCode's top-level error ({"type":"error","error":{"name","data":
		// {"message"}}}) ends the session, also with exit code 0.
		if failure, ok := evt["error"].(map[string]any); ok && typ == "error" {
			concluded = false
			name, _ := failure["name"].(string)
			data, _ := failure["data"].(map[string]any)
			message, _ := data["message"].(string)
			harnessError = strings.TrimSpace(strings.Trim(name+": "+strings.Join(strings.Fields(message), " "), ": "))
			if harnessError == "" {
				harnessError = "unknown error"
			}
		}
		if typ == "tool_use" || typ == "tool_result" || typ == "tool" {
			concluded = false
		}

		// Retain compatibility with older OpenCode JSON output.
		if typ == "assistant" {
			if msg, ok := evt["message"].(map[string]any); ok {
				if content, ok := msg["content"].([]any); ok {
					for _, block := range content {
						if blockMap, ok := block.(map[string]any); ok {
							if text, ok := blockMap["text"].(string); ok && strings.TrimSpace(text) != "" {
								lastAssistantText, concluded = text, true
							}
							if kind, _ := blockMap["type"].(string); kind == "tool_use" {
								concluded = false
							}
						}
					}
				}
			}
			if msgStr, ok := evt["message"].(string); ok && strings.TrimSpace(msgStr) != "" {
				lastAssistantText, concluded = msgStr, true
			}
		}

		if typ == "tool_result" {
			msgRaw, _ := json.Marshal(evt["message"])
			msgStr := string(msgRaw)
			lower := strings.ToLower(msgStr)
			if strings.Contains(lower, "exit code: 127") ||
				strings.Contains(lower, "exit status 127") ||
				strings.Contains(lower, `"is_error":true`) {
				toolFailures = append(toolFailures, fmt.Sprintf("tool failure detected: %s", typ))
			} else if strings.Contains(lower, "command not found") {
				toolGuesses = append(toolGuesses, fmt.Sprintf("tool failure detected: %s", typ))
			}
		}

		if typ == "tool_use" {
			state, _ := part["state"].(map[string]any)
			metadata, _ := state["metadata"].(map[string]any)
			exitCode, hasExitCode := metadata["exit"].(float64)
			tool, _ := part["tool"].(string)
			title, _ := state["title"].(string)
			if title == "" {
				title = tool
			}
			// Titles are the agent's text: one line, bounded.
			title = strings.Join(strings.Fields(title), " ")
			if len(title) > 200 {
				title = strings.ToValidUTF8(title[:200], "")
			}
			if hasExitCode && (exitCode == 126 || exitCode == 127) {
				toolFailures = append(toolFailures, fmt.Sprintf("tool command unavailable: %s", title))
			} else if output, _ := state["output"].(string); shellTools[tool] && strings.Contains(strings.ToLower(output), "command not found") {
				// Only a shell's output: a file that a read-type tool
				// returned may well contain the phrase.
				toolGuesses = append(toolGuesses, fmt.Sprintf("tool command unavailable: %s", title))
			}
		}
	}

	// A result event (Claude-style streams) is the conclusion; assistant text
	// replaces it only when nothing but that text ended the run.
	if lastAssistantText != "" && (concluded || !result.FinalMessage) {
		final := strings.TrimSpace(lastAssistantText)
		result.Summary, result.FinalMessage, result.SummaryTruncated = truncateSummary(final), concluded, concluded && len(final) > MaxSummaryBytes
	}
	if harnessError != "" && !result.FinalMessage {
		if len(harnessError) > 500 {
			harnessError = strings.ToValidUTF8(harnessError[:500], "")
		}
		result.FailureReason = "The harness reported an error: " + harnessError
	}

	// A tool failure or a guess is a risk note; it fails the run only when
	// the agent did not conclude. Kuayle's validation re-runs the declared
	// tests, so a concluded run's claims are checked there.
	if (len(toolFailures) > 0 || len(toolGuesses) > 0) && !result.FinalMessage {
		if result.Status == "succeeded" {
			result.Status = "failed"
		}
	}
	result.RiskNotes = append(append(result.RiskNotes, toolFailures...), toolGuesses...)

	return result
}

// shellTools are the tools whose output is a command's.
var shellTools = map[string]bool{"bash": true, "shell": true, "sh": true}

func NewOpenCodeProvider(image string) Provider {
	if image == "" {
		image = "ghcr.io/anomalyco/opencode:1.1.25"
	}
	return &openCodeProvider{
		metadata: Metadata{
			ID:              "opencode",
			DisplayName:     "OpenCode",
			DefaultImage:    image,
			RequiredSecrets: []string{},
			SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
		},
	}
}

func NewCodexProvider(image string) Provider {
	if image == "" {
		image = "ghcr.io/openai/codex:0.80.0"
	}
	return &builtinProvider{
		metadata: Metadata{
			ID:              "codex",
			DisplayName:     "Codex",
			DefaultImage:    image,
			RequiredSecrets: []string{},
			OptionalSecrets: []string{"OPENAI_API_KEY"},
			SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
		},
		build: func(input RunInput) []string {
			if input.Mode == ModeInteractive {
				argv := []string{"codex"}
				return appendCodexOverrides(argv, input)
			}
			// The agent container is the sandbox: it drops all capabilities and
			// forbids user namespaces, so Codex's own bubblewrap sandbox cannot
			// start there and every command would fail. Codex documents turning
			// its sandbox off in externally sandboxed environments; approvals are
			// never requested because the run is unattended.
			limits := []string{"-c", `sandbox_mode="danger-full-access"`, "-c", `approval_policy="never"`}
			if input.Session != nil && input.Session.ResumeID != "" {
				argv := appendCodexOverrides(append([]string{"codex", "exec", "resume", "--json"}, limits...), input)
				return append(argv, input.Session.ResumeID, input.Prompt)
			}
			argv := appendCodexOverrides(append([]string{"codex", "exec", "--json"}, limits...), input)
			return append(argv, input.Prompt)
		},
	}
}

func headlessProviderImage(image string) string {
	if image == "" {
		return "ghcr.io/kuayle/dev-machine-headless:0.1.0"
	}
	return image
}

func newStreamJSONBuiltinProvider(metadata Metadata, build func(RunInput) []string) Provider {
	return &builtinProvider{
		metadata:    metadata,
		build:       build,
		parseResult: parseAssistantJSONResult,
	}
}

func NewQwenCodeProvider(image string) Provider {
	return newStreamJSONBuiltinProvider(Metadata{
		ID:              "qwen-code",
		DisplayName:     "Qwen Code",
		DefaultImage:    headlessProviderImage(image),
		RequiredSecrets: []string{},
		SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
	}, func(input RunInput) []string {
		if input.Mode == ModeInteractive {
			return []string{"qwen"}
		}
		return []string{"qwen", "--yolo", "--output-format", "stream-json", input.Prompt}
	})
}

func NewCrushProvider(image string) Provider {
	return &builtinProvider{
		metadata: Metadata{
			ID:              "crush",
			DisplayName:     "Crush",
			DefaultImage:    headlessProviderImage(image),
			RequiredSecrets: []string{},
			SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
		},
		build: func(input RunInput) []string {
			if input.Mode == ModeInteractive {
				return []string{"crush"}
			}
			return []string{"crush", "--yolo", "run", "--quiet", input.Prompt}
		},
	}
}

func NewKimiProvider(image string) Provider {
	return newStreamJSONBuiltinProvider(Metadata{
		ID:              "kimi",
		DisplayName:     "Kimi",
		DefaultImage:    headlessProviderImage(image),
		RequiredSecrets: []string{},
		SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
	}, func(input RunInput) []string {
		if input.Mode == ModeInteractive {
			return []string{"kimi"}
		}
		return []string{"kimi", "--print", "--yolo", "--output-format", "stream-json", "--prompt", input.Prompt}
	})
}

func NewGooseProvider(image string) Provider {
	return newStreamJSONBuiltinProvider(Metadata{
		ID:              "goose",
		DisplayName:     "Goose",
		DefaultImage:    headlessProviderImage(image),
		RequiredSecrets: []string{},
		SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
	}, func(input RunInput) []string {
		if input.Mode == ModeInteractive {
			return []string{"goose", "session"}
		}
		return []string{"goose", "run", "--no-session", "--quiet", "--output-format", "stream-json", "--text", input.Prompt}
	})
}

// Codex is pointed at the model server entirely through -c overrides, so a
// user-managed config.toml in the container cannot redirect a run elsewhere.
func appendCodexOverrides(argv []string, input RunInput) []string {
	if input.OpenAIBaseURL != "" {
		argv = append(argv,
			"-c", `model_provider="kuayle"`,
			"-c", `model_providers.kuayle.name="Kuayle model server"`,
			"-c", `model_providers.kuayle.base_url="`+input.OpenAIBaseURL+`"`,
			"-c", `model_providers.kuayle.env_key="KUAYLE_MODEL_API_KEY"`,
			"-c", `model_providers.kuayle.wire_api="responses"`)
	}
	if input.Model != "" {
		argv = append(argv, "-c", `model="`+input.Model+`"`)
	}
	if input.Thinking != "" {
		effort := input.Thinking
		if effort == "off" {
			effort = "none"
		}
		argv = append(argv, "-c", `model_reasoning_effort="`+effort+`"`)
	}
	return argv
}

var (
	modelOverridePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$`)
	baseURLPattern       = regexp.MustCompile(`^https?://[^\s"'\\]{1,500}$`)
)

// Model values are interpolated into TOML strings or passed as flag values,
// so they are restricted to characters that need no quoting and cannot start
// a flag. Only Codex runs on a model server URL. Providers that take no model
// or effort ignore both.
func validateModelOverrides(providerID string, input RunInput) error {
	if providerID != "codex" && providerID != "claude-code" && providerID != "opencode" {
		return nil
	}
	if input.Model != "" && !modelOverridePattern.MatchString(input.Model) {
		return fmt.Errorf("unsupported model %q", input.Model)
	}
	if input.OpenAIBaseURL != "" && (providerID != "codex" || !baseURLPattern.MatchString(input.OpenAIBaseURL)) {
		return fmt.Errorf("unsupported model server URL")
	}
	switch input.Thinking {
	case "", "off", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("unsupported thinking level %q", input.Thinking)
	}
	return nil
}

type CustomCLIProvider struct {
	defaultImage string
}

func NewCustomCLIProvider(defaultImage string) Provider {
	return &CustomCLIProvider{defaultImage: defaultImage}
}

func (p *CustomCLIProvider) Metadata() Metadata {
	return Metadata{
		ID:             "custom",
		DisplayName:    "Custom CLI",
		DefaultImage:   p.defaultImage,
		SupportedModes: []Mode{ModeInteractive, ModeAutonomous},
		Custom:         true,
	}
}

func (p *CustomCLIProvider) BuildInvocation(input RunInput) (Invocation, error) {
	if err := validateInput(p.Metadata(), input); err != nil {
		return Invocation{}, err
	}
	var config struct {
		Image         string   `json:"image"`
		Entrypoint    string   `json:"entrypoint"`
		Args          []string `json:"args"`
		PromptAsStdin bool     `json:"prompt_as_stdin"`
		SecretNames   []string `json:"required_secrets"`
	}
	if len(input.Config) == 0 || string(input.Config) == "null" {
		return Invocation{}, fmt.Errorf("custom provider configuration is required")
	}
	if err := json.Unmarshal(input.Config, &config); err != nil {
		return Invocation{}, fmt.Errorf("invalid custom provider configuration: %w", err)
	}
	if config.Image == "" {
		config.Image = p.defaultImage
	}
	if !pinnedImage(config.Image) {
		return Invocation{}, fmt.Errorf("custom provider image must include a pinned tag or digest")
	}
	if config.Entrypoint == "" || strings.ContainsAny(config.Entrypoint, "\x00\r\n\t ") {
		return Invocation{}, fmt.Errorf("custom provider entrypoint is required")
	}
	switch strings.ToLower(filepath.Base(config.Entrypoint)) {
	case "sh", "ash", "bash", "dash", "fish", "ksh", "zsh", "cmd", "powershell", "pwsh":
		return Invocation{}, fmt.Errorf("shell entrypoints are not allowed")
	}
	if config.PromptAsStdin {
		return Invocation{}, fmt.Errorf("custom provider prompt_as_stdin is not supported")
	}
	if len(config.SecretNames) > 64 {
		return Invocation{}, fmt.Errorf("custom provider declares too many secrets")
	}
	for _, name := range config.SecretNames {
		if !validSecretName(name) || name == "GITHUB_TOKEN" || strings.HasPrefix(name, "KUAYLE_") {
			return Invocation{}, fmt.Errorf("custom provider has invalid required secret %q", name)
		}
	}
	argv := append([]string{config.Entrypoint}, config.Args...)
	argv = append(argv, input.ExtraArgs...)
	if !config.PromptAsStdin {
		argv = append(argv, input.Prompt)
	}
	workingDir := input.WorkspacePath
	if workingDir == "" {
		workingDir = "/workspace"
	}
	return Invocation{
		Image:       config.Image,
		Argv:        argv,
		WorkingDir:  workingDir,
		Interactive: input.Mode == ModeInteractive,
		SecretNames: config.SecretNames,
	}, nil
}

func pinnedImage(value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\r\n\t ") {
		return false
	}
	lastSlash := strings.LastIndexByte(value, '/')
	if at := strings.LastIndexByte(value, '@'); at > lastSlash {
		return at < len(value)-1 && strings.Contains(value[at+1:], ":")
	}
	colon := strings.LastIndexByte(value, ':')
	return colon > lastSlash && colon < len(value)-1
}

func validSecretName(value string) bool {
	if value == "" || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z') || value[0] == '_') {
		return false
	}
	for _, character := range value[1:] {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func (p *CustomCLIProvider) ParseEvents(raw []byte) []Event { return parseJSONLines(raw) }
func (p *CustomCLIProvider) ParseResult(stdout, stderr string, exitCode int) RunResult {
	var result RunResult
	if json.Unmarshal([]byte(stdout), &result) == nil && result.Status != "" {
		return result
	}
	return defaultResult(stdout, stderr, exitCode)
}

var (
	sessionDirPattern = regexp.MustCompile(`^/workspace/\.kuayle/agent-sessions/[0-9a-f-]{36}$`)
	sessionIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,127}$`)
)

// sessionScript links only the harnesses' transcript directories to the task's
// session directory; config.toml, auth files and credentials stay on tmpfs.
const sessionScript = `set -eu
dir=$1
shift
mkdir -p "$dir/codex" "$dir/claude" "$HOME/.codex" "$HOME/.claude"
rm -rf "$HOME/.codex/sessions" "$HOME/.claude/projects"
ln -s "$dir/codex" "$HOME/.codex/sessions"
ln -s "$dir/claude" "$HOME/.claude/projects"
exec "$@"
`

func sessionWrapper(dir string, argv []string) ([]string, error) {
	if !sessionDirPattern.MatchString(dir) {
		return nil, fmt.Errorf("invalid agent session directory")
	}
	return append([]string{"/bin/sh", "-c", sessionScript, "kuayle-session", dir}, argv...), nil
}

// ValidSessionID reports whether a native session ID is safe to pass to a CLI.
func ValidSessionID(id string) bool {
	return sessionIDPattern.MatchString(id)
}
