package middleware

import (
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

func RequirePermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role, ok := c.Get("workspace_role").(string)
			if !ok {
				return response.Forbidden(c)
			}
			if !domain.HasPermission(role, permission) {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

// RequireOwner restricts access to the workspace owner only.
func RequireOwner() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ws, ok := c.Get("workspace").(*domain.Workspace)
			if !ok || ws.OwnerID == uuid.Nil || ws.OwnerID != GetUserID(c) {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

func GetUserID(c echo.Context) uuid.UUID {
	id, _ := c.Get(string(UserIDKey)).(uuid.UUID)
	return id
}

// GetWorkspaceRole returns the requester's role within the current workspace.
func GetWorkspaceRole(c echo.Context) string {
	role, _ := c.Get("workspace_role").(string)
	return role
}
