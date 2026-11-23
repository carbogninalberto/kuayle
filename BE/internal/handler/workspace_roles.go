package handler

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

func roleError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return response.NotFound(c, "Role or membership")
	case errors.Is(err, repository.ErrRoleInUse), errors.Is(err, repository.ErrRoleNameTaken):
		return response.Error(c, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, repository.ErrRoleProtected), errors.Is(err, repository.ErrRoleGrant):
		return response.Forbidden(c)
	case errors.Is(err, service.ErrInvalidRole):
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		return response.InternalError(c)
	}
}

func (h *WorkspaceHandler) ListRoles(c *echo.Context) error {
	roles, err := h.workspaceSvc.ListRoles(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return roleError(c, err)
	}
	return response.Success(c, http.StatusOK, roles)
}

func (h *WorkspaceHandler) SaveRole(c *echo.Context) error {
	var req dto.SaveWorkspaceRoleRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	key := c.Param("roleKey")
	role, err := h.workspaceSvc.SaveRole(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), key, req)
	if err != nil {
		return roleError(c, err)
	}
	status := http.StatusOK
	if key == "" {
		status = http.StatusCreated
	}
	return response.Success(c, status, role)
}

func (h *WorkspaceHandler) DeleteRole(c *echo.Context) error {
	if err := h.workspaceSvc.DeleteRole(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), c.Param("roleKey")); err != nil {
		return roleError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *WorkspaceHandler) SetTeamRole(c *echo.Context) error {
	user, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID")
	}
	team, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}
	key := ""
	if c.Request().Method != http.MethodDelete {
		var req dto.UpdateMemberRoleRequest
		if err := c.Bind(&req); err != nil || req.Role == "" {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "A role is required")
		}
		key = req.Role
	}
	if err := h.workspaceSvc.SetTeamRole(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), user, team, key); err != nil {
		return roleError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *WorkspaceHandler) RolePermissions(c *echo.Context) error {
	return response.Success(c, http.StatusOK, domain.PermissionGroups)
}

func (h *WorkspaceHandler) TeamRoles(c *echo.Context) error {
	user, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID")
	}
	roles, err := h.workspaceSvc.TeamRoles(c.Request().Context(), middleware.GetWorkspace(c).ID, user)
	if err != nil {
		return roleError(c, err)
	}
	return response.Success(c, http.StatusOK, roles)
}
