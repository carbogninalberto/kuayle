package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Generating asks for a whole SKILL.md from the request; rewriting asks for
// the selected part only, with the whole draft as context.
func TestSkillWritePrompts(t *testing.T) {
	generate := skillWritePrompt(SkillWriteGenerate, "  Review SQL migrations ", "---\nname: x\n---", nil)
	assert.Contains(t, generate, "Write a complete SKILL.md for this request: Review SQL migrations")
	assert.Contains(t, generate, "<<<\n---\nname: x\n---\n>>>")

	selected := "Do the thing."
	rewrite := skillWritePrompt(SkillWriteRewrite, "", "whole draft", &selected)
	assert.Contains(t, rewrite, "Rewrite only the selected part")
	assert.Contains(t, rewrite, "clearer and more concise", "a rewrite without a request gets a sensible default")
	assert.Contains(t, rewrite, "Selected part to rewrite:\n<<<\nDo the thing.\n>>>")
	assert.Contains(t, rewrite, "whole draft")
}

func TestWriteSkillRefusesIncompleteRequestsBeforeCallingTheModel(t *testing.T) {
	svc := NewAISettingsService(nil, nil, nil, nil)
	blank := "  "
	_, err := svc.WriteSkill(context.Background(), [16]byte{}, SkillWriteRewrite, "", "draft", &blank)
	require.ErrorIs(t, err, ErrInvalidSkillWrite)
	_, err = svc.WriteSkill(context.Background(), [16]byte{}, SkillWriteGenerate, " ", "draft", nil)
	require.ErrorIs(t, err, ErrInvalidSkillWrite)
}

func TestCompleteWithSendsTheGivenSystemPrompt(t *testing.T) {
	var system string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		system = body.Messages[0].Content
		_, _ = w.Write([]byte(completionBody))
	}))
	defer provider.Close()
	svc := NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))
	_, err := svc.completeWith(context.Background(), provider.URL, "model", "key", skillWriterSystem, "prompt")
	require.NoError(t, err)
	assert.Equal(t, skillWriterSystem, system)
}

func TestWritePromptsNameWhatIsWritten(t *testing.T) {
	assert.Contains(t, writePrompt(WriteKindInstructions, SkillWriteGenerate, "Security focus", "", nil), "Write a complete set of agent profile instructions for this request: Security focus")
	assert.NotContains(t, writePrompt(WriteKindInstructions, SkillWriteGenerate, "x", "", nil), "front matter")
	selected := "Fix it."
	assert.Contains(t, writePrompt(WriteKindStepPrompt, SkillWriteRewrite, "", "Fix it. {{review_comments}}", &selected), "Rewrite only the selected part of this workflow step prompt")
	_, err := NewAISettingsService(nil, nil, nil, nil).WriteText(context.Background(), [16]byte{}, "poem", SkillWriteGenerate, "x", "", nil)
	require.ErrorIs(t, err, ErrInvalidSkillWrite)
}

// A custom tool is drafted whole, as JSON, under a system prompt that keeps
// secrets and unquoted arguments out of the script.
func TestCustomToolWritePrompts(t *testing.T) {
	prompt := writePrompt(WriteKindCustomTool, SkillWriteGenerate, " List open PRs ", `{"name":""}`, nil)
	assert.Contains(t, prompt, "Write a complete custom tool definition for this request: List open PRs")
	assert.Contains(t, prompt, "<<<\n{\"name\":\"\"}\n>>>")
	assert.Contains(t, prompt, "Return only the JSON object")
	for _, rule := range []string{"KUAYLE_ARG_", "KUAYLE_TOOL_INPUT_FILE", "without credentials", "Never put credentials", `"interpreter"`, "sh, bash, node, python3"} {
		assert.Contains(t, customToolWriterSystem, rule)
	}
	selected := "x"
	_, err := NewAISettingsService(nil, nil, nil, nil).WriteText(context.Background(), [16]byte{}, WriteKindCustomTool, SkillWriteRewrite, "", "{}", &selected)
	require.ErrorIs(t, err, ErrInvalidSkillWrite, "a tool definition is drafted whole, never rewritten in part")
}

// The model's answer keeps only the tool's own fields, in canonical form:
// anything else it adds (a credential, a run mode) is dropped.
func TestCustomToolDraftKeepsOnlyTheToolFields(t *testing.T) {
	answer := "Here it is:\n```json\n{\"name\":\"open_prs\",\"description\":\"List open pull requests.\",\"interpreter\":\"bash\",\"script\":\"gh pr list --repo \\\"$KUAYLE_ARG_REPO\\\"\",\"parameters\":{\"type\":\"object\",\"required\":[\"repo\"],\"properties\":{\"repo\":{\"type\":\"string\",\"maxLength\":200}}},\"read_only\":true,\"timeout_seconds\":5,\"api_key\":\"sk-123\",\"run_mode\":\"auto\"}\n```"
	text, err := customToolDraft(answer)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &got))
	assert.ElementsMatch(t, []string{"name", "description", "interpreter", "script", "parameters", "read_only", "timeout_seconds"}, draftFields(got))
	assert.Equal(t, "open_prs", got["name"])
	assert.Equal(t, true, got["read_only"])
	assert.EqualValues(t, 10, got["timeout_seconds"], "the timeout is clamped to the allowed range")
	params := got["parameters"].(map[string]any)
	assert.Equal(t, false, params["additionalProperties"], "a valid schema comes back canonical")
	assert.NotContains(t, text, "sk-123")

	unknown, err := customToolDraft(`{"name":"x","description":"d","interpreter":"ruby","script":"echo","parameters":{"type":"array"}}`)
	require.NoError(t, err)
	assert.Contains(t, unknown, `"interpreter":"sh"`, "an interpreter the container lacks falls back to sh")
	assert.Contains(t, unknown, `"parameters":{"type":"object","properties":{}}`, "an invalid schema is left for the user to build")

	for _, bad := range []string{"no json here", "[1,2]", `{"name": 1}`} {
		_, err := customToolDraft(bad)
		require.ErrorIs(t, err, ErrAIProviderRequestFailed, bad)
	}
}

func draftFields(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
