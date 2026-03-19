package middleware

import (
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func WorkspaceMembership(workspaceRepo *repository.WorkspaceRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			slug := c.Param("slug")
			if slug == "" {
				return next(c)
			}

			ws, err := workspaceRepo.GetBySlug(c.Request().Context(), slug)
			if err != nil || ws == nil {
				return response.NotFound(c, "Workspace")
			}

			userID := GetUserID(c)
			if userID == uuid.Nil {
				return response.Unauthorized(c)
			}

			member, err := workspaceRepo.GetMember(c.Request().Context(), ws.ID, userID)
			if err != nil || member == nil {
				return response.Forbidden(c)
			}

			c.Set("workspace", ws)
			c.Set("workspace_id", ws.ID)
			c.Set("workspace_role", member.Role)
			return next(c)
		}
	}
}
