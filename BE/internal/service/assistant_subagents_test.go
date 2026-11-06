package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// subagentFixture runs spawn_research_subagents against a fake model, with
// web_search and the scan tools wired to fakes.
type subagentFixture struct {
	svc      *ConversationService
	attempt  domain.ConversationAttempt
	provider *providerFake
	config   ResearchConfig
}

func newSubagentFixture(t *testing.T, model subagentCaller) *subagentFixture {
	t.Helper()
	scan := newScanFixture(t)
	f := &subagentFixture{provider: &providerFake{name: "brave", results: []WebSearchResult{{Title: "Go os.Root", URL: "https://go.dev/blog/osroot", Excerpt: "Traversal-resistant"}}}}
	f.config = ResearchConfig{SearchProvider: "brave", SearchAPIKey: searchKey, SubagentMaxParallel: 3}
	scan.tools.SetWebSearch(researchSettingsFunc(func() ResearchConfig { return f.config }), f.provider)
	scan.tools.SetSubagents(true)
	f.svc = &ConversationService{tools: scan.tools, subagentCall: model}
	f.attempt = domain.ConversationAttempt{Conversation: domain.Conversation{WorkspaceID: scan.ws, CreatorID: scan.admin}, Message: domain.ConversationMessage{Model: "big-model"}}
	return f
}

type researchSettingsFunc func() ResearchConfig

func (f researchSettingsFunc) ResearchConfig(context.Context, uuid.UUID) (ResearchConfig, error) {
	return f(), nil
}

func (f *subagentFixture) spawn(t *testing.T, tasks ...map[string]any) (*SubagentsOutput, error) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"tasks": tasks})
	output, err := f.svc.runSubagents(context.Background(), f.attempt, string(data))
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), assistantToolOutputBytes, "the result fits the tool result limit")
	var decoded SubagentsOutput
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return &decoded, nil
}

func task(title string) map[string]any {
	return map[string]any{"title": title, "instructions": "Research " + title + " in acme/app."}
}

func toolCall(id, name, arguments string) chatToolCall {
	call := chatToolCall{ID: id, Type: "function"}
	call.Function.Name, call.Function.Arguments = name, arguments
	return call
}

// taskTitle finds the sub-task a model request belongs to.
func taskTitle(messages []chatCompletionMessage) string {
	for _, message := range messages {
		if message.Role == "user" {
			return strings.TrimSuffix(strings.TrimPrefix(message.Content, "Research "), " in acme/app.")
		}
	}
	return ""
}

func hasToolResult(messages []chatCompletionMessage) bool {
	for _, message := range messages {
		if message.Role == "tool" {
			return true
		}
	}
	return false
}

func TestSubagentsRunInParallelUpToTheConfiguredCap(t *testing.T) {
	var running, peak atomic.Int32
	var models sync.Map
	f := newSubagentFixture(t, func(ctx context.Context, model string, messages []chatCompletionMessage, tools []chatToolSpec) (string, []chatToolCall, error) {
		models.Store(model, true)
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		running.Add(-1)
		return `{"summary":"done ` + taskTitle(messages) + `"}`, nil, nil
	})
	f.config.SubagentMaxParallel = 2
	f.config.SubagentModel = "small-model"
	out, err := f.spawn(t, task("a"), task("b"), task("c"), task("d"), task("e"))
	require.NoError(t, err)
	require.EqualValues(t, 2, peak.Load(), "never more than the configured cap at once")
	require.Equal(t, 2, out.Parallel)
	require.Equal(t, "small-model", out.Model, "the configured cheap model is used")
	_, usedBig := models.Load("big-model")
	require.False(t, usedBig)
	require.Len(t, out.Subagents, 5)
	require.Equal(t, 5, out.Completed)
	for i, result := range out.Subagents {
		require.Equal(t, i, result.Index, "results keep the task order")
		require.Equal(t, "completed", result.Status)
		require.Equal(t, "small-model", result.Model)
		require.Equal(t, "done "+result.Title, result.Summary)
		require.Nil(t, result.Cost)
		require.Positive(t, result.TokensEstimated)
	}
}

func TestSubagentsDefaultToTheConversationModelAndAConservativeCap(t *testing.T) {
	var peak, running atomic.Int32
	f := newSubagentFixture(t, func(_ context.Context, model string, _ []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		require.Equal(t, "big-model", model)
		now := running.Add(1)
		if now > peak.Load() {
			peak.Store(now)
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		return "plain text answer", nil, nil
	})
	f.config.SubagentMaxParallel = 0
	tasks := []map[string]any{}
	for i := range 6 {
		tasks = append(tasks, task(fmt.Sprint(i)))
	}
	out, err := f.spawn(t, tasks...)
	require.NoError(t, err)
	require.Equal(t, subagentDefaultParallel, out.Parallel)
	require.LessOrEqual(t, peak.Load(), int32(subagentDefaultParallel))
	require.Equal(t, "plain text answer", out.Subagents[0].Summary, "a non-JSON answer becomes the summary")
}

func TestAFailingSubagentDoesNotAbortTheOthers(t *testing.T) {
	f := newSubagentFixture(t, func(ctx context.Context, _ string, messages []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		switch taskTitle(messages) {
		case "broken":
			return "", nil, ErrAIProviderRequestFailed
		case "slow":
			<-ctx.Done()
			return "", nil, ctx.Err()
		case "panics":
			panic("model adapter bug")
		}
		return `{"summary":"ok"}`, nil, nil
	})
	withSubagentLimits(t, func(l *subagentLimitSet) { l.timeout = 100 * time.Millisecond })
	out, err := f.spawn(t, task("good"), task("broken"), task("slow"), task("panics"))
	require.NoError(t, err)
	status := map[string]string{}
	for _, result := range out.Subagents {
		status[result.Title] = result.Status
		if result.Status != "completed" {
			require.NotEmpty(t, result.Error, result.Title)
		}
	}
	require.Equal(t, map[string]string{"good": "completed", "broken": "failed", "slow": "timed_out", "panics": "failed"}, status)
	require.Equal(t, 1, out.Completed)
	require.Equal(t, 3, out.Failed)
}

func TestSubagentsCanOnlyUseReadOnlyResearchTools(t *testing.T) {
	var offered []string
	var mu sync.Mutex
	f := newSubagentFixture(t, func(_ context.Context, _ string, messages []chatCompletionMessage, tools []chatToolSpec) (string, []chatToolCall, error) {
		mu.Lock()
		for _, tool := range tools {
			offered = append(offered, tool.Function.Name)
		}
		mu.Unlock()
		if !hasToolResult(messages) {
			return "", []chatToolCall{
				toolCall("1", "create_issue", `{"team_key":"ENG","title":"x"}`),
				toolCall("2", subagentToolName, `{"tasks":[]}`),
				toolCall("3", "merge_pull_request", `{"identifier":"ENG-1"}`),
				toolCall("4", "repo_glob", `{"repository":"acme/app","pattern":"**/*.go"}`),
			}, nil
		}
		return `{"summary":"read"}`, nil, nil
	})
	out, err := f.spawn(t, task("a"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"web_search", "repo_list_files", "repo_glob", "repo_grep", "repo_read_file", "repo_symbols"}, unique(offered))
	steps := out.Subagents[0].Steps
	require.Len(t, steps, 4)
	for _, step := range steps[:3] {
		require.False(t, step.OK, step.Tool)
		require.Contains(t, step.Error, "not available to research subagents")
	}
	require.True(t, steps[3].OK, "a scan tool runs")
	require.Equal(t, "repo_glob", steps[3].Tool)
	require.JSONEq(t, `{"repository":"acme/app","pattern":"**/*.go"}`, string(steps[3].Input))
}

func unique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func TestATaskCanNarrowItsTools(t *testing.T) {
	f := newSubagentFixture(t, func(_ context.Context, _ string, messages []chatCompletionMessage, tools []chatToolSpec) (string, []chatToolCall, error) {
		require.Len(t, tools, 1)
		require.Equal(t, "web_search", tools[0].Function.Name)
		if !hasToolResult(messages) {
			return "", []chatToolCall{toolCall("1", "repo_grep", `{"repository":"acme/app","pattern":"x"}`)}, nil
		}
		return `{"summary":"ok"}`, nil, nil
	})
	out, err := f.spawn(t, map[string]any{"title": "web", "instructions": "search", "tools": []string{"web_search"}})
	require.NoError(t, err)
	require.Contains(t, out.Subagents[0].Steps[0].Error, "not available")
}

func TestSubagentsShareOneStepAndTokenBudget(t *testing.T) {
	var calls atomic.Int32
	f := newSubagentFixture(t, func(_ context.Context, _ string, _ []chatCompletionMessage, tools []chatToolSpec) (string, []chatToolCall, error) {
		if len(tools) == 0 {
			return `{"summary":"final"}`, nil, nil
		}
		n := calls.Add(1)
		return "", []chatToolCall{toolCall(fmt.Sprint(n), "repo_list_files", `{"repository":"acme/app"}`)}, nil
	})
	withSubagentLimits(t, func(l *subagentLimitSet) { l.steps = 3 })
	out, err := f.spawn(t, task("a"), task("b"))
	require.NoError(t, err)
	require.Equal(t, 3, out.Budget.MaxSteps)
	require.Equal(t, 3, out.Budget.StepsUsed)
	ran := 0
	for _, result := range out.Subagents {
		require.Equal(t, "completed", result.Status, "each still gives its final answer")
		require.Equal(t, "final", result.Summary)
		for _, step := range result.Steps {
			if step.OK {
				ran++
			}
		}
	}
	require.Equal(t, 3, ran, "no more tool calls run than the budget allows")

	f = newSubagentFixture(t, func(_ context.Context, _ string, _ []chatCompletionMessage, tools []chatToolSpec) (string, []chatToolCall, error) {
		if len(tools) == 0 {
			return `{"summary":"final"}`, nil, nil
		}
		return strings.Repeat("thinking ", 2000), []chatToolCall{toolCall("x", "repo_list_files", `{"repository":"acme/app"}`)}, nil
	})
	// One call fits; its long reply uses up the rest, so the next round is not started.
	withSubagentLimits(t, func(l *subagentLimitSet) { l.tokens = 6000 })
	out, err = f.spawn(t, task("a"))
	require.NoError(t, err)
	require.Equal(t, "budget_exhausted", out.Subagents[0].Status)
	require.GreaterOrEqual(t, out.Budget.TokensEstimated, 4000, "the first call's real usage is counted")
	require.Len(t, out.Subagents[0].Steps, 1, "only the first round's tool call ran")
}

// The token budget is reserved before each model call, so sub-tasks running at
// the same time cannot all start calls the remaining budget cannot cover.
func TestSubagentsReserveTokensBeforeConcurrentCalls(t *testing.T) {
	var started atomic.Int32
	release := make(chan struct{})
	f := newSubagentFixture(t, func(ctx context.Context, _ string, _ []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		if started.Add(1) == 1 {
			// Hold the first call open while the other sub-task tries to start.
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
		}
		return `{"summary":"done"}`, nil, nil
	})
	// Room for one call's reservation (prompt plus reply allowance), not two.
	withSubagentLimits(t, func(l *subagentLimitSet) { l.tokens = subagentReplyAllowance + 1500 })
	go func() {
		for started.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
		close(release)
	}()
	out, err := f.spawn(t, task("a"), task("b"))
	require.NoError(t, err)
	require.Equal(t, int32(1), started.Load(), "the second call never starts: its reservation does not fit")
	statuses := []string{out.Subagents[0].Status, out.Subagents[1].Status}
	require.ElementsMatch(t, []string{"completed", "budget_exhausted"}, statuses)
	require.LessOrEqual(t, out.Budget.TokensEstimated, out.Budget.MaxTokensEstimated)
}

func TestSubagentCitationsAndFilesAreOnlyWhatToolsReturned(t *testing.T) {
	f := newSubagentFixture(t, func(_ context.Context, _ string, messages []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		if !hasToolResult(messages) {
			return "", []chatToolCall{
				toolCall("1", "web_search", `{"query":"os.Root"}`),
				toolCall("2", "repo_read_file", `{"repository":"acme/app","path":"BE/main.go"}`),
			}, nil
		}
		return "Here you go:\n```json\n" + `{"summary":"Found it.","findings":["main calls Serve"],"files":["BE/main.go:5","BE/invented.go"],"citations":[{"url":"https://go.dev/blog/osroot","title":"Go"},{"url":"https://made-up.example/x","title":"Fake"},"https://go.dev/blog/osroot"]}` + "\n```", nil, nil
	})
	out, err := f.spawn(t, task("a"))
	require.NoError(t, err)
	result := out.Subagents[0]
	require.Equal(t, "Found it.", result.Summary)
	require.Equal(t, []string{"main calls Serve"}, result.Findings)
	require.Equal(t, []string{"BE/main.go:5"}, result.Files, "a path no tool returned is dropped")
	require.Equal(t, []SubagentCitation{{URL: "https://go.dev/blog/osroot", Title: "Go"}}, result.Citations, "a URL no search returned is dropped")
	require.Equal(t, 2, result.Dropped)
}

func TestSubagentInputIsValidated(t *testing.T) {
	f := newSubagentFixture(t, func(context.Context, string, []chatCompletionMessage, []chatToolSpec) (string, []chatToolCall, error) {
		return "", nil, errors.New("not called")
	})
	for _, args := range []string{`{"tasks":[]}`, `{}`, `{"tasks":[{"title":"","instructions":"x"}]}`, `{"tasks":[{"title":"a","instructions":"x","tools":["create_issue"]}]}`, `{"tasks":[{"title":"a","instructions":"x"}],"extra":1}`} {
		_, err := f.svc.runSubagents(context.Background(), f.attempt, args)
		require.Error(t, err, args)
	}
	nine := make([]map[string]any, 9)
	for i := range nine {
		nine[i] = task(fmt.Sprint(i))
	}
	_, err := f.spawn(t, nine...)
	require.Error(t, err)
}

func TestManyLargeSubagentResultsAreShrunkToFit(t *testing.T) {
	f := newSubagentFixture(t, func(_ context.Context, _ string, messages []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		findings := make([]string, 30)
		for i := range findings {
			findings[i] = strings.Repeat("finding ", 100)
		}
		data, _ := json.Marshal(map[string]any{"summary": strings.Repeat("long summary ", 2000), "findings": findings})
		return string(data), nil, nil
	})
	f.config.SubagentMaxParallel = 8
	tasks := make([]map[string]any, 8)
	for i := range tasks {
		tasks[i] = task(fmt.Sprint(i))
	}
	out, err := f.spawn(t, tasks...)
	require.NoError(t, err)
	require.True(t, out.Truncated)
	for _, result := range out.Subagents {
		require.NotEmpty(t, result.Summary, "every sub-task keeps a summary")
	}
}

func TestSubagentToolIsOfferedOnlyWithResearchTools(t *testing.T) {
	tool, ok := lookupAssistantTool(subagentToolName)
	require.True(t, ok)
	require.True(t, tool.subagents)
	require.False(t, tool.write || tool.approval)
	tools := &AssistantTools{}
	tools.SetSubagents(true)
	require.False(t, tools.available(tool), "no research settings, no research tools")
	tools.SetWebSearch(researchFake{}, &providerFake{name: "brave"})
	require.True(t, tools.available(tool))
	tools.SetSubagents(false)
	require.False(t, tools.available(tool))
}

// withSubagentLimits changes the subagent limits for one test.
func withSubagentLimits(t *testing.T, change func(*subagentLimitSet)) {
	t.Helper()
	saved := subagentLimits
	change(&subagentLimits)
	t.Cleanup(func() { subagentLimits = saved })
}

func TestResearchPromptFollowsTheToolsOffered(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	require.Empty(t, (&AssistantTools{}).researchPrompt(ctx, ws), "no research tools, no research guidance")

	f := newSubagentFixture(t, nil)
	prompt := f.svc.tools.researchPrompt(ctx, ws)
	require.Contains(t, prompt, "web_search")
	require.Contains(t, prompt, "repo_grep")
	require.Contains(t, prompt, "file paths", "issues filed after a scan cite concrete paths")
	require.Contains(t, prompt, subagentToolName)

	f.svc.tools.SetSettings(&toolSwitches{disabled: map[string]bool{"web_search": true, subagentToolName: true}})
	prompt = f.svc.tools.researchPrompt(ctx, ws)
	require.NotContains(t, prompt, "web_search", "a disabled tool is not described")
	require.NotContains(t, prompt, subagentToolName)
	require.Contains(t, prompt, "repo_grep")
}

type toolSwitches struct{ disabled map[string]bool }

func (s *toolSwitches) Disabled(context.Context, uuid.UUID) (map[string]bool, error) {
	return s.disabled, nil
}
func (s *toolSwitches) Set(context.Context, uuid.UUID, uuid.UUID, string, bool) error { return nil }

func TestSubagentsAreRefusedWhenUnavailableOrForGuests(t *testing.T) {
	called := false
	f := newSubagentFixture(t, func(context.Context, string, []chatCompletionMessage, []chatToolSpec) (string, []chatToolCall, error) {
		called = true
		return "", nil, nil
	})
	args := `{"tasks":[{"title":"a","instructions":"b"}]}`
	guest := f.attempt
	guest.Conversation.CreatorID = uuid.New()
	_, err := f.svc.runSubagents(context.Background(), guest, args)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "no longer have access")

	f.svc.tools.SetSubagents(false)
	_, err = f.svc.runSubagents(context.Background(), f.attempt, args)
	require.Error(t, err, "a tool that is not wired is unknown, even when called by name")
	unwired := &ConversationService{tools: NewAssistantTools(prIssues{}, nil, prMembers{}, nil)}
	_, err = unwired.runSubagents(context.Background(), f.attempt, args)
	require.Error(t, err)
	require.False(t, called, "no model is called for a refused run")
}

func TestDefaultOffToolsAreRegisteredTools(t *testing.T) {
	require.Equal(t, []string{subagentToolName}, repository.DefaultOffAssistantTools)
	for _, name := range repository.DefaultOffAssistantTools {
		_, ok := lookupAssistantTool(name)
		require.True(t, ok, name)
	}
}
