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
	service *service.PushService
}

func NewPushHandler(service *service.PushService) *PushHandler {
	return &PushHandler{service: service}
}

func (h *PushHandler) PublicKey(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "public, max-age=300")
	return response.Success(c, http.StatusOK, struct {
		PublicKey string `json:"public_key"`
	}{h.service.PublicKey()})
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
