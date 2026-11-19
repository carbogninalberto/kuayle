package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

type PushHandler struct {
	service    *service.PushService
	isSysAdmin func(uuid.UUID) bool
}

func NewPushHandler(service *service.PushService, isSysAdmin func(uuid.UUID) bool) *PushHandler {
	return &PushHandler{service: service, isSysAdmin: isSysAdmin}
}

func (h *PushHandler) PublicKey(c *echo.Context) error {
	key, err := h.service.PublicKey(c.Request().Context())
	if err != nil {
		return response.InternalError(c)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return response.Success(c, http.StatusOK, struct {
		PublicKey string `json:"public_key"`
	}{key})
}

func (h *PushHandler) Register(c *echo.Context) error {
	var req dto.RegisterPushSubscriptionRequest
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 8<<10)
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid push subscription")
	}
	id, err := h.service.Register(c.Request().Context(), middleware.GetUserID(c), middleware.GetSessionSSOWorkspace(c), c.Request().UserAgent(), req)
	switch {
	case errors.Is(err, service.ErrPushUnavailable):
		return response.Error(c, http.StatusServiceUnavailable, "PUSH_UNAVAILABLE", err.Error())
	case errors.Is(err, service.ErrPushInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_PUSH_SUBSCRIPTION", err.Error())
	case errors.Is(err, service.ErrPushForbidden):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrPushSubscriptionConflict):
		return response.Error(c, http.StatusConflict, "PUSH_SUBSCRIPTION_CONFLICT", "This subscription cannot be replaced")
	case err != nil:
		return response.InternalError(c)
	default:
		return response.Success(c, http.StatusOK, struct {
			ID uuid.UUID `json:"id"`
		}{id})
	}
}

func (h *PushHandler) Delete(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid subscription ID")
	}
	if err := h.service.Delete(c.Request().Context(), middleware.GetUserID(c), middleware.GetSessionSSOWorkspace(c), id); err != nil {
		return response.InternalError(c)
	}
	return c.NoContent(http.StatusNoContent)
}

func pushSettingsResponse(c *echo.Context, data any, err error) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	switch {
	case err == nil:
		return response.Success(c, http.StatusOK, data)
	case errors.Is(err, service.ErrPushReadOnly):
		return response.Error(c, http.StatusConflict, "PUSH_READ_ONLY", err.Error())
	case errors.Is(err, service.ErrPushConfirm):
		return response.Error(c, http.StatusConflict, "PUSH_CONFIRM_REQUIRED", err.Error())
	case errors.Is(err, service.ErrPushSubject), errors.Is(err, service.ErrPushNoKeys):
		return response.Error(c, http.StatusBadRequest, "INVALID_PUSH_SETTINGS", err.Error())
	case errors.Is(err, service.ErrPushUnavailable):
		return response.Error(c, http.StatusServiceUnavailable, "PUSH_UNAVAILABLE", err.Error())
	default:
		return response.InternalError(c)
	}
}

func (h *PushHandler) Settings(c *echo.Context) error {
	user := middleware.GetUserID(c)
	if !h.isSysAdmin(user) {
		return response.Forbidden(c)
	}
	status, err := h.service.Settings(c.Request().Context(), user)
	return pushSettingsResponse(c, status, err)
}
func (h *PushHandler) GenerateKeys(c *echo.Context) error {
	user := middleware.GetUserID(c)
	if !h.isSysAdmin(user) {
		return response.Forbidden(c)
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := bindConversation(c, &req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid key generation request")
	}
	status, err := h.service.GenerateKeys(c.Request().Context(), user, req.Confirm)
	return pushSettingsResponse(c, status, err)
}
func (h *PushHandler) UpdateSettings(c *echo.Context) error {
	user := middleware.GetUserID(c)
	if !h.isSysAdmin(user) {
		return response.Forbidden(c)
	}
	var req struct {
		Subject *string `json:"subject"`
		Enabled *bool   `json:"enabled"`
	}
	if err := bindConversation(c, &req); err != nil || req.Subject == nil || req.Enabled == nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Contact and enabled setting are required")
	}
	status, err := h.service.UpdateSettings(c.Request().Context(), user, *req.Subject, *req.Enabled)
	return pushSettingsResponse(c, status, err)
}
func (h *PushHandler) Test(c *echo.Context) error {
	user := middleware.GetUserID(c)
	if !h.isSysAdmin(user) {
		return response.Forbidden(c)
	}
	result, err := h.service.Test(c.Request().Context(), user)
	return pushSettingsResponse(c, result, err)
}
