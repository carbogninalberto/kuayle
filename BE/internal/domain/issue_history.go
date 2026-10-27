package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type IssueHistory struct {
	ID       uuid.UUID `json:"id" db:"id"`
	IssueID  uuid.UUID `json:"issue_id" db:"issue_id"`
	UserID   uuid.UUID `json:"user_id" db:"user_id"`
	Field    string    `json:"field" db:"field"`
	OldValue *string   `json:"old_value" db:"old_value"`
	NewValue *string   `json:"new_value" db:"new_value"`
	// Agent names the harness or assistant that acted for the user, if any.
	Agent     *string   `json:"agent,omitempty" db:"agent"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type actingAgentKey struct{}

// WithActingAgent marks issue changes made in ctx as done by an agent for the
// user, such as "assistant" or a coding harness ID.
func WithActingAgent(ctx context.Context, agent string) context.Context {
	return context.WithValue(ctx, actingAgentKey{}, agent)
}

// ActingAgent returns the agent acting in ctx, or nil.
func ActingAgent(ctx context.Context) *string {
	if agent, ok := ctx.Value(actingAgentKey{}).(string); ok && agent != "" {
		return &agent
	}
	return nil
}
