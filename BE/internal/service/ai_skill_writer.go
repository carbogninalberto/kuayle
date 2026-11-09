package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

// Writing modes: generate a whole text from a request, or rewrite only the
// selected part of one.
const (
	SkillWriteGenerate = "generate"
	SkillWriteRewrite  = "rewrite"
)

// What is being written: a SKILL.md, an agent profile's instructions, a
// workflow step's iteration prompt, or a custom tool's definition (JSON).
const (
	WriteKindSkill        = "skill"
	WriteKindInstructions = "instructions"
	WriteKindStepPrompt   = "step_prompt"
	WriteKindCustomTool   = "custom_tool"
)

const skillWriterSystem = `You write Agent Skills: SKILL.md files that coding agents (Claude Code, Codex, OpenCode) load when a task needs them.
A SKILL.md starts with YAML front matter between two --- lines holding exactly "name" (lowercase letters, digits and hyphens, at most 64 characters) and "description" (one line, at most 500 characters, saying what the skill does and when to use it), followed by concise Markdown instructions: imperative steps, conventions, commands and short examples.
Return only what is asked, as plain text: no code fences around the answer and no commentary.`

const instructionsWriterSystem = `You write the instructions of a Kuayle agent profile: the standing guidance a coding agent (Claude Code, Codex, OpenCode) follows for every task it runs as that profile, saved as its CLAUDE.md or AGENTS.md.
Write concise Markdown: the agent's focus and responsibilities, the conventions and checks it must follow, and what to avoid. Use short headings and imperative bullet points; no filler.
Return only what is asked, as plain text: no code fences around the answer and no commentary.`

const stepPromptWriterSystem = `You write the iteration prompt of one step in a Kuayle workflow: what the coding agent should do when the workflow enters this step, on top of the task's own instructions.
Keep it short and specific, in imperative voice. It may use these variables, which Kuayle fills in: {{previous_summary}}, {{review_comments}}, {{failing_checks}}, {{diff}}. Keep any variable already in the text.
Return only what is asked, as plain text: no code fences around the answer and no commentary.`

var customToolWriterSystem = `You draft custom tools for the Kuayle assistant. A custom tool is a script that Kuayle runs with one interpreter (one of: ` + strings.Join(domain.CustomToolInterpreters, ", ") + `) in a coding task's check container, without credentials, when the assistant calls the tool with arguments.
Answer with exactly one JSON object and nothing else, with these fields only:
- "name": snake_case, lowercase letters, digits and underscores, starting with a letter, at most 48 characters;
- "description": one or two sentences, at most 1000 characters, saying what the tool does and when the assistant should use it;
- "interpreter": one of ` + strings.Join(domain.CustomToolInterpreters, ", ") + `;
- "script": the script's source;
- "parameters": a JSON Schema {"type":"object","properties":{...},"required":[...],"additionalProperties":false} whose properties are lowercase snake_case names of type string, number, integer or boolean (a string may have an "enum" list), each with a short "description";
- "read_only": true only if the script changes nothing anywhere;
- "timeout_seconds": from 10 to 1800.
The script reads each argument only from the environment variable KUAYLE_ARG_<NAME> (the parameter name in upper case) or from the JSON file named by KUAYLE_TOOL_INPUT_FILE, always quotes arguments and never evaluates them as code or builds an unquoted command line from them. It prints a short, useful result and exits non-zero on failure.
Never put credentials, tokens, API keys, passwords or private hosts in any field, and never ask for them as parameters: the tool runs without credentials, so prefer public endpoints and local commands. No code fences and no commentary.`

var writerSystems = map[string]struct{ system, noun string }{
	WriteKindSkill:        {skillWriterSystem, "SKILL.md"},
	WriteKindInstructions: {instructionsWriterSystem, "set of agent profile instructions"},
	WriteKindStepPrompt:   {stepPromptWriterSystem, "workflow step prompt"},
	WriteKindCustomTool:   {customToolWriterSystem, "custom tool definition"},
}

// skillWritePrompt builds the request for the workspace's model. The draft
// and selection are the user's own text, framed as material to edit.
func skillWritePrompt(mode, instruction, source string, selection *string) string {
	return writePrompt(WriteKindSkill, mode, instruction, source, selection)
}

func writePrompt(kind, mode, instruction, source string, selection *string) string {
	noun := writerSystems[kind].noun
	instruction = strings.TrimSpace(instruction)
	if mode == SkillWriteRewrite {
		if instruction == "" {
			instruction = "Make it clearer and more concise, in imperative voice, keeping its meaning and any commands exact."
		}
		return fmt.Sprintf("Rewrite only the selected part of this %s. Request: %s\n\nThe whole text, for context:\n<<<\n%s\n>>>\n\nSelected part to rewrite:\n<<<\n%s\n>>>\n\nReturn only the replacement for the selected part, in the same format (Markdown, or a front matter line if that is what was selected).", noun, instruction, source, *selection)
	}
	whole := "Return the whole text."
	switch kind {
	case WriteKindSkill:
		whole = "Return the whole SKILL.md, front matter first."
	case WriteKindCustomTool:
		whole = "Return only the JSON object with the tool's fields."
	}
	return fmt.Sprintf("Write a complete %s for this request: %s\n\nThe current draft, which you may replace or build on:\n<<<\n%s\n>>>\n\n%s", noun, instruction, source, whole)
}

// WriteSkill asks the workspace's AI model to generate a SKILL.md or rewrite a
// selection of one. Nothing is saved: the editor shows the result to accept.
// It uses the same provider settings and private-workspace guard as issue
// description expansion.
func (s *AISettingsService) WriteSkill(ctx context.Context, workspaceID uuid.UUID, mode, instruction, source string, selection *string) (string, error) {
	return s.WriteText(ctx, workspaceID, WriteKindSkill, mode, instruction, source, selection)
}

// WriteText is WriteSkill for any kind of text an agent reads.
func (s *AISettingsService) WriteText(ctx context.Context, workspaceID uuid.UUID, kind, mode, instruction, source string, selection *string) (string, error) {
	writer, ok := writerSystems[kind]
	if !ok {
		return "", fmt.Errorf("%w: unknown kind of text", ErrInvalidSkillWrite)
	}
	if mode == SkillWriteRewrite && (selection == nil || strings.TrimSpace(*selection) == "") {
		return "", fmt.Errorf("%w: select the text to rewrite", ErrInvalidSkillWrite)
	}
	if kind == WriteKindCustomTool && mode != SkillWriteGenerate {
		return "", fmt.Errorf("%w: a custom tool is drafted whole", ErrInvalidSkillWrite)
	}
	if mode == SkillWriteGenerate && strings.TrimSpace(instruction) == "" {
		return "", fmt.Errorf("%w: describe the skill to write", ErrInvalidSkillWrite)
	}
	settings, err := s.Get(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if !aiSettingsConfigured(settings) {
		return "", ErrAISettingsNotConfigured
	}
	apiKey, err := crypto.Decrypt(*settings.APIKeyEncrypted, s.encryptionKey)
	if err != nil {
		return "", ErrAISettingsNotConfigured
	}
	releaseSlot, err := acquirePublicationSlot(ctx)
	if err != nil {
		return "", err
	}
	defer releaseSlot()
	guard, err := s.aiRepo.BeginPublicOperation(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	defer func() { _ = guard.Rollback() }()
	content, err := s.completeWith(ctx, settings.BaseURL, settings.Model, apiKey, writer.system, writePrompt(kind, mode, instruction, source, selection))
	if err != nil {
		return "", err
	}
	if content = strings.TrimSpace(stripCodeFence(content)); content == "" {
		return "", ErrAIProviderRequestFailed
	}
	if kind == WriteKindCustomTool {
		return customToolDraft(content)
	}
	return content, nil
}

// customToolDraftFields are the only fields a drafted custom tool keeps. The
// run mode stays the user's choice, and nothing else the model adds (such as
// a credential) survives.
type customToolDraftFields struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Interpreter    string          `json:"interpreter"`
	Script         string          `json:"script"`
	Parameters     json.RawMessage `json:"parameters"`
	ReadOnly       bool            `json:"read_only"`
	TimeoutSeconds int             `json:"timeout_seconds"`
}

// customToolDraft reads the JSON object in the model's answer and returns it
// in canonical form, for the editor to prefill. It is a draft, not a valid
// tool: the user reviews every field and the API validates the save.
func customToolDraft(answer string) (string, error) {
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("%w: the answer is not a tool definition", ErrAIProviderRequestFailed)
	}
	var draft customToolDraftFields
	if err := json.Unmarshal([]byte(answer[start:end+1]), &draft); err != nil {
		return "", fmt.Errorf("%w: the answer is not a tool definition", ErrAIProviderRequestFailed)
	}
	draft.Name, draft.Description = strings.TrimSpace(draft.Name), strings.TrimSpace(draft.Description)
	if !slices.Contains(domain.CustomToolInterpreters, draft.Interpreter) {
		draft.Interpreter = "sh"
	}
	if _, canonical, err := domain.ParseToolSchema(draft.Parameters); err == nil && len(bytes.TrimSpace(draft.Parameters)) > 0 {
		draft.Parameters = canonical
	} else {
		draft.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	switch {
	case draft.TimeoutSeconds == 0:
		draft.TimeoutSeconds = domain.CustomToolDefaultTimeout
	case draft.TimeoutSeconds < domain.CustomToolMinTimeout:
		draft.TimeoutSeconds = domain.CustomToolMinTimeout
	case draft.TimeoutSeconds > domain.CustomToolMaxTimeout:
		draft.TimeoutSeconds = domain.CustomToolMaxTimeout
	}
	out, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
