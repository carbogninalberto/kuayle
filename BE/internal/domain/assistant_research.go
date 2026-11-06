package domain

import (
	"time"

	"github.com/google/uuid"
)

// AssistantResearchSettings are a workspace's settings for the assistant's
// research tools (KUA-93): the web search provider and its encrypted API key,
// and the model and parallelism of research sub-tasks. An empty model means
// the conversation's own model.
type AssistantResearchSettings struct {
	WorkspaceID           uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	SearchProvider        string     `json:"search_provider" db:"search_provider"`
	SearchAPIKeyEncrypted *string    `json:"-" db:"search_api_key_encrypted"`
	SubagentModel         string     `json:"subagent_model" db:"subagent_model"`
	SubagentMaxParallel   int        `json:"subagent_max_parallel" db:"subagent_max_parallel"`
	UpdatedBy             *uuid.UUID `json:"-" db:"updated_by"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at" db:"updated_at"`
}
