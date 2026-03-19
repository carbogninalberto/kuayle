package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type NotificationHandler struct {
	notifSvc *service.NotificationService
}

func NewNotificationHandler(notifSvc *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{notifSvc: notifSvc}
}

func (h *NotificationHandler) List(c echo.Context) error {
	userID := middleware.GetUserID(c)
	notifications, err := h.notifSvc.ListByUser(c.Request().Context(), userID, 50, 0)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.NotificationResponse, len(notifications))
	for i, n := range notifications {
		resp[i] = dto.NotificationResponse{
			ID:           n.ID.String(),
			Type:         n.Type,
			Title:        n.Title,
			ReadAt:       n.ReadAt,
			SnoozedUntil: n.SnoozedUntil,
			ArchivedAt:   n.ArchivedAt,
			CreatedAt:    n.CreatedAt,
		}
		if n.IssueID != nil {
			s := n.IssueID.String()
			resp[i].IssueID = &s
		}
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *NotificationHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid notification ID")
	}
	var req dto.UpdateNotificationRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	n, err := h.notifSvc.Update(c.Request().Context(), id, req)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, n)
}

func (h *NotificationHandler) MarkAllRead(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.notifSvc.MarkAllRead(c.Request().Context(), userID); err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "done"})
}
