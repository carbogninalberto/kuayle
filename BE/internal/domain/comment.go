package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	IssueID    uuid.UUID  `json:"issue_id" db:"issue_id"`
	UserID     uuid.UUID  `json:"user_id" db:"user_id"`
	Body       string     `json:"body" db:"body"`
	ParentID   *uuid.UUID `json:"parent_id" db:"parent_id"`
	ResolvedAt *time.Time `json:"resolved_at" db:"resolved_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
}

type interactiveSessionKey struct{}

// WithInteractiveSession marks ctx as a request a person made in a signed-in
// Kuayle session, not through a personal access token or automation. Only such
// comments may steer a coding agent, so agents and scripts cannot loop.
func WithInteractiveSession(ctx context.Context) context.Context {
	return context.WithValue(ctx, interactiveSessionKey{}, true)
}

// InteractiveSession reports whether WithInteractiveSession marked ctx.
func InteractiveSession(ctx context.Context) bool {
	interactive, _ := ctx.Value(interactiveSessionKey{}).(bool)
	return interactive
}
