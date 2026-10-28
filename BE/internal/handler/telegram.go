package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

type TelegramHandler struct{ svc *service.TelegramService }

func NewTelegramHandler(svc *service.TelegramService) *TelegramHandler { return &TelegramHandler{svc} }
func telegramError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrTelegramDenied), errors.Is(err, repository.ErrConversationNotFound):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrTelegramBusy):
		return response.Error(c, 409, "TELEGRAM_BUSY", "Another Telegram settings update is in progress")
	case errors.Is(err, repository.ErrTelegramInvalid):
		return response.Error(c, 400, "INVALID_TELEGRAM", "Invalid Telegram settings or account link")
	case errors.Is(err, service.ErrTelegramAPI):
		var refused *service.TelegramAPIError
		if errors.As(err, &refused) {
			return response.Error(c, 502, "TELEGRAM_UNAVAILABLE", refused.Error())
		}
		return response.Error(c, 502, "TELEGRAM_UNAVAILABLE", service.ErrTelegramAPI.Error())
	default:
		return response.InternalError(c)
	}
}
func (h *TelegramHandler) Status(c echo.Context) error {
	result, err := h.svc.Status(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return telegramError(c, err)
	}
	return response.Success(c, 200, result)
}
func (h *TelegramHandler) Save(c echo.Context) error {
	var req service.TelegramSettingsRequest
	if err := bindConversation(c, &req); err != nil {
		return telegramError(c, repository.ErrTelegramInvalid)
	}
	result, err := h.svc.Save(c.Request().Context(), middleware.GetWorkspace(c).ID, req)
	if err != nil {
		return telegramError(c, err)
	}
	return response.Success(c, 200, result)
}
func (h *TelegramHandler) Link(c echo.Context) error {
	var req struct {
		ID int64 `json:"telegram_user_id"`
	}
	if err := bindConversation(c, &req); err != nil {
		return telegramError(c, repository.ErrTelegramInvalid)
	}
	command, err := h.svc.Link(c.Request().Context(), middleware.GetWorkspace(c).ID, req.ID)
	if err != nil {
		return telegramError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return response.Success(c, 200, map[string]any{"command": command, "expires_in": 600})
}
func (h *TelegramHandler) Unlink(c echo.Context) error {
	if err := h.svc.Repo.Unlink(c.Request().Context(), middleware.GetWorkspace(c).ID); err != nil {
		return telegramError(c, err)
	}
	return c.NoContent(204)
}
func (h *TelegramHandler) Webhook(c echo.Context) error {
	ws, err := uuid.Parse(c.Param("workspaceId"))
	if err != nil {
		return c.NoContent(404)
	}
	secret := c.Request().Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if secret == "" || len(secret) > 256 {
		return c.NoContent(http.StatusForbidden)
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 64<<10)
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(400)
	}
	if err = h.svc.Receive(c.Request().Context(), ws, secret, body); err != nil {
		return telegramError(c, err)
	}
	return c.NoContent(http.StatusOK)
}
