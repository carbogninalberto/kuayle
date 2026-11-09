package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

// Writing modes: generate a whole text from a request, or rewrite only the
// selected part of one.
const (
	SkillWriteGenerate = "generate"
	SkillWriteRewrite  = "rewrite"
)

// What is being written: a SKILL.md, an agent profile's instructions, or a
// workflow step's iteration prompt.
const (
	WriteKindSkill        = "skill"
	WriteKindInstructions = "instructions"
	WriteKindStepPrompt   = "step_prompt"
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

var writerSystems = map[string]struct{ system, noun string }{
	WriteKindSkill:        {skillWriterSystem, "SKILL.md"},
	WriteKindInstructions: {instructionsWriterSystem, "set of agent profile instructions"},
	WriteKindStepPrompt:   {stepPromptWriterSystem, "workflow step prompt"},
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
	if kind == WriteKindSkill {
		whole = "Return the whole SKILL.md, front matter first."
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
	return content, nil
}
