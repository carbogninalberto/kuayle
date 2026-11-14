package middleware

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// SSORequirementSource reports whether a workspace has require-SSO turned on.
// Implemented by SSOService so the middleware does not depend on its storage.
type SSORequirementSource interface {
	RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error)
}

func WorkspaceMembership(workspaceRepo repository.WorkspaceRepo, sso ...SSORequirementSource) echo.MiddlewareFunc {
	var ssoSource SSORequirementSource
	if len(sso) > 0 {
		ssoSource = sso[0]
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
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

			// A PAT restricted to specific workspaces may not cross into others.
			tokenWorkspaces, isPAT := c.Get(TokenWorkspacesKey).([]string)
			if isPAT && tokenWorkspaces != nil {
				if !slices.Contains(tokenWorkspaces, slug) {
					return response.Forbidden(c)
				}
			}

			// Require-SSO is per workspace. A session minted by another
			// workspace's SSO is not a credential for this one. Owners keep the
			// break-glass password path, and PAT callers are unaffected in
			// phase 1 (SCIM deprovisioning revokes them in phase 3).
			if _, pat := c.Get(TokenScopesKey).([]string); !pat && ssoSource != nil &&
				member.Role != domain.RoleOwner && GetSessionSSOWorkspace(c) != ws.ID {
				required, err := ssoSource.RequiresSSO(c.Request().Context(), ws.ID)
				if err != nil {
					return response.InternalError(c)
				}
				if required {
					return response.SSORequired(c, ws.Slug)
				}
			}

			c.Set("workspace", ws)
			c.Set("workspace_id", ws.ID)
			c.Set("workspace_role", member.Role)
			c.SetRequest(c.Request().WithContext(domain.WithAccess(c.Request().Context(), domain.Access{UserID: userID, WorkspaceID: ws.ID})))
			return next(c)
		}
	}
}

func GetWorkspace(c *echo.Context) *domain.Workspace {
	workspace, _ := c.Get("workspace").(*domain.Workspace)
	return workspace
}
