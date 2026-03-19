package handler

import (
	"net/http"
	"time"

	"github.com/carbon/carbon-backend/internal/domain"
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
	tab := c.QueryParam("tab")

	var items []domain.Notification
	var err error

	switch tab {
	case "snoozed":
		items, err = h.notifSvc.ListSnoozed(c.Request().Context(), userID)
	case "archived":
		items, err = h.notifSvc.ListArchived(c.Request().Context(), userID, 50)
	default:
		items, err = h.notifSvc.ListByUser(c.Request().Context(), userID, 50, 0)
	}

	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.NotificationResponse, len(items))
	for i, n := range items {
		resp[i] = toNotifResponse(n)
	}

	unreadCount, _ := h.notifSvc.UnreadCount(c.Request().Context(), userID)

	return response.Success(c, http.StatusOK, dto.NotificationListResponse{
		Notifications: resp,
		UnreadCount:   unreadCount,
	})
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
	return response.Success(c, http.StatusOK, toNotifResponse(*n))
}

func (h *NotificationHandler) Snooze(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid notification ID")
	}
	var req struct {
		Until string `json:"until"`
	}
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	until, err := time.Parse(time.RFC3339, req.Until)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid date format, use RFC3339")
	}
	n, err := h.notifSvc.Snooze(c.Request().Context(), id, until)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toNotifResponse(*n))
}

func (h *NotificationHandler) Archive(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid notification ID")
	}
	n, err := h.notifSvc.Archive(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toNotifResponse(*n))
}

func (h *NotificationHandler) MarkAllRead(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.notifSvc.MarkAllRead(c.Request().Context(), userID); err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "done"})
}

func toNotifResponse(n domain.Notification) dto.NotificationResponse {
	resp := dto.NotificationResponse{
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
		resp.IssueID = &s
	}
	return resp
}
