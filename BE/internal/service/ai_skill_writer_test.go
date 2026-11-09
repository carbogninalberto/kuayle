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
