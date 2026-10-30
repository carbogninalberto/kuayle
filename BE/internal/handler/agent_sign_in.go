package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

// Agent sign-ins belong to the signed-in user: every handler reads and writes
// only the caller's own, so no role (admins included) can see another user's.
// Values are never returned. Guests run no agents and have none.

func (h *DevMachineHandler) ListAgentSignIns(c echo.Context) error {
	if middleware.GetWorkspaceRole(c) == domain.RoleGuest {
		return response.Forbidden(c)
	}
	signIns, err := h.service.ListAgentSignIns(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, signIns)
}

// SaveAgentSignInToken stores a token the user pasted, such as the output
// of `claude setup-token`.
func (h *DevMachineHandler) SaveAgentSignInToken(c echo.Context) error {
	if middleware.GetWorkspaceRole(c) == domain.RoleGuest {
		return response.Forbidden(c)
	}
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 8*1024)
	var request dto.SaveAgentSignInTokenRequest
	if !bindAndValidate(c, &request) {
		return nil
	}
	signIn, err := h.service.SaveAgentSignInToken(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), c.Param("provider"), request.Token)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, signIn)
}

// DeleteAgentSignIn removes the user's token (?kind=token), terminal sign-in
// (?kind=credentials) or both for an agent.
func (h *DevMachineHandler) DeleteAgentSignIn(c echo.Context) error {
	if middleware.GetWorkspaceRole(c) == domain.RoleGuest {
		return response.Forbidden(c)
	}
	if err := h.service.DeleteAgentSignIn(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), c.Param("provider"), c.QueryParam("kind")); err != nil {
		return machineError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
