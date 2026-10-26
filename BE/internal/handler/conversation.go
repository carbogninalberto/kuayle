package handler

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
	"net/http"
	"net/url"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
	"strconv"
	"strings"
	"time"
)

type ConversationHandler struct {
	svc    *service.ConversationService
	origin string
}

func NewConversationHandler(svc *service.ConversationService, frontendURL string) *ConversationHandler {
	u, _ := url.Parse(frontendURL)
	origin := ""
	if u != nil {
		origin = u.Scheme + "://" + u.Host
	}
	return &ConversationHandler{svc: svc, origin: origin}
}
func conversationError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrConversationNotFound):
		return response.NotFound(c, "Conversation")
	case errors.Is(err, repository.ErrConversationBusy):
		return response.Error(c, http.StatusConflict, "CONVERSATION_BUSY", "A reply is already in progress")
	case errors.Is(err, repository.ErrConversationConflict):
		return response.Error(c, http.StatusConflict, "CONVERSATION_CONFLICT", "This request conflicts with the current conversation")
	case errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_CONVERSATION", "Invalid conversation request")
	case errors.Is(err, service.ErrAISettingsNotConfigured):
		return response.Error(c, http.StatusBadRequest, "AI_NOT_CONFIGURED", "Configure the workspace AI provider first")
	default:
		return response.InternalError(c)
	}
}
func conversationID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("conversationId"))
	if err != nil {
		return uuid.Nil, repository.ErrConversationNotFound
	}
	return id, nil
}
func conversationCursor(c echo.Context, name string) (int64, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, repository.ErrConversationInvalid
	}
	return v, nil
}
func (h *ConversationHandler) Status(c echo.Context) error {
	result, err := h.svc.Status(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) List(c echo.Context) error {
	result, err := h.svc.Repo.List(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) Create(c echo.Context) error {
	var req struct {
		Title string `json:"title"`
	}
	if err := bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.Repo.Create(c.Request().Context(), middleware.GetWorkspace(c).ID, req.Title)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}
func (h *ConversationHandler) History(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	before, err := conversationCursor(c, "before")
	if err != nil {
		return conversationError(c, err)
	}
	result, err := h.svc.Repo.History(c.Request().Context(), middleware.GetWorkspace(c).ID, id, before)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) Events(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	after, err := conversationCursor(c, "after")
	if err != nil {
		return conversationError(c, err)
	}
	result, err := h.svc.Repo.Events(c.Request().Context(), middleware.GetWorkspace(c).ID, id, after)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) Send(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	var req struct {
		RequestID uuid.UUID `json:"request_id"`
		Content   string    `json:"content"`
	}
	if err = bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.Send(c.Request().Context(), middleware.GetWorkspace(c).ID, id, req.RequestID, req.Content)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}
func (h *ConversationHandler) Retry(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	message, err := uuid.Parse(c.Param("messageId"))
	if err != nil {
		return conversationError(c, repository.ErrConversationNotFound)
	}
	var req struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	if err = bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.Retry(c.Request().Context(), middleware.GetWorkspace(c).ID, id, message, req.RequestID)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}
func (h *ConversationHandler) Cancel(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	if err = h.svc.Repo.Cancel(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return conversationError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Stream reuses the authenticated Echo/WebSocket stack, with conversation-level
// authorization rather than the workspace-wide broadcast hub. Persisted events
// are the source for both initial replay and live delivery on every API replica.
func (h *ConversationHandler) Stream(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	cursor, err := conversationCursor(c, "after")
	if err != nil {
		return conversationError(c, err)
	}
	ws := middleware.GetWorkspace(c).ID
	if _, err = h.svc.Repo.Events(c.Request().Context(), ws, id, cursor); err != nil {
		return conversationError(c, err)
	}
	if h.origin == "" || !strings.EqualFold(c.Request().Header.Get("Origin"), h.origin) {
		return response.Forbidden(c)
	}
	conn, err := websocket.Accept(c.Response().Writer, c.Request(), &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	ctx := conn.CloseRead(c.Request().Context())
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		events, err := h.svc.Repo.Events(ctx, ws, id, cursor)
		if err != nil {
			conn.Close(websocket.StatusPolicyViolation, "Conversation access unavailable")
			return nil
		}
		for _, event := range events {
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = wsjson.Write(writeCtx, conn, event)
			cancel()
			if err != nil {
				return nil
			}
			cursor = event.Cursor
		}
		if len(events) == 200 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-heartbeat.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return nil
			}
		}
	}
}

func bindConversation(c echo.Context, req any) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 32<<10)
	return c.Bind(req)
}
