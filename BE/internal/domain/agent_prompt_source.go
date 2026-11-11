package domain

import (
	"encoding/json"
	"strings"
)

// The prompt a step received, by source: what Kuayle assembled for one
// attempt, split by where each part came from, so conflicting instructions
// can be seen. It is debugging data kept on the run.
const (
	PromptSourceTask         = "task"
	PromptSourceWorkflowStep = "workflow_step"
	PromptSourcePlan         = "plan"
	PromptSourceFollowUp     = "follow_up"
	PromptSourceProfile      = "profile"
	PromptSourceContract     = "contract"
	PromptSourceProject      = "project"
	PromptSourceNotebook     = "notebook"
	PromptSourceHandoff      = "handoff"
	PromptSourceIssue        = "issue"
	PromptSourceRelated      = "related_work"
	PromptSourceMessages     = "messages"
	PromptSourceAttachments  = "attachments"
	// PromptSourceMemory is reserved for workspace memory.
	PromptSourceMemory = "memory"
)

// Channels: where a part reached the agent.
const (
	PromptChannelPrompt = "prompt"
	// PromptChannelInstructions is the harness's instructions file
	// (domain.ProfileFiles).
	PromptChannelInstructions = "instructions_file"
)

// Bounds of the record.
const (
	PromptSourceBytes  = 64 << 10
	PromptSourcesBytes = 512 << 10
	promptSourcesCount = 64
)

// PromptSource is one part of what an attempt received. The parts of the
// prompt channel, in order, are the prompt exactly as it was assembled.
type PromptSource struct {
	Source    string `json:"source"`
	Channel   string `json:"channel"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// BoundPromptSources keeps the record within its limits: each part and all
// of them together are cut, and marked as cut.
func BoundPromptSources(sources []PromptSource) []PromptSource {
	bounded := make([]PromptSource, 0, len(sources))
	room := PromptSourcesBytes
	for _, source := range sources {
		if len(bounded) == promptSourcesCount || room <= 0 {
			break
		}
		source.Text = strings.ToValidUTF8(source.Text, "\uFFFD")
		if limit := min(PromptSourceBytes, room); len(source.Text) > limit {
			source.Text, source.Truncated = truncateUTF8(source.Text, limit), true
		}
		room -= len(source.Text)
		bounded = append(bounded, source)
	}
	return bounded
}

// EncodePromptSources bounds and encodes the record for storage.
func EncodePromptSources(sources []PromptSource) json.RawMessage {
	if len(sources) == 0 {
		return nil
	}
	encoded, err := json.Marshal(BoundPromptSources(sources))
	if err != nil {
		return nil
	}
	return encoded
}

// TaskFollowUpMarker starts the follow-up section of a task prompt; what
// precedes it is the task's original request.
const TaskFollowUpMarker = "\n\n---\nFollow-up from "
