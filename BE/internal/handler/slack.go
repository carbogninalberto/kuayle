package handler

import (
	"database/sql"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/slack"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

type SlackHandler struct{ svc *service.SlackService }

func NewSlackHandler(svc *service.SlackService) *SlackHandler { return &SlackHandler{svc} }

func slackError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrSlackSignature):
		return response.Unauthorized(c)
	case errors.Is(err, service.ErrSlackInput), errors.Is(err, service.ErrSlackState):
		return response.Error(c, 400, "BAD_REQUEST", err.Error())
	case errors.Is(err, sql.ErrNoRows):
		return response.NotFound(c, "Slack installation or binding target")
	case errors.Is(err, repository.ErrSlackAdminRequired):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrSlackConnected):
		return response.Error(c, 409, "ALREADY_CONNECTED", "A workspace is already connected. Disconnect it before installing again.")
	case errors.Is(err, service.ErrSlackDisabled):
		return response.Error(c, 503, "SLACK_DISABLED", err.Error())
	case errors.Is(err, slack.ErrRateLimited):
		return response.Error(c, 429, "RATE_LIMITED", "Slack is rate limiting requests. Try again later.")
	case errors.Is(err, slack.ErrAPI), errors.Is(err, slack.ErrRevoked), errors.Is(err, slack.ErrInvalidAuth):
		return response.Error(c, 502, "SLACK_API_ERROR", err.Error())
	default:
		// Driver errors can include row details. Keep credentials out of logs.
		log.Error("Slack integration database or encryption operation failed")
		return response.InternalError(c)
	}
}

func (h *SlackHandler) Status(c *echo.Context) error {
	status, err := h.svc.Status(c.Request().Context(), mw.GetWorkspace(c).ID)
	if err != nil {
		return slackError(c, err)
	}
	return response.Success(c, 200, status)
}

func (h *SlackHandler) Start(c *echo.Context) error {
	url, err := h.svc.Start(c.Request().Context(), mw.GetWorkspace(c).ID, mw.GetUserID(c))
	if err != nil {
		return slackError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return response.Success(c, 200, map[string]string{"url": url})
}

func (h *SlackHandler) Connect(c *echo.Context) error {
	var req struct {
		State uuid.UUID
		Code  string
	}
	if c.Bind(&req) != nil || req.State == uuid.Nil || req.Code == "" {
		return slackError(c, service.ErrSlackInput)
	}
	if err := h.svc.Connect(c.Request().Context(), mw.GetWorkspace(c).ID, mw.GetUserID(c), req.State, req.Code); err != nil {
		return slackError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *SlackHandler) Disconnect(c *echo.Context) error {
	if err := h.svc.Disconnect(c.Request().Context(), mw.GetWorkspace(c).ID); err != nil {
		return slackError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *SlackHandler) Channels(c *echo.Context) error {
	channels, err := h.svc.Channels(c.Request().Context(), mw.GetWorkspace(c).ID)
	if err != nil {
		return slackError(c, err)
	}
	return response.Success(c, 200, channels)
}

func (h *SlackHandler) SetBinding(c *echo.Context) error {
	var b domain.SlackBinding
	if c.Bind(&b) != nil {
		return slackError(c, service.ErrSlackInput)
	}
	if err := h.svc.SetBinding(c.Request().Context(), mw.GetWorkspace(c).ID, mw.GetUserID(c), b); err != nil {
		return slackError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *SlackHandler) Events(c *echo.Context) error {
	const limit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, limit+1))
	if err != nil {
		return slackError(c, service.ErrSlackInput)
	}
	if len(body) > limit {
		return c.NoContent(http.StatusRequestEntityTooLarge)
	}
	headers := c.Request().Header
	challenge, err := h.svc.Event(c.Request().Context(), headers.Get("X-Slack-Request-Timestamp"), headers.Get("X-Slack-Signature"), body)
	if err != nil {
		return slackError(c, err)
	}
	if challenge != "" {
		return c.JSON(200, map[string]string{"challenge": challenge})
	}
	return c.NoContent(200)
}
