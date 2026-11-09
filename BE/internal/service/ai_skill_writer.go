package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

// Skill writing modes: generate a whole SKILL.md from a request, or rewrite
// only the selected part of one.
const (
	SkillWriteGenerate = "generate"
	SkillWriteRewrite  = "rewrite"
)

const skillWriterSystem = `You write Agent Skills: SKILL.md files that coding agents (Claude Code, Codex, OpenCode) load when a task needs them.
A SKILL.md starts with YAML front matter between two --- lines holding exactly "name" (lowercase letters, digits and hyphens, at most 64 characters) and "description" (one line, at most 500 characters, saying what the skill does and when to use it), followed by concise Markdown instructions: imperative steps, conventions, commands and short examples.
Return only what is asked, as plain text: no code fences around the answer and no commentary.`

// skillWritePrompt builds the request for the workspace's model. The draft
// and selection are the user's own text, framed as material to edit.
func skillWritePrompt(mode, instruction, source string, selection *string) string {
	instruction = strings.TrimSpace(instruction)
	if mode == SkillWriteRewrite {
		if instruction == "" {
			instruction = "Make it clearer and more concise, in imperative voice, keeping its meaning and any commands exact."
		}
		return fmt.Sprintf("Rewrite only the selected part of this SKILL.md. Request: %s\n\nThe whole SKILL.md, for context:\n<<<\n%s\n>>>\n\nSelected part to rewrite:\n<<<\n%s\n>>>\n\nReturn only the replacement for the selected part, in the same format (Markdown, or a front matter line if that is what was selected).", instruction, source, *selection)
	}
	return fmt.Sprintf("Write a complete SKILL.md for this request: %s\n\nThe current draft, which you may replace or build on:\n<<<\n%s\n>>>\n\nReturn the whole SKILL.md, front matter first.", instruction, source)
}

// WriteSkill asks the workspace's AI model to generate a SKILL.md or rewrite a
// selection of one. Nothing is saved: the editor shows the result to accept.
// It uses the same provider settings and private-workspace guard as issue
// description expansion.
func (s *AISettingsService) WriteSkill(ctx context.Context, workspaceID uuid.UUID, mode, instruction, source string, selection *string) (string, error) {
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
	content, err := s.completeWith(ctx, settings.BaseURL, settings.Model, apiKey, skillWriterSystem, skillWritePrompt(mode, instruction, source, selection))
	if err != nil {
		return "", err
	}
	if content = strings.TrimSpace(stripCodeFence(content)); content == "" {
		return "", ErrAIProviderRequestFailed
	}
	return content, nil
}
