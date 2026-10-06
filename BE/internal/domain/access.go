package domain

import (
	"context"

	"github.com/google/uuid"
)

type accessKey struct{}

// Access identifies a caller, not a cached authorization decision. Repositories
// must read current workspace roles and team membership when applying visibility.
// A nil WorkspaceID is used only for user-level requests spanning workspaces.
type Access struct {
	UserID      uuid.UUID
	WorkspaceID uuid.UUID
}

func WithAccess(ctx context.Context, access Access) context.Context {
	return context.WithValue(ctx, accessKey{}, access)
}

func AccessFromContext(ctx context.Context) (Access, bool) {
	access, ok := ctx.Value(accessKey{}).(Access)
	return access, ok
}

// PublicContext preserves cancellation but removes caller privileges. Use it
// when checking whether a resource may be served through an anonymous URL.
func PublicContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, accessKey{}, struct{}{})
}
