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

// SSONotEnforced is the stub for callers that do not enforce require-SSO,
// e.g. tests that only exercise membership.
var SSONotEnforced SSORequirementSource = ssoNotEnforced{}

type ssoNotEnforced struct{}

func (ssoNotEnforced) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	return false, nil
}

func WorkspaceMembership(workspaceRepo repository.WorkspaceRepo, sso SSORequirementSource) echo.MiddlewareFunc {
	if sso == nil {
		panic("middleware.WorkspaceMembership: an SSO requirement source is required")
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

			// An SSO-minted session is scoped to the workspace whose IdP
			// minted it. It is not a credential for any other workspace, so a
			// workspace admin's own IdP can never reach beyond their workspace.
			_, isPAT = c.Get(TokenScopesKey).([]string)
			if !isPAT {
				if ssoWorkspace := GetSessionSSOWorkspace(c); ssoWorkspace != uuid.Nil && ssoWorkspace != ws.ID {
					return response.SSOSessionScoped(c)
				}
			}

			// Require-SSO is per workspace. For sessions, owners keep the
			// break-glass password path. A PAT reaches the workspace only if
			// it was minted from a session SSO-scoped to it, owners included:
			// break-glass is interactive sign-in only. This also stops older
			// tokens once require-SSO is turned on.
			var mustCheck bool
			if isPAT {
				tokenSSOWorkspace, _ := c.Get(TokenSSOWorkspaceKey).(uuid.UUID)
				mustCheck = tokenSSOWorkspace != ws.ID
			} else {
				mustCheck = member.Role != domain.RoleOwner && GetSessionSSOWorkspace(c) != ws.ID
			}
			if mustCheck {
				required, err := sso.RequiresSSO(c.Request().Context(), ws.ID)
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
