package middleware

import (
	"net/http"
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
			if !RequestAllows(c, permission) {
				return response.Forbidden(c)
			}
			ctx := c.Request().Context()
			if a, ok := domain.AccessFromContext(ctx); ok {
				a.Permission = permission
				c.SetRequest(c.Request().WithContext(domain.WithAccess(ctx, a)))
			}
			return next(c)
		}
	}
}

// RequireSession rejects PAT callers, keeping a route interactive-session
// (JWT) only. Routes without a permission annotation get this instead, so
// personal access tokens are denied by default.
func RequireSession() echo.MiddlewareFunc {
	return requireSession(response.Forbidden)
}

func RequireSessionWithRefusal(code, reason string) echo.MiddlewareFunc {
	return requireSession(func(c *echo.Context) error {
		return response.Error(c, http.StatusForbidden, code, reason)
	})
}

func UsesDelegatedRole(c *echo.Context) bool {
	access, _ := c.Get("role_access").(*domain.RoleAccess)
	return access != nil && (!domain.BuiltinRole(access.Role) || len(access.Teams) > 0)
}

func requireSession(refuse echo.HandlerFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Get(TokenScopesKey) != nil {
				return refuse(c)
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
			if (UsesDelegatedRole(c) || c.Get(TokenScopesKey) != nil) && !RequestAllows(c, permission) {
				return response.Forbidden(c)
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

func RequestAllows(c *echo.Context, permission string) bool {
	if !TokenAllows(c, permission) {
		return false
	}
	if access, ok := c.Get("role_access").(*domain.RoleAccess); ok {
		if team := c.Param("teamId"); team != "" {
			id, err := uuid.Parse(team)
			return err == nil && access.Allows(id, permission)
		}
		if a, ok := domain.AccessFromContext(c.Request().Context()); ok && a.TeamID != uuid.Nil {
			return access.Allows(a.TeamID, permission)
		}
		// Membership is workspace-wide. Membership writes separately check
		// the affected permissions in each team under the role-change lock.
		if permission == domain.PermMemberInvite {
			return access.Allows(uuid.Nil, permission)
		}
		return access.AllowsEverywhere(permission)
	}
	role, ok := c.Get("workspace_role").(string)
	return !ok || domain.HasPermission(role, permission)
}

// RequireTeamResourcePermission is for routes whose repositories apply
// teamVisible to the selected rows, including insert and update targets.
func RequireTeamResourcePermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		plain := RequirePermission(permission)(next)
		return func(c *echo.Context) error {
			roles, ok := c.Get("role_access").(*domain.RoleAccess)
			if !ok || len(roles.Teams) == 0 {
				return plain(c)
			}
			if !TokenAllows(c, permission) {
				return response.Forbidden(c)
			}
			allowed := roles.Allows(uuid.Nil, permission)
			for team := range roles.Teams {
				allowed = allowed || roles.Allows(team, permission)
			}
			if !allowed {
				return response.Forbidden(c)
			}
			ctx := c.Request().Context()
			access, ok := domain.AccessFromContext(ctx)
			if !ok {
				return response.Forbidden(c)
			}
			access.TeamPermission = permission
			c.SetRequest(c.Request().WithContext(domain.WithAccess(ctx, access)))
			return next(c)
		}
	}
}
