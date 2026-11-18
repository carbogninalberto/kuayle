package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

func (h *DevMachineHandler) StartCodexDeviceLogin(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	if middleware.GetWorkspaceRole(c) == domain.RoleGuest {
		return response.Forbidden(c)
	}
	attempt, err := h.service.StartCodexDeviceLogin(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c))
	if err != nil {
		return codexDeviceError(c, err)
	}
	return response.Success(c, http.StatusOK, attempt)
}

func (h *DevMachineHandler) PollCodexDeviceLogin(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	if middleware.GetWorkspaceRole(c) == domain.RoleGuest {
		return response.Forbidden(c)
	}
	id, ok := bindCodexDeviceAttempt(c)
	if !ok {
		return nil
	}
	status, err := h.service.PollCodexDeviceLogin(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), id)
	if err != nil {
		return codexDeviceError(c, err)
	}
	return response.Success(c, http.StatusOK, status)
}

func (h *DevMachineHandler) CancelCodexDeviceLogin(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	if middleware.GetWorkspaceRole(c) == domain.RoleGuest {
		return response.Forbidden(c)
	}
	id, ok := bindCodexDeviceAttempt(c)
	if !ok {
		return nil
	}
	if err := h.service.CancelCodexDeviceLogin(middleware.GetWorkspace(c).ID, middleware.GetUserID(c), id); err != nil {
		return codexDeviceError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func bindCodexDeviceAttempt(c *echo.Context) (uuid.UUID, bool) {
	var request dto.CodexDeviceAttemptRequest
	if err := bindStrictJSON(c, &request); err != nil || responseCommitted(c) {
		return uuid.Nil, false
	}
	if request.AttemptID == uuid.Nil {
		_ = response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid Codex device login attempt ID")
		return uuid.Nil, false
	}
	return request.AttemptID, true
}

func codexDeviceError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrCodexDeviceUnavailable):
		return response.Error(c, http.StatusServiceUnavailable, "CODEX_DEVICE_UNAVAILABLE", service.ErrCodexDeviceUnavailable.Error())
	case errors.Is(err, service.ErrCodexDeviceBusy):
		return response.Error(c, http.StatusTooManyRequests, "CODEX_DEVICE_BUSY", service.ErrCodexDeviceBusy.Error())
	case errors.Is(err, service.ErrCodexDeviceAttemptNotFound):
		return response.NotFound(c, "Codex device login attempt")
	default:
		return machineError(c, err)
	}
}
