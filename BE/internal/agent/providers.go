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
	if err := validateDeepSeekOverrides(p.metadata.ID, input); err != nil {
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
	return defaultResult(stdout, stderr, exitCode)
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
				argv = append(argv, "--print", "--output-format", "stream-json", "--verbose")
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
	build    func(RunInput) []string
}

func (p *openCodeProvider) Metadata() Metadata { return p.metadata }

func (p *openCodeProvider) BuildInvocation(input RunInput) (Invocation, error) {
	if err := validateInput(p.metadata, input); err != nil {
		return Invocation{}, err
	}
	workingDir := input.WorkspacePath
	if workingDir == "" {
		workingDir = "/workspace"
	}
	argv := p.build(input)
	argv = append(argv, input.ExtraArgs...)
	return Invocation{
		Image:       p.metadata.DefaultImage,
		Argv:        argv,
		WorkingDir:  workingDir,
		Interactive: input.Mode == ModeInteractive,
		SecretNames: append([]string(nil), p.metadata.RequiredSecrets...),
	}, nil
}

func (p *openCodeProvider) ParseEvents(raw []byte) []Event { return parseJSONLines(raw) }

func (p *openCodeProvider) ParseResult(stdout, stderr string, exitCode int) RunResult {
	return parseAssistantJSONResult(stdout, stderr, exitCode)
}

func parseAssistantJSONResult(stdout, stderr string, exitCode int) RunResult {
	result := defaultResult(stdout, stderr, exitCode)

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var lastAssistantText string
	var toolFailures []string

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
				lastAssistantText = text
			}
		}

		// Retain compatibility with older OpenCode JSON output.
		if typ == "assistant" {
			if msg, ok := evt["message"].(map[string]any); ok {
				if content, ok := msg["content"].([]any); ok {
					for _, block := range content {
						if blockMap, ok := block.(map[string]any); ok {
							if text, ok := blockMap["text"].(string); ok && strings.TrimSpace(text) != "" {
								lastAssistantText = text
							}
						}
					}
				}
			}
			if msgStr, ok := evt["message"].(string); ok && strings.TrimSpace(msgStr) != "" {
				lastAssistantText = msgStr
			}
		}

		if typ == "tool_result" {
			msgRaw, _ := json.Marshal(evt["message"])
			msgStr := string(msgRaw)
			lower := strings.ToLower(msgStr)
			if strings.Contains(lower, "exit code: 127") ||
				strings.Contains(lower, "exit status 127") ||
				strings.Contains(lower, "command not found") ||
				strings.Contains(lower, `"is_error":true`) {
				toolFailures = append(toolFailures, fmt.Sprintf("tool failure detected: %s", typ))
			}
		}

		if typ == "tool_use" {
			state, _ := part["state"].(map[string]any)
			metadata, _ := state["metadata"].(map[string]any)
			exitCode, hasExitCode := metadata["exit"].(float64)
			output, _ := state["output"].(string)
			lower := strings.ToLower(output)
			if (hasExitCode && (exitCode == 126 || exitCode == 127)) || strings.Contains(lower, "command not found") {
				title, _ := state["title"].(string)
				if title == "" {
					title, _ = part["tool"].(string)
				}
				toolFailures = append(toolFailures, fmt.Sprintf("tool command unavailable: %s", title))
			}
		}
	}

	if lastAssistantText != "" {
		result.Summary = strings.TrimSpace(lastAssistantText)
		if len(result.Summary) > 4000 {
			result.Summary = result.Summary[:4000]
		}
	}

	if len(toolFailures) > 0 {
		if result.Status == "succeeded" {
			result.Status = "failed"
		}
		result.RiskNotes = append(result.RiskNotes, toolFailures...)
	}

	return result
}

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
		build: func(input RunInput) []string {
			if input.Mode == ModeInteractive {
				return []string{"opencode"}
			}
			return []string{"opencode", "run", "--format", "json", input.Prompt}
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
			// Current Codex CLIs removed --full-auto, so fresh and resumed attempts
			// pass the same sandbox and approval settings explicitly.
			// `exec resume` only accepts them as config overrides.
			if input.Session != nil && input.Session.ResumeID != "" {
				argv := appendCodexOverrides([]string{"codex", "exec", "resume", "--json", "-c", `sandbox_mode="workspace-write"`, "-c", `approval_policy="never"`}, input)
				return append(argv, input.Session.ResumeID, input.Prompt)
			}
			argv := appendCodexOverrides([]string{"codex", "exec", "--json", "--sandbox", "workspace-write", "-c", `approval_policy="never"`}, input)
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

func appendCodexOverrides(argv []string, input RunInput) []string {
	if input.Model != "" {
		argv = append(argv, "-c", `model="`+input.Model+`"`, "-c", `model_provider="deepseek"`)
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

func validateDeepSeekOverrides(providerID string, input RunInput) error {
	if providerID != "codex" && providerID != "claude-code" {
		return nil
	}
	switch input.Model {
	case "", "deepseek-flash", "deepseek-v4-pro":
	default:
		return fmt.Errorf("unsupported DeepSeek model %q", input.Model)
	}
	switch input.Thinking {
	case "", "off", "low", "high", "max":
	default:
		return fmt.Errorf("unsupported DeepSeek thinking level %q", input.Thinking)
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
