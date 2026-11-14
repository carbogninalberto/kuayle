package middleware

import (
	"slices"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// RequirePermission enforces scope ∩ RBAC. For PAT callers the token's
// scopes are the ceiling and are checked first; the workspace role is the
// floor and is checked second. User-level routes (no WorkspaceMembership)
// have no role: there the scope check alone governs PATs, and JWT sessions
// pass as before.
func RequirePermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if scopes, ok := c.Get(TokenScopesKey).([]string); ok {
				if !slices.Contains(scopes, permission) {
					return response.Forbidden(c)
				}
			}
			role, ok := c.Get("workspace_role").(string)
			if !ok {
				return next(c)
			}
			if !domain.HasPermission(role, permission) {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

// RequireSession rejects PAT callers, keeping a route interactive-session
// (JWT) only. Routes without a permission annotation get this instead, so
// personal access tokens are denied by default.
func RequireSession() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Get(TokenScopesKey) != nil {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

// RequireWorkspaceAdmin allows an interactive owner or admin and rejects
// personal access tokens. Used where the product grants admins a workspace
// setting without granting them every workspace:manage permission.
func RequireWorkspaceAdmin() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Get(TokenScopesKey) != nil {
				return response.Forbidden(c)
			}
			role := GetWorkspaceRole(c)
			if role != domain.RoleOwner && role != domain.RoleAdmin {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

// RequireOwner restricts access to the workspace owner only.
// PAT callers are always rejected: owner-only operations (e.g. deleting a
// workspace) stay interactive-session-only as defense in depth.
func RequireOwner() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Get(TokenScopesKey) != nil {
				return response.Forbidden(c)
			}
			ws, ok := c.Get("workspace").(*domain.Workspace)
			if !ok || ws.OwnerID == uuid.Nil || ws.OwnerID != GetUserID(c) {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

// TokenAllows reports whether the caller's token ceiling permits permission.
// Interactive sessions always pass; a personal access token must hold the
// scope. Combine it with the role check when a handler grants broader access
// than the route's annotated scope (for example deleting any issue).
func TokenAllows(c *echo.Context, permission string) bool {
	scopes, ok := c.Get(TokenScopesKey).([]string)
	if !ok {
		return true
	}
	return slices.Contains(scopes, permission)
}

func GetUserID(c *echo.Context) uuid.UUID {
	id, _ := c.Get(string(UserIDKey)).(uuid.UUID)
	return id
}

// GetWorkspaceRole returns the requester's role within the current workspace.
func GetWorkspaceRole(c *echo.Context) string {
	role, _ := c.Get("workspace_role").(string)
	return role
}

// RequireTokenPermission adds a scope ceiling while preserving existing JWT RBAC.
// Machine operations that launch code require a write scope even where browser
// sessions historically use the read permission to reach the machine.
func RequireTokenPermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if scopes, ok := c.Get(TokenScopesKey).([]string); ok {
				if !slices.Contains(scopes, permission) {
					return response.Forbidden(c)
				}
				if role, ok := c.Get("workspace_role").(string); ok && !domain.HasPermission(role, permission) {
					return response.Forbidden(c)
				}
			}
			return next(c)
		}
	}
}

// RequireUnrestrictedTokenWorkspaces protects user-level responses containing
// data from multiple workspaces when the handler cannot filter its response.
func RequireUnrestrictedTokenWorkspaces() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if slugs, ok := c.Get(TokenWorkspacesKey).([]string); ok && slugs != nil {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}
