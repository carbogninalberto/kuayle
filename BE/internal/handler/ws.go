package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/realtime"
	"github.com/labstack/echo/v4"
	"nhooyr.io/websocket"
)

type WSHandler struct {
	hub *realtime.Hub
}

func NewWSHandler(hub *realtime.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

func (h *WSHandler) Handle(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)

	conn, err := websocket.Accept(c.Response().Writer, c.Request(), &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to upgrade connection"})
	}

	client := h.hub.Register(conn, ws.ID, userID)
	defer h.hub.Unregister(client)

	ctx := c.Request().Context()
	go h.hub.WritePump(ctx, client)
	h.hub.ReadPump(ctx, client)

	return nil
}
