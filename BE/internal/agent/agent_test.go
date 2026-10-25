package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinProvidersBuildArgvWithoutShell(t *testing.T) {
	providers := []Provider{NewClaudeCodeProvider(""), NewOpenCodeProvider(""), NewCodexProvider("")}
	for _, provider := range providers {
		invocation, err := provider.BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "fix the issue"})
		require.NoError(t, err)
		require.NotEmpty(t, invocation.Argv)
		require.NotContains(t, invocation.Argv, "sh")
		require.NotContains(t, invocation.Argv, "-c")
		require.Contains(t, invocation.Image, ":")
	}
}

func TestNewBuiltinProvidersBuildExactArgv(t *testing.T) {
	const headlessImage = "registry.example/kuayle/dev-machine-headless:test"
	tests := []struct {
		name            string
		provider        Provider
		interactiveArgv []string
		autonomousArgv  []string
	}{

		{
			name:            "qwen-code",
			provider:        NewQwenCodeProvider(headlessImage),
			interactiveArgv: []string{"qwen"},
			autonomousArgv:  []string{"qwen", "--yolo", "--output-format", "stream-json", "fix the issue"},
		},
		{
			name:            "crush",
			provider:        NewCrushProvider(headlessImage),
			interactiveArgv: []string{"crush"},
			autonomousArgv:  []string{"crush", "--yolo", "run", "--quiet", "fix the issue"},
		},
		{
			name:            "kimi",
			provider:        NewKimiProvider(headlessImage),
			interactiveArgv: []string{"kimi"},
			autonomousArgv:  []string{"kimi", "--print", "--yolo", "--output-format", "stream-json", "--prompt", "fix the issue"},
		},
		{
			name:            "goose",
			provider:        NewGooseProvider(headlessImage),
			interactiveArgv: []string{"goose", "session"},
			autonomousArgv:  []string{"goose", "run", "--no-session", "--quiet", "--output-format", "stream-json", "--text", "fix the issue"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interactive, err := test.provider.BuildInvocation(RunInput{Mode: ModeInteractive, Prompt: "fix the issue"})
			require.NoError(t, err)
			require.Equal(t, test.interactiveArgv, interactive.Argv)
			require.True(t, interactive.Interactive)

			autonomous, err := test.provider.BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "fix the issue"})
			require.NoError(t, err)
			require.Equal(t, test.autonomousArgv, autonomous.Argv)
			require.False(t, autonomous.Interactive)
			require.Equal(t, headlessImage, autonomous.Image)
		})
	}
}

func TestNewBuiltinProvidersUseHeadlessImageDefaults(t *testing.T) {
	const headlessImage = "ghcr.io/kuayle/dev-machine-headless:0.1.0"
	providers := []Provider{
		NewQwenCodeProvider(""),
		NewCrushProvider(""),
		NewKimiProvider(""),
		NewGooseProvider(""),
	}

	for _, provider := range providers {
		metadata := provider.Metadata()
		require.Equal(t, headlessImage, metadata.DefaultImage)
		require.Empty(t, metadata.RequiredSecrets)
		require.ElementsMatch(t, []Mode{ModeInteractive, ModeAutonomous}, metadata.SupportedModes)
	}
}

func TestStreamJSONProvidersExtractLastAssistantText(t *testing.T) {
	const headlessImage = "registry.example/kuayle/dev-machine-headless:test"
	stdout := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First message"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Final provider answer"}]}}`,
	}, "\n")
	for _, provider := range []Provider{
		NewQwenCodeProvider(headlessImage),
		NewKimiProvider(headlessImage),
		NewGooseProvider(headlessImage),
	} {
		t.Run(provider.Metadata().ID, func(t *testing.T) {
			require.Equal(t, "Final provider answer", provider.ParseResult(stdout, "", 0).Summary)
		})
	}
}

func TestCodexRunOverridesBuildDeepSeekArgs(t *testing.T) {
	provider := NewCodexProvider("")

	invocation, err := provider.BuildInvocation(RunInput{
		Mode: ModeAutonomous, Prompt: "fix the issue", Model: "deepseek-v4-pro", Thinking: "off",
	})

	require.NoError(t, err)
	require.Equal(t, []string{
		"codex", "exec", "--json", "--full-auto",
		"-c", `model="deepseek-v4-pro"`,
		"-c", `model_provider="deepseek"`,
		"-c", `model_reasoning_effort="none"`,
		"fix the issue",
	}, invocation.Argv)
	require.Empty(t, invocation.Environment)
}

func TestClaudeCodeRunOverridesUseArgvModelAndEnvironmentEffort(t *testing.T) {
	provider := NewClaudeCodeProvider("")

	invocation, err := provider.BuildInvocation(RunInput{
		Mode: ModeAutonomous, Prompt: "fix the issue", Model: "deepseek-flash", Thinking: "max",
	})

	require.NoError(t, err)
	require.Equal(t, []string{
		"claude", "--print", "--output-format", "stream-json", "--verbose",
		"--model", "deepseek-flash", "-p", "fix the issue",
	}, invocation.Argv)
	require.Equal(t, map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": "max"}, invocation.Environment)
}

func TestClaudeCodeThinkingOffOmitsEffortEnvironment(t *testing.T) {
	invocation, err := NewClaudeCodeProvider("").BuildInvocation(RunInput{
		Mode: ModeAutonomous, Prompt: "fix the issue", Thinking: "off",
	})

	require.NoError(t, err)
	require.Empty(t, invocation.Environment)
	require.NotContains(t, invocation.Argv, "--effort")
}

func TestCustomProviderRejectsShell(t *testing.T) {
	provider := NewCustomCLIProvider("")
	for _, entrypoint := range []string{"/bin/sh", "/bin/zsh", "pwsh"} {
		config, err := json.Marshal(map[string]any{"image": "example/agent:1.0.0", "entrypoint": entrypoint})
		require.NoError(t, err)
		_, err = provider.BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "fix", Config: config})
		require.ErrorContains(t, err, "shell entrypoints")
	}
}

func TestCustomProviderRequiresImageTagOrDigest(t *testing.T) {
	provider := NewCustomCLIProvider("")
	for _, image := range []string{"registry.example.com:5000/agent", "example/agent"} {
		config, err := json.Marshal(map[string]any{"image": image, "entrypoint": "/usr/local/bin/agent"})
		require.NoError(t, err)
		_, err = provider.BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "fix", Config: config})
		require.ErrorContains(t, err, "pinned tag or digest")
	}
}

func TestRegistryListIsStable(t *testing.T) {
	registry := NewRegistry(NewOpenCodeProvider(""), NewClaudeCodeProvider(""), NewCodexProvider(""))
	providers := registry.List()
	require.Equal(t, "claude-code", providers[0].Metadata().ID)
	require.Equal(t, "codex", providers[1].Metadata().ID)
	require.Equal(t, "opencode", providers[2].Metadata().ID)
}

func TestOpenCodeProviderMetadata(t *testing.T) {
	p := NewOpenCodeProvider("")
	require.Equal(t, "opencode", p.Metadata().ID)
	require.Equal(t, "OpenCode", p.Metadata().DisplayName)
	require.Contains(t, p.Metadata().SupportedModes, ModeAutonomous)
}

func TestOpenCodeProviderBuildInvocation(t *testing.T) {
	p := NewOpenCodeProvider("")
	inv, err := p.BuildInvocation(RunInput{Mode: ModeAutonomous, Prompt: "fix bug"})
	require.NoError(t, err)
	require.Equal(t, "opencode", inv.Argv[0])
	require.Equal(t, "run", inv.Argv[1])
	require.Equal(t, "--format", inv.Argv[2])
	require.Equal(t, "json", inv.Argv[3])
	require.Equal(t, "fix bug", inv.Argv[4])
	require.Equal(t, "/workspace", inv.WorkingDir)
	require.False(t, inv.Interactive)
}

func TestOpenCodeParseResultEmptyInput(t *testing.T) {
	p := NewOpenCodeProvider("")
	result := p.ParseResult("", "", 0)
	require.Equal(t, "succeeded", result.Status)
	require.Empty(t, result.Summary)
}

func TestOpenCodeParseResultExitCodeNonZero(t *testing.T) {
	p := NewOpenCodeProvider("")
	result := p.ParseResult("some output", "", 1)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "some output", result.Summary)
}

func TestOpenCodeParseResultExtractsFinalAssistantText(t *testing.T) {
	p := NewOpenCodeProvider("")

	stdout := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First message"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Final answer with solution"}]}}`,
	}, "\n")

	result := p.ParseResult(stdout, "", 0)
	require.Equal(t, "Final answer with solution", result.Summary)
}

func TestOpenCodeParseResultToolFailureExit127(t *testing.T) {
	p := NewOpenCodeProvider("")

	stdout := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll run go build."}]}}`,
		`{"type":"tool_use","message":{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"run_shell_command","input":{"command":"go build ./...","description":"Build the project"}}]}}`,
		`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"/bin/bash: line 1: go: command not found\n\nExit code: 127"}],"is_error":true}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The build failed because Go is not installed."}]}}`,
	}, "\n")

	result := p.ParseResult(stdout, "", 0)
	require.Equal(t, "failed", result.Status, "should detect tool failure even when outer exit code is 0")
	require.Equal(t, "The build failed because Go is not installed.", result.Summary)
	require.NotEmpty(t, result.RiskNotes)
	require.Contains(t, strings.Join(result.RiskNotes, " "), "tool failure")
}

func TestOpenCodeParseResultUsesCurrentJSONFormat(t *testing.T) {
	p := NewOpenCodeProvider("")
	stdout := strings.Join([]string{
		`{"type":"text","part":{"type":"text","text":"I will build the project."}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"bash","state":{"status":"completed","title":"Check Go compilation","output":"/usr/bin/bash: go: command not found\n","metadata":{"exit":127}}}}`,
		`{"type":"text","part":{"type":"text","text":"Final summary from OpenCode"}}`,
	}, "\n")

	result := p.ParseResult(stdout, "", 0)

	require.Equal(t, "failed", result.Status)
	require.Equal(t, "Final summary from OpenCode", result.Summary)
	require.Contains(t, strings.Join(result.RiskNotes, " "), "Check Go compilation")
}

func TestOpenCodeParseResultToolFailureIsError(t *testing.T) {
	p := NewOpenCodeProvider("")

	stdout := strings.Join([]string{
		`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_2","content":[{"type":"text","text":"npm install failed with exit code 1"}],"is_error":true}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Installation failed, trying alternative approach."}]}}`,
	}, "\n")

	result := p.ParseResult(stdout, "", 0)
	require.Equal(t, "failed", result.Status, "should detect is_error:true tool result")
	require.NotEmpty(t, result.RiskNotes)
}

func TestOpenCodeParseResultMalformedJSONGraceful(t *testing.T) {
	p := NewOpenCodeProvider("")

	stdout := strings.Join([]string{
		`not valid json`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Valid message after bad line"}]}}`,
	}, "\n")

	result := p.ParseResult(stdout, "", 0)
	require.Equal(t, "Valid message after bad line", result.Summary)
}

func TestOpenCodeParseResultStringMessageAssistant(t *testing.T) {
	p := NewOpenCodeProvider("")

	stdout := strings.Join([]string{
		`{"type":"assistant","message":"Simple string summary"}`,
	}, "\n")

	result := p.ParseResult(stdout, "", 0)
	require.Equal(t, "Simple string summary", result.Summary)
	require.Equal(t, "succeeded", result.Status)
}

func TestOpenCodeParseEventsProdicesEvents(t *testing.T) {
	p := NewOpenCodeProvider("")

	raw := []byte(`{"type":"assistant","message":"hello"}
{"type":"tool_use","message":"running tool"}`)

	events := p.ParseEvents(raw)
	require.Len(t, events, 2)
	require.Equal(t, "assistant", events[0].Type)
	require.Equal(t, "hello", events[0].Message)
	require.Equal(t, "tool_use", events[1].Type)
}

func TestOpenCodeParseResultSummaryTruncation(t *testing.T) {
	p := NewOpenCodeProvider("")

	longText := strings.Repeat("a", 5000)
	stdout := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + longText + `"}]}}`

	result := p.ParseResult(stdout, "", 0)
	require.LessOrEqual(t, len(result.Summary), 4000)
}
