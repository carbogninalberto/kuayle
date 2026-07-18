package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type builtinProvider struct {
	metadata Metadata
	build    func(RunInput) []string
}

func (p *builtinProvider) Metadata() Metadata { return p.metadata }

func (p *builtinProvider) BuildInvocation(input RunInput) (Invocation, error) {
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

func (p *builtinProvider) ParseEvents(raw []byte) []Event { return parseJSONLines(raw) }
func (p *builtinProvider) ParseResult(stdout, stderr string, exitCode int) RunResult {
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
			RequiredSecrets: []string{"ANTHROPIC_API_KEY"},
			SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
		},
		build: func(input RunInput) []string {
			if input.Mode == ModeInteractive {
				return []string{"claude"}
			}
			return []string{"claude", "--print", "--output-format", "stream-json", "--verbose", "-p", input.Prompt}
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
			RequiredSecrets: []string{"OPENAI_API_KEY"},
			SupportedModes:  []Mode{ModeInteractive, ModeAutonomous},
		},
		build: func(input RunInput) []string {
			if input.Mode == ModeInteractive {
				return []string{"codex"}
			}
			return []string{"codex", "exec", "--json", "--full-auto", input.Prompt}
		},
	}
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
